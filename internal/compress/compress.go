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

// DefaultBlockSize is the default number of records per block.
const DefaultBlockSize = 100000

// batchPool recycles RecordBatch objects to reduce allocations.
var batchPool = sync.Pool{
	New: func() interface{} {
		return &parser.RecordBatch{
			Records: make([]parser.Record, 0, DefaultBlockSize),
			Data:    make([]byte, 0, DefaultBlockSize*500), // ~500 bytes per record estimation
		}
	},
}

// bufferPool recycles bytes.Buffer objects for output construction.
var bufferPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

func getBatch() *parser.RecordBatch {
	return batchPool.Get().(*parser.RecordBatch)
}

func putBatch(b *parser.RecordBatch) {
	b.Reset()
	batchPool.Put(b)
}

func getBuffer() *bytes.Buffer {
	b := bufferPool.Get().(*bytes.Buffer)
	b.Reset()
	return b
}

func putBuffer(b *bytes.Buffer) {
	if b != nil {
		bufferPool.Put(b)
	}
}

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
	seqNum int
	batch  *parser.RecordBatch
}

// compressResult represents a compressed block.
type compressResult struct {
	seqNum int
	data   []byte
	buf    *bytes.Buffer // underlying buffer to be recycled
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
	buf    *bytes.Buffer // underlying buffer to be recycled
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
	firstBatch := getBatch()

	// Ensure capacity for requested block size
	if cap(firstBatch.Records) < int(opts.BlockSize) {
		firstBatch.Records = make([]parser.Record, 0, opts.BlockSize)
	}

	err := p.ReadBatch(firstBatch, int(opts.BlockSize))
	// Check for EOF is implicit in len(Records) == 0, but ReadBatch might return error
	if err != nil && !errors.Is(err, io.EOF) {
		putBatch(firstBatch)
		return fmt.Errorf("parsing FASTQ: %w", err)
	}

	firstBatchEOF := false
	if len(firstBatch.Records) < int(opts.BlockSize) {
		firstBatchEOF = true
	}
	if errors.Is(err, io.EOF) {
		firstBatchEOF = true
	}

	// Detect encoding from first batch
	qualEncoding := encoder.EncodingPhred33
	if len(firstBatch.Records) > 0 {
		qualities := make([][]byte, len(firstBatch.Records))
		for i := range firstBatch.Records {
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
		putBatch(firstBatch)
		return fmt.Errorf("writing file header: %w", err)
	}

	// Single worker path (simpler, no goroutine overhead)
	if opts.Workers == 1 {
		err := compressSingleWorkerWithBatch(firstBatch, p, w, opts, qualEncoding, firstBatchEOF)
		putBatch(firstBatch) // Ensure we return it
		return err
	}

	return compressParallelWithBatch(firstBatch, p, w, opts, qualEncoding, firstBatchEOF)
}

func compressSingleWorkerWithBatch(firstBatch *parser.RecordBatch, p *parser.Parser, w io.Writer, opts *Options, qualEncoding encoder.QualityEncoding, firstBatchEOF bool) error {
	zstdEnc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return fmt.Errorf("creating zstd encoder: %w", err)
	}
	defer zstdEnc.Close() //nolint:errcheck // encoder close during cleanup

	// Process first batch if present
	if len(firstBatch.Records) > 0 {
		if blockErr := compressBlock(firstBatch.Records, w, zstdEnc, qualEncoding); blockErr != nil {
			return fmt.Errorf("compressing block: %w", blockErr)
		}
	}

	if firstBatchEOF {
		return nil
	}

	// Reuse a batch for subsequent reads
	batch := getBatch()
	defer putBatch(batch)

	for {
		err := p.ReadBatch(batch, int(opts.BlockSize))
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("parsing FASTQ: %w", err)
		}
		if len(batch.Records) == 0 {
			break
		}

		if blockErr := compressBlock(batch.Records, w, zstdEnc, qualEncoding); blockErr != nil {
			return fmt.Errorf("compressing block: %w", blockErr)
		}

		if errors.Is(err, io.EOF) {
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
			putBatch(job.batch) // cleanup
			return ctx.Err()
		default:
		}

		data, buf, err := compressBlockToBytes(job.batch.Records, zstdEnc, qualEncoding)
		putBatch(job.batch) // Return batch to pool after processing

		results <- compressResult{seqNum: job.seqNum, data: data, buf: buf, err: err}
	}
	return nil
}

func produceCompressJobs(ctx context.Context, jobs chan<- compressJob, firstBatch *parser.RecordBatch, p *parser.Parser, blockSize uint32, firstBatchEOF bool) error {
	seqNum := 0

	// Send first batch if present
	if len(firstBatch.Records) > 0 {
		select {
		case jobs <- compressJob{seqNum: seqNum, batch: firstBatch}:
			seqNum++
		case <-ctx.Done():
			putBatch(firstBatch) // cleanup
			return ctx.Err()
		}
	} else {
		// If empty, return it to pool
		putBatch(firstBatch)
	}

	if firstBatchEOF {
		return nil
	}

	for {
		batch := getBatch()
		// Ensure capacity
		if cap(batch.Records) < int(blockSize) {
			batch.Records = make([]parser.Record, 0, blockSize)
		}

		err := p.ReadBatch(batch, int(blockSize))
		if err != nil && !errors.Is(err, io.EOF) {
			putBatch(batch)
			return fmt.Errorf("parsing FASTQ: %w", err)
		}
		if len(batch.Records) == 0 {
			putBatch(batch)
			break
		}

		select {
		case jobs <- compressJob{seqNum: seqNum, batch: batch}:
			seqNum++
		case <-ctx.Done():
			putBatch(batch)
			return ctx.Err()
		}

		if errors.Is(err, io.EOF) {
			break
		}
	}
	return nil
}

func collectAndWriteResults(results <-chan compressResult, w io.Writer) error {
	pending := make(map[int]compressResult)
	nextSeqNum := 0

	for result := range results {
		if result.err != nil {
			return fmt.Errorf("compressing block %d: %w", result.seqNum, result.err)
		}

		pending[result.seqNum] = result

		// Write all sequential results available
		for {
			res, ok := pending[nextSeqNum]
			if !ok {
				break
			}
			if _, err := w.Write(res.data); err != nil {
				return fmt.Errorf("writing block %d: %w", nextSeqNum, err)
			}
			putBuffer(res.buf)
			delete(pending, nextSeqNum)
			nextSeqNum++
		}
	}

	return nil
}

func collectAndWriteDecompressResults(results <-chan decompressResult, w io.Writer) error {
	pending := make(map[int]decompressResult)
	nextSeqNum := 0

	for result := range results {
		if result.err != nil {
			return fmt.Errorf("decompressing block %d: %w", result.seqNum, result.err)
		}

		pending[result.seqNum] = result

		// Write all sequential results available
		for {
			res, ok := pending[nextSeqNum]
			if !ok {
				break
			}
			if _, err := w.Write(res.data); err != nil {
				return fmt.Errorf("writing block %d: %w", nextSeqNum, err)
			}
			putBuffer(res.buf)
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
func compressBlockToBytes(records []parser.Record, zstdEnc *zstd.Encoder, qualEncoding encoder.QualityEncoding) ([]byte, *bytes.Buffer, error) {
	buf := getBuffer()
	if err := compressBlock(records, buf, zstdEnc, qualEncoding); err != nil {
		putBuffer(buf)
		return nil, nil, err
	}
	return buf.Bytes(), buf, nil
}

func compressBlock(records []parser.Record, w io.Writer, zstdEnc *zstd.Encoder, qualEncoding encoder.QualityEncoding) error {
	// Estimate sizes for preallocation (typical 150bp reads)
	estimatedSeqBytes := len(records) * 40     // ~150bp / 4 bases per byte
	estimatedQualBytes := len(records) * 150   // 1 byte per base
	estimatedHeaderBytes := len(records) * 100 // typical header size

	allSeqPacked := make([]byte, 0, estimatedSeqBytes)
	allNPositions := make([]byte, 0, len(records)*4) // minimum 2 bytes per record
	allSeqLengths := make([]byte, 0, len(records)*4) // exactly 4 bytes per record
	allQuality := make([]byte, 0, estimatedQualBytes)
	allHeaders := make([]byte, 0, estimatedHeaderBytes)

	var originalSeqSize, originalQualSize uint32
	var nPosRec []uint16 // reused buffer for N positions per record

	for i := range records {
		rec := &records[i]

		// Encode sequence
		// Use AppendPackedBases to avoid allocations
		nPosRec = nPosRec[:0]
		allSeqPacked, nPosRec = encoder.AppendPackedBases(allSeqPacked, nPosRec, rec.Sequence)

		// Store N positions: count (uint16) + positions (uint16 each)
		allNPositions = binary.LittleEndian.AppendUint16(allNPositions, uint16(len(nPosRec)))
		for _, pos := range nPosRec {
			allNPositions = binary.LittleEndian.AppendUint16(allNPositions, pos)
		}

		// Store sequence length
		allSeqLengths = binary.LittleEndian.AppendUint32(allSeqLengths, uint32(len(rec.Sequence))) //nolint:gosec // seq length bounded

		originalSeqSize += uint32(len(rec.Sequence)) //nolint:gosec // bounded

		// Encode quality: normalize to 0-based, then delta encode
		// Append directly to avoid allocation, then modify in-place
		qualStart := len(allQuality)
		allQuality = append(allQuality, rec.Quality...)
		qualSlice := allQuality[qualStart:]

		encoder.NormalizeQuality(qualSlice, qualEncoding)
		encoder.DeltaEncode(qualSlice)

		originalQualSize += uint32(len(rec.Quality)) //nolint:gosec // bounded

		// Store header with length prefix
		allHeaders = binary.LittleEndian.AppendUint16(allHeaders, uint16(len(rec.Header))) //nolint:gosec // header length bounded
		allHeaders = append(allHeaders, rec.Header...)
	}

	// Compress each stream with zstd
	compressedSeq := zstdEnc.EncodeAll(allSeqPacked, nil)
	compressedQual := zstdEnc.EncodeAll(allQuality, nil)
	compressedHeaders := zstdEnc.EncodeAll(allHeaders, nil)
	compressedNPos := zstdEnc.EncodeAll(allNPositions, nil)
	compressedLengths := zstdEnc.EncodeAll(allSeqLengths, nil)

	// Write block header
	//nolint:gosec // All lengths are bounded by block size and data sizes
	blockHeader := format.BlockHeader{
		NumRecords:       uint32(len(records)),
		SeqDataSize:      uint32(len(compressedSeq)),
		QualDataSize:     uint32(len(compressedQual)),
		HeaderDataSize:   uint32(len(compressedHeaders)),
		NPositionsSize:   uint32(len(compressedNPos)),
		SeqLengthsSize:   uint32(len(compressedLengths)),
		OriginalSeqSize:  originalSeqSize,
		OriginalQualSize: originalQualSize,
	}
	if err := blockHeader.Write(w); err != nil {
		return err
	}

	// Write compressed data
	for _, data := range [][]byte{compressedSeq, compressedQual, compressedHeaders, compressedNPos, compressedLengths} {
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

		data, buf, err := decompressJobToBytes(job, zstdDec, qualEncoding)
		results <- decompressResult{seqNum: job.seqNum, data: data, buf: buf, err: err}
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

func decompressJobToBytes(job decompressJob, zstdDec *zstd.Decoder, qualEncoding encoder.QualityEncoding) ([]byte, *bytes.Buffer, error) {
	// Decompress each stream
	seqData, err := zstdDec.DecodeAll(job.compressed[0], nil)
	if err != nil {
		return nil, nil, fmt.Errorf("decompressing sequences: %w", err)
	}
	qualData, err := zstdDec.DecodeAll(job.compressed[1], nil)
	if err != nil {
		return nil, nil, fmt.Errorf("decompressing quality: %w", err)
	}
	headerData, err := zstdDec.DecodeAll(job.compressed[2], nil)
	if err != nil {
		return nil, nil, fmt.Errorf("decompressing headers: %w", err)
	}
	nPosData, err := zstdDec.DecodeAll(job.compressed[3], nil)
	if err != nil {
		return nil, nil, fmt.Errorf("decompressing N positions: %w", err)
	}
	lengthData, err := zstdDec.DecodeAll(job.compressed[4], nil)
	if err != nil {
		return nil, nil, fmt.Errorf("decompressing lengths: %w", err)
	}

	data := &blockData{
		seqData:    seqData,
		qualData:   qualData,
		headerData: headerData,
		nPosData:   nPosData,
		lengthData: lengthData,
	}

	// Format into FASTQ
	buf := getBuffer()

	br := &blockReader{data: data, qualEncoding: qualEncoding}
	for range job.header.NumRecords {
		if err := br.writeRecord(buf); err != nil {
			putBuffer(buf)
			return nil, nil, err
		}
	}

	return buf.Bytes(), buf, nil
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

	// Use pooled buffer for formatting
	buf := getBuffer()
	defer putBuffer(buf)

	br := &blockReader{data: data, qualEncoding: qualEncoding}
	for range header.NumRecords {
		// If buffer gets too large, flush it
		if buf.Len() > 1<<20 { // 1MB
			if _, err := w.Write(buf.Bytes()); err != nil {
				return err
			}
			buf.Reset()
		}

		if err := br.writeRecord(buf); err != nil {
			return err
		}
	}

	// Write remaining
	if buf.Len() > 0 {
		if _, err := w.Write(buf.Bytes()); err != nil {
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

func (br *blockReader) writeRecord(buf *bytes.Buffer) error {
	seqLen, err := br.readSeqLength()
	if err != nil {
		return err
	}

	nPos, err := br.readNPositions()
	if err != nil {
		return err
	}

	// @
	buf.WriteByte('@')
	if err := br.appendHeader(buf); err != nil {
		return err
	}
	buf.WriteByte('\n')

	// Sequence
	if err := br.appendSequence(buf, seqLen, nPos); err != nil {
		return err
	}
	buf.WriteByte('\n')

	// +
	buf.WriteString("+\n")

	// Quality
	if err := br.appendQuality(buf, seqLen); err != nil {
		return err
	}
	buf.WriteByte('\n')

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

func (br *blockReader) appendSequence(buf *bytes.Buffer, seqLen int, nPos []uint16) error {
	packedLen := (seqLen + 3) / 4
	if br.seqOffset+packedLen > len(br.data.seqData) {
		return errors.New("truncated sequence data")
	}

	// Ensure capacity in buffer for direct write (optimization)
	buf.Grow(seqLen)

	// Get available buffer space
	// Go 1.21+ has AvailableBuffer(), but we can assume we are on modern Go.
	// However, `bytes.Buffer` doesn't let us write directly to unexported `buf`.
	// But `encoder.AppendUnpackBases` appends to a slice.
	// We can use `buf.Write` with a slice returned by `AppendUnpackBases`?
	// No, that allocates the slice if we pass `nil`.
	// We want to write directly into `buf`'s memory if possible.
	// `bytes.Buffer` doesn't expose mutable slice of free space easily.
	//
	// Alternative: `encoder.AppendUnpackBases` takes `[]byte`.
	// We can pass a scratch buffer? But we want to write to `buf`.

	// Let's use `AvailableBuffer` pattern if available, or just rely on `Write`.
	// To be zero-alloc, we need a scratch buffer or write directly.
	// Since we are pooling `bytes.Buffer`, maybe we can abuse it? No.

	// Let's use a temporary scratch slice from a pool?
	// Or just allocate for now? No, we want zero alloc.

	// Wait, `encoder.AppendUnpackBases` works on `[]byte`.
	// If we use `buf.AvailableBuffer()` (added in Go 1.21), we get a slice.
	// Does `go.mod` say 1.24? Yes.
	dst := buf.AvailableBuffer()
	dst = encoder.AppendUnpackBases(dst, br.data.seqData[br.seqOffset:br.seqOffset+packedLen], nPos, seqLen)
	buf.Write(dst)

	br.seqOffset += packedLen
	return nil
}

func (br *blockReader) appendQuality(buf *bytes.Buffer, seqLen int) error {
	if br.qualOffset+seqLen > len(br.data.qualData) {
		return errors.New("truncated quality data")
	}

	// We modify the quality data IN PLACE in the blockData!
	// This is safe because blockData is created fresh for each block
	// and discarded after use (it's not pooled, but the byte slices inside come from zstd).
	// zstd DecodeAll returns a new slice or appends.

	qual := br.data.qualData[br.qualOffset : br.qualOffset+seqLen]
	encoder.DeltaDecode(qual)
	encoder.DenormalizeQuality(qual, br.qualEncoding)

	buf.Write(qual)

	br.qualOffset += seqLen
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
	buf.Write(br.data.headerData[br.headerOffset : br.headerOffset+headerLen])
	br.headerOffset += headerLen
	return nil
}
