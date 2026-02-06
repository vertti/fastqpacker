// Package compress provides FASTQ compression and decompression.
package compress

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"

	"github.com/klauspost/compress/zstd"
	"golang.org/x/sync/errgroup"

	"github.com/vertti/fastqpacker/internal/encoder"
	"github.com/vertti/fastqpacker/internal/format"
	"github.com/vertti/fastqpacker/internal/parser"
)

// blockBuffers holds reusable buffers for block compression.
// Pooled via sync.Pool to avoid allocations across blocks.
type blockBuffers struct {
	seqPacked   []byte
	nPositions  []byte
	seqLengths  []byte
	quality     []byte
	headers     []byte
	nPosScratch []byte
	// Reusable destination slices for zstd EncodeAll
	compSeq     []byte
	compQual    []byte
	compHeaders []byte
	compNPos    []byte
	compLen     []byte
	outputBuf   bytes.Buffer
}

var blockBufferPool = sync.Pool{
	New: func() any {
		return &blockBuffers{
			nPosScratch: make([]byte, 0, 256),
		}
	},
}

func (b *blockBuffers) reset() {
	b.seqPacked = b.seqPacked[:0]
	b.nPositions = b.nPositions[:0]
	b.seqLengths = b.seqLengths[:0]
	b.quality = b.quality[:0]
	b.headers = b.headers[:0]
	b.compSeq = b.compSeq[:0]
	b.compQual = b.compQual[:0]
	b.compHeaders = b.compHeaders[:0]
	b.compNPos = b.compNPos[:0]
	b.compLen = b.compLen[:0]
	b.outputBuf.Reset()
}

// DefaultBlockSize is the default number of records per block.
const DefaultBlockSize = 100000

// Options configures compression behavior.
type Options struct {
	BlockSize uint32 // Records per block (default: 100000)
	Workers   int    // Number of parallel compression workers (default: NumCPU)
}

// DecompressOptions configures decompression behavior.
type DecompressOptions struct {
	Workers int // Number of parallel decompression workers (default: NumCPU)
}

// compressJob represents a block to be compressed.
type compressJob struct {
	seqNum  int
	records []*parser.Record
}

// compressResult represents a compressed block.
type compressResult struct {
	seqNum int
	data   []byte
	err    error
}

// decompressJob represents a block to be decompressed.
type decompressJob struct {
	seqNum     int
	header     *format.BlockHeader
	compressed [][]byte // 5 compressed streams: seq, qual, headers, nPos, lengths
}

// decompressResult represents a decompressed block.
type decompressResult struct {
	seqNum int
	data   []byte
	err    error
}

// Compress reads FASTQ from r and writes compressed data to w.
func Compress(r io.Reader, w io.Writer, opts *Options) error {
	if opts == nil {
		opts = &Options{BlockSize: DefaultBlockSize}
	}
	if opts.BlockSize == 0 {
		opts.BlockSize = DefaultBlockSize
	}
	if opts.Workers == 0 {
		opts.Workers = runtime.NumCPU()
	}

	// Parse first batch to detect quality encoding
	p := parser.New(r)
	firstBatch, err := p.NextBatch(int(opts.BlockSize))
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("parsing FASTQ: %w", err)
	}

	// Detect encoding from first batch
	qualEncoding := encoder.EncodingPhred33
	if len(firstBatch) > 0 {
		qualities := make([][]byte, len(firstBatch))
		for i, rec := range firstBatch {
			qualities[i] = rec.Quality
		}
		qualEncoding = encoder.DetectEncoding(qualities)
	}

	// Write file header with encoding flag
	header := format.FileHeader{
		Version:   1,
		BlockSize: opts.BlockSize,
		Flags:     0,
	}
	if qualEncoding == encoder.EncodingPhred64 {
		header.Flags |= format.FlagPhred64
	}
	if err := header.Write(w); err != nil {
		return fmt.Errorf("writing file header: %w", err)
	}

	// Single worker path (simpler, no goroutine overhead)
	if opts.Workers == 1 {
		return compressSingleWorkerWithBatch(firstBatch, p, w, opts, qualEncoding, errors.Is(err, io.EOF))
	}

	return compressParallelWithBatch(firstBatch, p, w, opts, qualEncoding, errors.Is(err, io.EOF))
}

func compressSingleWorkerWithBatch(firstBatch []*parser.Record, p *parser.Parser, w io.Writer, opts *Options, qualEncoding encoder.QualityEncoding, firstBatchEOF bool) error {
	zstdEnc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return fmt.Errorf("creating zstd encoder: %w", err)
	}
	defer zstdEnc.Close() //nolint:errcheck // encoder close during cleanup

	// Process first batch if present
	if len(firstBatch) > 0 {
		if blockErr := compressBlock(firstBatch, w, zstdEnc, qualEncoding); blockErr != nil {
			return fmt.Errorf("compressing block: %w", blockErr)
		}
	}

	if firstBatchEOF {
		return nil
	}

	for {
		batch, err := p.NextBatch(int(opts.BlockSize))
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("parsing FASTQ: %w", err)
		}
		if len(batch) == 0 {
			break
		}

		if blockErr := compressBlock(batch, w, zstdEnc, qualEncoding); blockErr != nil {
			return fmt.Errorf("compressing block: %w", blockErr)
		}

		if errors.Is(err, io.EOF) {
			break
		}
	}

	return nil
}

func compressParallelWithBatch(firstBatch []*parser.Record, p *parser.Parser, w io.Writer, opts *Options, qualEncoding encoder.QualityEncoding, firstBatchEOF bool) error {
	jobs := make(chan compressJob, opts.Workers*2)
	results := make(chan compressResult, opts.Workers*2)

	g, ctx := errgroup.WithContext(ctx())

	// Start workers
	for range opts.Workers {
		g.Go(func() error {
			return runCompressionWorker(ctx, jobs, results, qualEncoding)
		})
	}

	// Producer: dispatch first batch and continue parsing
	g.Go(func() error {
		defer close(jobs)
		return produceCompressJobs(ctx, jobs, firstBatch, p, opts.BlockSize, firstBatchEOF)
	})

	// Collector: write results in order
	var collectorErr error
	collectorDone := make(chan struct{})
	go func() {
		defer close(collectorDone)
		collectorErr = collectAndWriteResults(results, w)
	}()

	// Wait for workers and producer
	workerErr := g.Wait()
	close(results)

	// Wait for collector
	<-collectorDone

	if workerErr != nil {
		return workerErr
	}
	return collectorErr
}

func runCompressionWorker(ctx context.Context, jobs <-chan compressJob, results chan<- compressResult, qualEncoding encoder.QualityEncoding) error {
	zstdEnc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return fmt.Errorf("creating zstd encoder: %w", err)
	}
	defer zstdEnc.Close() //nolint:errcheck // encoder close during cleanup

	for job := range jobs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		data, err := compressBlockToBytes(job.records, zstdEnc, qualEncoding)
		results <- compressResult{seqNum: job.seqNum, data: data, err: err}
	}
	return nil
}

func produceCompressJobs(ctx context.Context, jobs chan<- compressJob, firstBatch []*parser.Record, p *parser.Parser, blockSize uint32, firstBatchEOF bool) error {
	seqNum := 0

	// Send first batch if present
	if len(firstBatch) > 0 {
		select {
		case jobs <- compressJob{seqNum: seqNum, records: firstBatch}:
			seqNum++
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if firstBatchEOF {
		return nil
	}

	for {
		batch, err := p.NextBatch(int(blockSize))
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("parsing FASTQ: %w", err)
		}
		if len(batch) == 0 {
			break
		}

		select {
		case jobs <- compressJob{seqNum: seqNum, records: batch}:
			seqNum++
		case <-ctx.Done():
			return ctx.Err()
		}

		if errors.Is(err, io.EOF) {
			break
		}
	}
	return nil
}

func collectAndWriteResults(results <-chan compressResult, w io.Writer) error {
	pending := make(map[int][]byte)
	nextSeqNum := 0

	for result := range results {
		if result.err != nil {
			return fmt.Errorf("compressing block %d: %w", result.seqNum, result.err)
		}

		pending[result.seqNum] = result.data

		// Write all sequential results available
		for {
			data, ok := pending[nextSeqNum]
			if !ok {
				break
			}
			if _, err := w.Write(data); err != nil {
				return fmt.Errorf("writing block %d: %w", nextSeqNum, err)
			}
			delete(pending, nextSeqNum)
			nextSeqNum++
		}
	}

	return nil
}

func collectAndWriteDecompressResults(results <-chan decompressResult, w io.Writer) error {
	pending := make(map[int][]byte)
	nextSeqNum := 0

	for result := range results {
		if result.err != nil {
			return fmt.Errorf("decompressing block %d: %w", result.seqNum, result.err)
		}

		pending[result.seqNum] = result.data

		// Write all sequential results available
		for {
			data, ok := pending[nextSeqNum]
			if !ok {
				break
			}
			if _, err := w.Write(data); err != nil {
				return fmt.Errorf("writing block %d: %w", nextSeqNum, err)
			}
			delete(pending, nextSeqNum)
			nextSeqNum++
		}
	}

	return nil
}

// ctx returns a background context. Separate function to avoid import cycle.
func ctx() context.Context {
	return context.Background()
}

// compressBlockToBytes compresses a block and returns the serialized bytes.
func compressBlockToBytes(records []*parser.Record, zstdEnc *zstd.Encoder, qualEncoding encoder.QualityEncoding) ([]byte, error) {
	bufs := blockBufferPool.Get().(*blockBuffers) //nolint:errcheck // pool always returns *blockBuffers
	bufs.reset()
	defer blockBufferPool.Put(bufs)

	if err := compressBlockWithBuffers(records, &bufs.outputBuf, zstdEnc, qualEncoding, bufs); err != nil {
		return nil, err
	}
	// Copy output so the pooled buffer can be reused
	out := make([]byte, bufs.outputBuf.Len())
	copy(out, bufs.outputBuf.Bytes())
	return out, nil
}

func compressBlock(records []*parser.Record, w io.Writer, zstdEnc *zstd.Encoder, qualEncoding encoder.QualityEncoding) error {
	bufs := blockBufferPool.Get().(*blockBuffers) //nolint:errcheck // pool always returns *blockBuffers
	bufs.reset()
	defer blockBufferPool.Put(bufs)

	return compressBlockWithBuffers(records, w, zstdEnc, qualEncoding, bufs)
}

func compressBlockWithBuffers(records []*parser.Record, w io.Writer, zstdEnc *zstd.Encoder, qualEncoding encoder.QualityEncoding, bufs *blockBuffers) error {
	var originalSeqSize, originalQualSize uint32
	var seqLenBuf [4]byte
	var headerLenBuf [2]byte

	for _, rec := range records {
		// Encode sequence
		packed, nPos := encoder.PackBases(rec.Sequence)
		bufs.seqPacked = append(bufs.seqPacked, packed...)

		// Store N positions: count (uint16) + positions (uint16 each)
		needed := 2 + len(nPos)*2
		if cap(bufs.nPosScratch) < needed {
			bufs.nPosScratch = make([]byte, needed)
		}
		bufs.nPosScratch = bufs.nPosScratch[:needed]
		binary.LittleEndian.PutUint16(bufs.nPosScratch[0:2], uint16(len(nPos))) //nolint:gosec // len(nPos) bounded by seq length
		for i, pos := range nPos {
			binary.LittleEndian.PutUint16(bufs.nPosScratch[2+i*2:4+i*2], pos)
		}
		bufs.nPositions = append(bufs.nPositions, bufs.nPosScratch...)

		// Store sequence length (stack array, zero allocs)
		binary.LittleEndian.PutUint32(seqLenBuf[:], uint32(len(rec.Sequence))) //nolint:gosec // seq length bounded
		bufs.seqLengths = append(bufs.seqLengths, seqLenBuf[:]...)

		originalSeqSize += uint32(len(rec.Sequence)) //nolint:gosec // bounded

		// Encode quality: append into buffer, then normalize+delta the tail in-place
		qualStart := len(bufs.quality)
		bufs.quality = append(bufs.quality, rec.Quality...)
		qualSlice := bufs.quality[qualStart:]
		encoder.NormalizeQuality(qualSlice, qualEncoding)
		encoder.DeltaEncode(qualSlice)
		originalQualSize += uint32(len(rec.Quality)) //nolint:gosec // bounded

		// Store header with length prefix (no allocation)
		binary.LittleEndian.PutUint16(headerLenBuf[:], uint16(len(rec.Header))) //nolint:gosec // header length bounded
		bufs.headers = append(bufs.headers, headerLenBuf[:]...)
		bufs.headers = append(bufs.headers, rec.Header...)
	}

	// Compress each stream with zstd, reusing destination slices
	bufs.compSeq = zstdEnc.EncodeAll(bufs.seqPacked, bufs.compSeq[:0])
	bufs.compQual = zstdEnc.EncodeAll(bufs.quality, bufs.compQual[:0])
	bufs.compHeaders = zstdEnc.EncodeAll(bufs.headers, bufs.compHeaders[:0])
	bufs.compNPos = zstdEnc.EncodeAll(bufs.nPositions, bufs.compNPos[:0])
	bufs.compLen = zstdEnc.EncodeAll(bufs.seqLengths, bufs.compLen[:0])

	// Write block header
	//nolint:gosec // All lengths are bounded by block size and data sizes
	blockHeader := format.BlockHeader{
		NumRecords:       uint32(len(records)),
		SeqDataSize:      uint32(len(bufs.compSeq)),
		QualDataSize:     uint32(len(bufs.compQual)),
		HeaderDataSize:   uint32(len(bufs.compHeaders)),
		NPositionsSize:   uint32(len(bufs.compNPos)),
		SeqLengthsSize:   uint32(len(bufs.compLen)),
		OriginalSeqSize:  originalSeqSize,
		OriginalQualSize: originalQualSize,
	}
	if err := blockHeader.Write(w); err != nil {
		return err
	}

	// Write compressed data
	for _, data := range [][]byte{bufs.compSeq, bufs.compQual, bufs.compHeaders, bufs.compNPos, bufs.compLen} {
		if _, err := w.Write(data); err != nil {
			return err
		}
	}

	return nil
}

// Decompress reads compressed data from r and writes FASTQ to w.
func Decompress(r io.Reader, w io.Writer, opts *DecompressOptions) error {
	if opts == nil {
		opts = &DecompressOptions{}
	}
	if opts.Workers == 0 {
		opts.Workers = runtime.NumCPU()
	}

	// Read file header
	fileHeader, err := format.ReadFileHeader(r)
	if err != nil {
		return fmt.Errorf("reading file header: %w", err)
	}

	// Determine quality encoding from flags
	qualEncoding := encoder.EncodingPhred33
	if fileHeader.Flags&format.FlagPhred64 != 0 {
		qualEncoding = encoder.EncodingPhred64
	}

	// Single worker path
	if opts.Workers == 1 {
		return decompressSingleWorker(r, w, qualEncoding)
	}

	return decompressParallel(r, w, opts.Workers, qualEncoding)
}

func decompressSingleWorker(r io.Reader, w io.Writer, qualEncoding encoder.QualityEncoding) error {
	zstdDec, err := zstd.NewReader(nil)
	if err != nil {
		return fmt.Errorf("creating zstd decoder: %w", err)
	}
	defer zstdDec.Close()

	for {
		blockHeader, err := format.ReadBlockHeader(r)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("reading block header: %w", err)
		}

		if err := decompressBlockToWriter(blockHeader, r, w, zstdDec, qualEncoding); err != nil {
			return fmt.Errorf("decompressing block: %w", err)
		}
	}

	return nil
}

func decompressParallel(r io.Reader, w io.Writer, workers int, qualEncoding encoder.QualityEncoding) error {
	jobs := make(chan decompressJob, workers*2)
	results := make(chan decompressResult, workers*2)

	g, ctx := errgroup.WithContext(ctx())

	// Start workers
	for range workers {
		g.Go(func() error {
			return runDecompressionWorker(ctx, jobs, results, qualEncoding)
		})
	}

	// Producer: read blocks and dispatch
	g.Go(func() error {
		defer close(jobs)
		return produceDecompressJobs(ctx, r, jobs)
	})

	// Collector: write results in order
	var collectorErr error
	collectorDone := make(chan struct{})
	go func() {
		defer close(collectorDone)
		collectorErr = collectAndWriteDecompressResults(results, w)
	}()

	// Wait for workers and producer
	workerErr := g.Wait()
	close(results)

	// Wait for collector
	<-collectorDone

	if workerErr != nil {
		return workerErr
	}
	return collectorErr
}

func runDecompressionWorker(ctx context.Context, jobs <-chan decompressJob, results chan<- decompressResult, qualEncoding encoder.QualityEncoding) error {
	zstdDec, err := zstd.NewReader(nil)
	if err != nil {
		return fmt.Errorf("creating zstd decoder: %w", err)
	}
	defer zstdDec.Close()

	for job := range jobs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		data, err := decompressJobToBytes(job, zstdDec, qualEncoding)
		results <- decompressResult{seqNum: job.seqNum, data: data, err: err}
	}
	return nil
}

func produceDecompressJobs(ctx context.Context, r io.Reader, jobs chan<- decompressJob) error {
	seqNum := 0
	for {
		blockHeader, err := format.ReadBlockHeader(r)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading block header: %w", err)
		}

		// Read all compressed data for this block
		compressed := [][]byte{
			make([]byte, blockHeader.SeqDataSize),
			make([]byte, blockHeader.QualDataSize),
			make([]byte, blockHeader.HeaderDataSize),
			make([]byte, blockHeader.NPositionsSize),
			make([]byte, blockHeader.SeqLengthsSize),
		}

		for _, buf := range compressed {
			if _, err := io.ReadFull(r, buf); err != nil {
				return fmt.Errorf("reading compressed data: %w", err)
			}
		}

		select {
		case jobs <- decompressJob{seqNum: seqNum, header: blockHeader, compressed: compressed}:
			seqNum++
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func decompressJobToBytes(job decompressJob, zstdDec *zstd.Decoder, qualEncoding encoder.QualityEncoding) ([]byte, error) {
	// Decompress each stream
	seqData, err := zstdDec.DecodeAll(job.compressed[0], nil)
	if err != nil {
		return nil, fmt.Errorf("decompressing sequences: %w", err)
	}
	qualData, err := zstdDec.DecodeAll(job.compressed[1], nil)
	if err != nil {
		return nil, fmt.Errorf("decompressing quality: %w", err)
	}
	headerData, err := zstdDec.DecodeAll(job.compressed[2], nil)
	if err != nil {
		return nil, fmt.Errorf("decompressing headers: %w", err)
	}
	nPosData, err := zstdDec.DecodeAll(job.compressed[3], nil)
	if err != nil {
		return nil, fmt.Errorf("decompressing N positions: %w", err)
	}
	lengthData, err := zstdDec.DecodeAll(job.compressed[4], nil)
	if err != nil {
		return nil, fmt.Errorf("decompressing lengths: %w", err)
	}

	data := &blockData{
		seqData:    seqData,
		qualData:   qualData,
		headerData: headerData,
		nPosData:   nPosData,
		lengthData: lengthData,
	}

	// Format into FASTQ
	var buf bytes.Buffer
	br := &blockReader{data: data, qualEncoding: qualEncoding}
	for range job.header.NumRecords {
		if err := br.writeRecord(&buf); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

// blockData holds decompressed data for a block.
type blockData struct {
	seqData    []byte
	qualData   []byte
	headerData []byte
	nPosData   []byte
	lengthData []byte
}

// blockReader tracks offsets while reading block data.
type blockReader struct {
	data         *blockData
	seqOffset    int
	qualOffset   int
	headerOffset int
	nPosOffset   int
	lengthOffset int
	qualEncoding encoder.QualityEncoding
}

func decompressBlockToWriter(header *format.BlockHeader, r io.Reader, w io.Writer, zstdDec *zstd.Decoder, qualEncoding encoder.QualityEncoding) error {
	data, err := readAndDecompressBlock(header, r, zstdDec)
	if err != nil {
		return err
	}

	br := &blockReader{data: data, qualEncoding: qualEncoding}
	for range header.NumRecords {
		if err := br.writeRecord(w); err != nil {
			return err
		}
	}

	return nil
}

func readAndDecompressBlock(header *format.BlockHeader, r io.Reader, zstdDec *zstd.Decoder) (*blockData, error) {
	// Read compressed data
	buffers := [][]byte{
		make([]byte, header.SeqDataSize),
		make([]byte, header.QualDataSize),
		make([]byte, header.HeaderDataSize),
		make([]byte, header.NPositionsSize),
		make([]byte, header.SeqLengthsSize),
	}

	for _, buf := range buffers {
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
	}

	// Decompress each stream
	seqData, err := zstdDec.DecodeAll(buffers[0], nil)
	if err != nil {
		return nil, fmt.Errorf("decompressing sequences: %w", err)
	}
	qualData, err := zstdDec.DecodeAll(buffers[1], nil)
	if err != nil {
		return nil, fmt.Errorf("decompressing quality: %w", err)
	}
	headerData, err := zstdDec.DecodeAll(buffers[2], nil)
	if err != nil {
		return nil, fmt.Errorf("decompressing headers: %w", err)
	}
	nPosData, err := zstdDec.DecodeAll(buffers[3], nil)
	if err != nil {
		return nil, fmt.Errorf("decompressing N positions: %w", err)
	}
	lengthData, err := zstdDec.DecodeAll(buffers[4], nil)
	if err != nil {
		return nil, fmt.Errorf("decompressing lengths: %w", err)
	}

	return &blockData{
		seqData:    seqData,
		qualData:   qualData,
		headerData: headerData,
		nPosData:   nPosData,
		lengthData: lengthData,
	}, nil
}

func (br *blockReader) writeRecord(w io.Writer) error {
	seqLen, err := br.readSeqLength()
	if err != nil {
		return err
	}

	nPos, err := br.readNPositions()
	if err != nil {
		return err
	}

	seq, err := br.readSequence(seqLen, nPos)
	if err != nil {
		return err
	}

	qual, err := br.readQuality(seqLen)
	if err != nil {
		return err
	}

	recHeader, err := br.readHeader()
	if err != nil {
		return err
	}

	// Write FASTQ record using direct writes instead of fmt.Fprintf
	// to avoid format string parsing and interface dispatch per record.
	if bw, ok := w.(*bytes.Buffer); ok {
		// Fast path for bytes.Buffer (parallel decompression)
		bw.WriteByte('@')
		bw.Write(recHeader)
		bw.WriteByte('\n')
		bw.Write(seq)
		bw.WriteByte('\n')
		bw.WriteByte('+')
		bw.WriteByte('\n')
		bw.Write(qual)
		bw.WriteByte('\n')
		return nil
	}
	// General io.Writer path — build record into scratch buffer
	needed := 1 + len(recHeader) + 1 + len(seq) + 3 + len(qual) + 1
	buf := make([]byte, 0, needed)
	buf = append(buf, '@')
	buf = append(buf, recHeader...)
	buf = append(buf, '\n')
	buf = append(buf, seq...)
	buf = append(buf, '\n', '+', '\n')
	buf = append(buf, qual...)
	buf = append(buf, '\n')
	_, err = w.Write(buf)
	return err
}

func (br *blockReader) readSeqLength() (int, error) {
	if br.lengthOffset+4 > len(br.data.lengthData) {
		return 0, errors.New("truncated length data")
	}
	seqLen := int(binary.LittleEndian.Uint32(br.data.lengthData[br.lengthOffset : br.lengthOffset+4]))
	br.lengthOffset += 4
	return seqLen, nil
}

func (br *blockReader) readNPositions() ([]uint16, error) {
	if br.nPosOffset+2 > len(br.data.nPosData) {
		return nil, errors.New("truncated N position data")
	}
	nCount := int(binary.LittleEndian.Uint16(br.data.nPosData[br.nPosOffset : br.nPosOffset+2]))
	br.nPosOffset += 2

	nPos := make([]uint16, nCount)
	for j := range nCount {
		if br.nPosOffset+2 > len(br.data.nPosData) {
			return nil, errors.New("truncated N position data")
		}
		nPos[j] = binary.LittleEndian.Uint16(br.data.nPosData[br.nPosOffset : br.nPosOffset+2])
		br.nPosOffset += 2
	}
	return nPos, nil
}

func (br *blockReader) readSequence(seqLen int, nPos []uint16) ([]byte, error) {
	packedLen := (seqLen + 3) / 4
	if br.seqOffset+packedLen > len(br.data.seqData) {
		return nil, errors.New("truncated sequence data")
	}
	seq := encoder.UnpackBases(br.data.seqData[br.seqOffset:br.seqOffset+packedLen], nPos, seqLen)
	br.seqOffset += packedLen
	return seq, nil
}

func (br *blockReader) readQuality(seqLen int) ([]byte, error) {
	if br.qualOffset+seqLen > len(br.data.qualData) {
		return nil, errors.New("truncated quality data")
	}
	qual := make([]byte, seqLen)
	copy(qual, br.data.qualData[br.qualOffset:br.qualOffset+seqLen])
	encoder.DeltaDecode(qual)
	encoder.DenormalizeQuality(qual, br.qualEncoding)
	br.qualOffset += seqLen
	return qual, nil
}

func (br *blockReader) readHeader() ([]byte, error) {
	if br.headerOffset+2 > len(br.data.headerData) {
		return nil, errors.New("truncated header data")
	}
	headerLen := int(binary.LittleEndian.Uint16(br.data.headerData[br.headerOffset : br.headerOffset+2]))
	br.headerOffset += 2

	if br.headerOffset+headerLen > len(br.data.headerData) {
		return nil, errors.New("truncated header data")
	}
	recHeader := br.data.headerData[br.headerOffset : br.headerOffset+headerLen]
	br.headerOffset += headerLen
	return recHeader, nil
}
