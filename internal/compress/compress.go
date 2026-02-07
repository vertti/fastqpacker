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
	seqPacked  []byte
	nPositions []byte
	nPosBuf    []uint16 // reusable N position slice per record
	seqLengths []byte
	quality    []byte
	headers    []byte
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
		return &blockBuffers{}
	},
}

var batchPool = sync.Pool{
	New: func() any {
		return parser.NewRecordBatch(DefaultBlockSize)
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
	records []parser.Record
	batch   *parser.RecordBatch // non-nil if batch should be returned to pool
}

// compressResult represents a compressed block.
type compressResult struct {
	seqNum int
	bufs   *blockBuffers
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
	buf    *bytes.Buffer
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
	firstBatch := batchPool.Get().(*parser.RecordBatch) //nolint:errcheck // pool always returns *RecordBatch
	err := p.ReadBatch(firstBatch)
	firstBatchEOF := errors.Is(err, io.EOF)
	if err != nil && !firstBatchEOF {
		batchPool.Put(firstBatch)
		return fmt.Errorf("parsing FASTQ: %w", err)
	}

	// Detect encoding from first batch
	qualEncoding := encoder.EncodingPhred33
	if firstBatch.Len() > 0 {
		qualities := make([][]byte, firstBatch.Len())
		for i := range firstBatch.Len() {
			qualities[i] = firstBatch.Records[i].Quality
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
		batchPool.Put(firstBatch)
		return fmt.Errorf("writing file header: %w", err)
	}

	// Single worker path (simpler, no goroutine overhead)
	if opts.Workers == 1 {
		return compressSingleWorkerWithBatch(firstBatch, p, w, qualEncoding, firstBatchEOF)
	}

	return compressParallelWithBatch(firstBatch, p, w, opts, qualEncoding, firstBatchEOF)
}

func compressSingleWorkerWithBatch(firstBatch *parser.RecordBatch, p *parser.Parser, w io.Writer, qualEncoding encoder.QualityEncoding, firstBatchEOF bool) error {
	zstdEnc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		batchPool.Put(firstBatch)
		return fmt.Errorf("creating zstd encoder: %w", err)
	}
	defer zstdEnc.Close() //nolint:errcheck // encoder close during cleanup

	// Process first batch if present
	if firstBatch.Len() > 0 {
		if blockErr := compressBlock(firstBatch.Records[:firstBatch.Len()], w, zstdEnc, qualEncoding); blockErr != nil {
			batchPool.Put(firstBatch)
			return fmt.Errorf("compressing block: %w", blockErr)
		}
	}
	batchPool.Put(firstBatch)

	if firstBatchEOF {
		return nil
	}

	batch := batchPool.Get().(*parser.RecordBatch) //nolint:errcheck // pool always returns *RecordBatch
	defer batchPool.Put(batch)

	for {
		err := p.ReadBatch(batch)
		isEOF := errors.Is(err, io.EOF)
		if err != nil && !isEOF {
			return fmt.Errorf("parsing FASTQ: %w", err)
		}
		if batch.Len() == 0 {
			break
		}

		if blockErr := compressBlock(batch.Records[:batch.Len()], w, zstdEnc, qualEncoding); blockErr != nil {
			return fmt.Errorf("compressing block: %w", blockErr)
		}

		if isEOF {
			break
		}
	}

	return nil
}

func compressParallelWithBatch(firstBatch *parser.RecordBatch, p *parser.Parser, w io.Writer, opts *Options, qualEncoding encoder.QualityEncoding, firstBatchEOF bool) error {
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
		return produceCompressJobs(ctx, jobs, firstBatch, p, firstBatchEOF)
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

		bufs, err := compressBlockToPooledBuffer(job.records, zstdEnc, qualEncoding)
		if job.batch != nil {
			batchPool.Put(job.batch)
		}
		results <- compressResult{seqNum: job.seqNum, bufs: bufs, err: err}
	}
	return nil
}

func produceCompressJobs(ctx context.Context, jobs chan<- compressJob, firstBatch *parser.RecordBatch, p *parser.Parser, firstBatchEOF bool) error {
	seqNum := 0

	// Send first batch if present
	if firstBatch.Len() > 0 {
		select {
		case jobs <- compressJob{seqNum: seqNum, records: firstBatch.Records[:firstBatch.Len()], batch: firstBatch}:
			seqNum++
		case <-ctx.Done():
			batchPool.Put(firstBatch)
			return ctx.Err()
		}
	} else {
		batchPool.Put(firstBatch)
	}

	if firstBatchEOF {
		return nil
	}

	for {
		batch := batchPool.Get().(*parser.RecordBatch) //nolint:errcheck // pool always returns *RecordBatch
		err := p.ReadBatch(batch)
		isEOF := errors.Is(err, io.EOF)
		if err != nil && !isEOF {
			batchPool.Put(batch)
			return fmt.Errorf("parsing FASTQ: %w", err)
		}
		if batch.Len() == 0 {
			batchPool.Put(batch)
			break
		}

		select {
		case jobs <- compressJob{seqNum: seqNum, records: batch.Records[:batch.Len()], batch: batch}:
			seqNum++
		case <-ctx.Done():
			batchPool.Put(batch)
			return ctx.Err()
		}

		if isEOF {
			break
		}
	}
	return nil
}

func collectAndWriteResults(results <-chan compressResult, w io.Writer) error {
	pending := make(map[int]*blockBuffers)
	nextSeqNum := 0

	for result := range results {
		if result.err != nil {
			if result.bufs != nil {
				blockBufferPool.Put(result.bufs)
			}
			for _, bufs := range pending {
				blockBufferPool.Put(bufs)
			}
			return fmt.Errorf("compressing block %d: %w", result.seqNum, result.err)
		}

		pending[result.seqNum] = result.bufs

		// Write all sequential results available
		for {
			bufs, ok := pending[nextSeqNum]
			if !ok {
				break
			}
			if _, err := w.Write(bufs.outputBuf.Bytes()); err != nil {
				blockBufferPool.Put(bufs)
				delete(pending, nextSeqNum)
				for _, pendingBufs := range pending {
					blockBufferPool.Put(pendingBufs)
				}
				return fmt.Errorf("writing block %d: %w", nextSeqNum, err)
			}
			blockBufferPool.Put(bufs)
			delete(pending, nextSeqNum)
			nextSeqNum++
		}
	}

	return nil
}

func collectAndWriteDecompressResults(results <-chan decompressResult, w io.Writer) error {
	pending := make(map[int]*bytes.Buffer)
	nextSeqNum := 0

	for result := range results {
		if result.err != nil {
			if result.buf != nil {
				decompBufPool.Put(result.buf)
			}
			for _, buf := range pending {
				decompBufPool.Put(buf)
			}
			return fmt.Errorf("decompressing block %d: %w", result.seqNum, result.err)
		}

		pending[result.seqNum] = result.buf

		// Write all sequential results available
		for {
			buf, ok := pending[nextSeqNum]
			if !ok {
				break
			}
			if _, err := w.Write(buf.Bytes()); err != nil {
				decompBufPool.Put(buf)
				delete(pending, nextSeqNum)
				for _, pendingBuf := range pending {
					decompBufPool.Put(pendingBuf)
				}
				return fmt.Errorf("writing block %d: %w", nextSeqNum, err)
			}
			decompBufPool.Put(buf)
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

// compressBlockToPooledBuffer compresses a block and returns a pooled buffer.
// Caller must return the buffer to blockBufferPool when done.
func compressBlockToPooledBuffer(records []parser.Record, zstdEnc *zstd.Encoder, qualEncoding encoder.QualityEncoding) (*blockBuffers, error) {
	bufs := blockBufferPool.Get().(*blockBuffers) //nolint:errcheck // pool always returns *blockBuffers
	bufs.reset()

	if err := compressBlockWithBuffers(records, &bufs.outputBuf, zstdEnc, qualEncoding, bufs); err != nil {
		blockBufferPool.Put(bufs)
		return nil, err
	}
	return bufs, nil
}

func compressBlock(records []parser.Record, w io.Writer, zstdEnc *zstd.Encoder, qualEncoding encoder.QualityEncoding) error {
	bufs := blockBufferPool.Get().(*blockBuffers) //nolint:errcheck // pool always returns *blockBuffers
	bufs.reset()
	defer blockBufferPool.Put(bufs)

	return compressBlockWithBuffers(records, w, zstdEnc, qualEncoding, bufs)
}

func compressBlockWithBuffers(records []parser.Record, w io.Writer, zstdEnc *zstd.Encoder, qualEncoding encoder.QualityEncoding, bufs *blockBuffers) error {
	var originalSeqSize, originalQualSize uint32

	for i := range records {
		rec := &records[i]

		// Encode sequence using append-style (no per-record allocation)
		bufs.nPosBuf = bufs.nPosBuf[:0]
		bufs.seqPacked = encoder.AppendPackedBases(bufs.seqPacked, rec.Sequence, &bufs.nPosBuf)

		// Store N positions: count (uint16) + positions (uint16 each)
		bufs.nPositions = binary.LittleEndian.AppendUint16(bufs.nPositions, uint16(len(bufs.nPosBuf))) //nolint:gosec // bounded
		for _, pos := range bufs.nPosBuf {
			bufs.nPositions = binary.LittleEndian.AppendUint16(bufs.nPositions, pos)
		}

		// Store sequence length
		bufs.seqLengths = binary.LittleEndian.AppendUint32(bufs.seqLengths, uint32(len(rec.Sequence))) //nolint:gosec // bounded

		originalSeqSize += uint32(len(rec.Sequence)) //nolint:gosec // bounded

		// Encode quality: append into buffer, then normalize+delta the tail in-place
		qualStart := len(bufs.quality)
		bufs.quality = append(bufs.quality, rec.Quality...)
		qualSlice := bufs.quality[qualStart:]
		encoder.NormalizeQuality(qualSlice, qualEncoding)
		encoder.DeltaEncode(qualSlice)
		originalQualSize += uint32(len(rec.Quality)) //nolint:gosec // bounded

		// Store header with length prefix
		bufs.headers = binary.LittleEndian.AppendUint16(bufs.headers, uint16(len(rec.Header))) //nolint:gosec // bounded
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

		buf, err := decompressJobToPooledBuffer(job, zstdDec, qualEncoding)
		results <- decompressResult{seqNum: job.seqNum, buf: buf, err: err}
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

func decompressJobToPooledBuffer(job decompressJob, zstdDec *zstd.Decoder, qualEncoding encoder.QualityEncoding) (*bytes.Buffer, error) {
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

	// Format into FASTQ using pooled buffer
	buf := decompBufPool.Get().(*bytes.Buffer) //nolint:errcheck // pool always returns *bytes.Buffer
	buf.Reset()

	br := &blockReader{data: data, qualEncoding: qualEncoding}
	for range job.header.NumRecords {
		if err := br.writeRecord(buf); err != nil {
			decompBufPool.Put(buf)
			return nil, err
		}
	}
	return buf, nil
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
	nPosBuf      []uint16 // reusable N position buffer
}

var decompBufPool = sync.Pool{
	New: func() any {
		b := &bytes.Buffer{}
		b.Grow(1 << 20) // 1MB initial
		return b
	},
}

func decompressBlockToWriter(header *format.BlockHeader, r io.Reader, w io.Writer, zstdDec *zstd.Decoder, qualEncoding encoder.QualityEncoding) error {
	data, err := readAndDecompressBlock(header, r, zstdDec)
	if err != nil {
		return err
	}

	buf := decompBufPool.Get().(*bytes.Buffer) //nolint:errcheck // pool always returns *bytes.Buffer
	buf.Reset()

	br := &blockReader{data: data, qualEncoding: qualEncoding}
	for range header.NumRecords {
		if err := br.writeRecord(buf); err != nil {
			decompBufPool.Put(buf)
			return err
		}
	}

	_, err = w.Write(buf.Bytes())
	decompBufPool.Put(buf)
	return err
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

func (br *blockReader) writeRecord(buf *bytes.Buffer) error {
	seqLen, err := br.readSeqLength()
	if err != nil {
		return err
	}

	nPos, err := br.readNPositions()
	if err != nil {
		return err
	}

	// Write '@' + header
	if err := br.appendHeader(buf); err != nil {
		return err
	}

	// Append sequence directly into buffer using AppendUnpackBases
	if err := br.appendSequence(buf, seqLen, nPos); err != nil {
		return err
	}

	// Write "+\n"
	buf.WriteByte('+')
	buf.WriteByte('\n')

	// Append quality with in-place delta decode (safe: qualData is per-block)
	if err := br.appendQuality(buf, seqLen); err != nil {
		return err
	}

	return nil
}

func (br *blockReader) appendHeader(buf *bytes.Buffer) error {
	if br.headerOffset+2 > len(br.data.headerData) {
		return errors.New("truncated header data")
	}
	headerLen := int(binary.LittleEndian.Uint16(br.data.headerData[br.headerOffset : br.headerOffset+2]))
	br.headerOffset += 2

	if br.headerOffset+headerLen > len(br.data.headerData) {
		return errors.New("truncated header data")
	}
	buf.WriteByte('@')
	buf.Write(br.data.headerData[br.headerOffset : br.headerOffset+headerLen])
	buf.WriteByte('\n')
	br.headerOffset += headerLen
	return nil
}

func (br *blockReader) appendSequence(buf *bytes.Buffer, seqLen int, nPos []uint16) error {
	packedLen := (seqLen + 3) / 4
	if br.seqOffset+packedLen > len(br.data.seqData) {
		return errors.New("truncated sequence data")
	}
	// Use AvailableBuffer + AppendUnpackBases for zero-copy into buffer
	avail := buf.AvailableBuffer()
	avail = encoder.AppendUnpackBases(avail, br.data.seqData[br.seqOffset:br.seqOffset+packedLen], nPos, seqLen)
	avail = append(avail, '\n')
	buf.Write(avail)
	br.seqOffset += packedLen
	return nil
}

func (br *blockReader) appendQuality(buf *bytes.Buffer, seqLen int) error {
	if br.qualOffset+seqLen > len(br.data.qualData) {
		return errors.New("truncated quality data")
	}
	// Delta decode + denormalize in-place on the block's qualData
	// (safe: qualData is only used once per record, offsets advance past it)
	qual := br.data.qualData[br.qualOffset : br.qualOffset+seqLen]
	encoder.DeltaDecode(qual)
	encoder.DenormalizeQuality(qual, br.qualEncoding)
	buf.Write(qual)
	buf.WriteByte('\n')
	br.qualOffset += seqLen
	return nil
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

	// Reuse nPosBuf to avoid per-record allocation
	if nCount == 0 {
		return nil, nil
	}
	if cap(br.nPosBuf) < nCount {
		br.nPosBuf = make([]uint16, nCount)
	}
	br.nPosBuf = br.nPosBuf[:nCount]
	for j := range nCount {
		if br.nPosOffset+2 > len(br.data.nPosData) {
			return nil, errors.New("truncated N position data")
		}
		br.nPosBuf[j] = binary.LittleEndian.Uint16(br.data.nPosData[br.nPosOffset : br.nPosOffset+2])
		br.nPosOffset += 2
	}
	return br.nPosBuf, nil
}
