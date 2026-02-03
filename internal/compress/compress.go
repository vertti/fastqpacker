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

	"github.com/klauspost/compress/zstd"
	"golang.org/x/sync/errgroup"

	"github.com/vertti/fastqpacker/internal/encoder"
	"github.com/vertti/fastqpacker/internal/format"
	"github.com/vertti/fastqpacker/internal/parser"
)

// DefaultBlockSize is the default number of records per block.
const DefaultBlockSize = 100000

// Options configures compression behavior.
type Options struct {
	BlockSize uint32 // Records per block (default: 100000)
	Workers   int    // Number of parallel compression workers (default: NumCPU)
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

	// Write file header
	header := format.FileHeader{
		Version:   1,
		BlockSize: opts.BlockSize,
		Flags:     0,
	}
	if err := header.Write(w); err != nil {
		return fmt.Errorf("writing file header: %w", err)
	}

	// Single worker path (simpler, no goroutine overhead)
	if opts.Workers == 1 {
		return compressSingleWorker(r, w, opts)
	}

	return compressParallel(r, w, opts)
}

func compressSingleWorker(r io.Reader, w io.Writer, opts *Options) error {
	zstdEnc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return fmt.Errorf("creating zstd encoder: %w", err)
	}
	defer zstdEnc.Close() //nolint:errcheck // encoder close during cleanup

	p := parser.New(r)
	for {
		batch, err := p.NextBatch(int(opts.BlockSize))
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("parsing FASTQ: %w", err)
		}
		if len(batch) == 0 {
			break
		}

		if blockErr := compressBlock(batch, w, zstdEnc); blockErr != nil {
			return fmt.Errorf("compressing block: %w", blockErr)
		}

		if errors.Is(err, io.EOF) {
			break
		}
	}

	return nil
}

func compressParallel(r io.Reader, w io.Writer, opts *Options) error {
	jobs := make(chan compressJob, opts.Workers*2)
	results := make(chan compressResult, opts.Workers*2)

	g, ctx := errgroup.WithContext(ctx())

	// Start workers
	for range opts.Workers {
		g.Go(func() error {
			// Each worker has its own zstd encoder (not thread-safe)
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

				data, err := compressBlockToBytes(job.records, zstdEnc)
				results <- compressResult{seqNum: job.seqNum, data: data, err: err}
			}
			return nil
		})
	}

	// Producer: parse and dispatch blocks
	g.Go(func() error {
		defer close(jobs)

		p := parser.New(r)
		seqNum := 0
		for {
			batch, err := p.NextBatch(int(opts.BlockSize))
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

// ctx returns a background context. Separate function to avoid import cycle.
func ctx() context.Context {
	return context.Background()
}

// compressBlockToBytes compresses a block and returns the serialized bytes.
func compressBlockToBytes(records []*parser.Record, zstdEnc *zstd.Encoder) ([]byte, error) {
	var buf bytes.Buffer
	if err := compressBlock(records, &buf, zstdEnc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func compressBlock(records []*parser.Record, w io.Writer, zstdEnc *zstd.Encoder) error {
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

	for _, rec := range records {
		// Encode sequence
		packed, nPos := encoder.PackBases(rec.Sequence)
		allSeqPacked = append(allSeqPacked, packed...)

		// Store N positions: count (uint16) + positions (uint16 each)
		nPosBuf := make([]byte, 2+len(nPos)*2)
		binary.LittleEndian.PutUint16(nPosBuf[0:2], uint16(len(nPos))) //nolint:gosec // len(nPos) bounded by seq length
		for i, pos := range nPos {
			binary.LittleEndian.PutUint16(nPosBuf[2+i*2:4+i*2], pos)
		}
		allNPositions = append(allNPositions, nPosBuf...)

		// Store sequence length
		seqLenBuf := make([]byte, 4)
		binary.LittleEndian.PutUint32(seqLenBuf, uint32(len(rec.Sequence))) //nolint:gosec // seq length bounded
		allSeqLengths = append(allSeqLengths, seqLenBuf...)

		originalSeqSize += uint32(len(rec.Sequence)) //nolint:gosec // bounded

		// Encode quality (delta encoding)
		qualCopy := make([]byte, len(rec.Quality))
		copy(qualCopy, rec.Quality)
		encoder.DeltaEncode(qualCopy)
		allQuality = append(allQuality, qualCopy...)
		originalQualSize += uint32(len(rec.Quality)) //nolint:gosec // bounded

		// Store header with length prefix
		headerBuf := make([]byte, 2+len(rec.Header))
		binary.LittleEndian.PutUint16(headerBuf[0:2], uint16(len(rec.Header))) //nolint:gosec // header length bounded
		copy(headerBuf[2:], rec.Header)
		allHeaders = append(allHeaders, headerBuf...)
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
func Decompress(r io.Reader, w io.Writer) error {
	// Read file header
	fileHeader, err := format.ReadFileHeader(r)
	if err != nil {
		return fmt.Errorf("reading file header: %w", err)
	}
	_ = fileHeader // We could use this for validation

	// Create zstd decoder
	zstdDec, err := zstd.NewReader(nil)
	if err != nil {
		return fmt.Errorf("creating zstd decoder: %w", err)
	}
	defer zstdDec.Close()

	// Read and decompress blocks until EOF
	for {
		blockHeader, err := format.ReadBlockHeader(r)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("reading block header: %w", err)
		}

		if err := decompressBlock(blockHeader, r, w, zstdDec); err != nil {
			return fmt.Errorf("decompressing block: %w", err)
		}
	}

	return nil
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
}

func decompressBlock(header *format.BlockHeader, r io.Reader, w io.Writer, zstdDec *zstd.Decoder) error {
	data, err := readAndDecompressBlock(header, r, zstdDec)
	if err != nil {
		return err
	}

	br := &blockReader{data: data}
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

	_, err = fmt.Fprintf(w, "@%s\n%s\n+\n%s\n", recHeader, seq, qual)
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
	br.qualOffset += seqLen
	return qual, nil
}

func (br *blockReader) readHeader() (string, error) {
	if br.headerOffset+2 > len(br.data.headerData) {
		return "", errors.New("truncated header data")
	}
	headerLen := int(binary.LittleEndian.Uint16(br.data.headerData[br.headerOffset : br.headerOffset+2]))
	br.headerOffset += 2

	if br.headerOffset+headerLen > len(br.data.headerData) {
		return "", errors.New("truncated header data")
	}
	recHeader := string(br.data.headerData[br.headerOffset : br.headerOffset+headerLen])
	br.headerOffset += headerLen
	return recHeader, nil
}
