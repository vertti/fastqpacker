// Package parser provides fast FASTQ file parsing.
package parser

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// Record represents a single FASTQ record.
type Record struct {
	Header   string // Header line without the leading '@'
	Sequence []byte // DNA sequence (A, C, G, T, N)
	Quality  []byte // Quality scores (Phred+33 encoded)
}

// Parser reads FASTQ records from an input stream.
type Parser struct {
	reader *bufio.Reader
	line   []byte // reusable buffer for reading lines
}

// New creates a new FASTQ parser.
func New(r io.Reader) *Parser {
	return &Parser{
		reader: bufio.NewReaderSize(r, 1<<20), // 1MB buffer
		line:   make([]byte, 0, 512),
	}
}

// Next reads and returns the next FASTQ record.
// Returns io.EOF when no more records are available.
func (p *Parser) Next() (*Record, error) {
	rec := &Record{}

	// Line 1: Header (starts with @)
	line, err := p.readLine()
	if err != nil {
		return nil, err
	}
	if len(line) == 0 || line[0] != '@' {
		return nil, errors.New("invalid FASTQ: header line must start with @")
	}
	rec.Header = string(line[1:]) // strip leading @

	// Line 2: Sequence
	line, err = p.readLine()
	if err != nil {
		return nil, err
	}
	rec.Sequence = make([]byte, len(line))
	copy(rec.Sequence, line)

	// Line 3: Plus line (we ignore it)
	line, err = p.readLine()
	if err != nil {
		return nil, err
	}
	if len(line) == 0 || line[0] != '+' {
		return nil, errors.New("invalid FASTQ: separator line must start with +")
	}

	// Line 4: Quality scores
	line, err = p.readLine()
	if err != nil {
		return nil, err
	}
	rec.Quality = make([]byte, len(line))
	copy(rec.Quality, line)

	// Validate lengths match
	if len(rec.Sequence) != len(rec.Quality) {
		return nil, errors.New("invalid FASTQ: sequence and quality lengths must match")
	}

	return rec, nil
}

// NextBatch reads up to n records into a batch.
// Returns the records read and any error encountered.
// If fewer than n records are available, returns what's available.
func (p *Parser) NextBatch(n int) ([]*Record, error) {
	// Pre-allocate a contiguous slab of Records (1 allocation instead of n)
	slab := make([]Record, n)
	batch := make([]*Record, 0, n)

	// Pre-allocate a backing buffer for sequence+quality data.
	// Typical Illumina reads are ~150bp, so estimate 300 bytes per record (seq+qual).
	dataBuf := make([]byte, 0, n*300)

	for i := 0; i < n; i++ {
		var err error
		dataBuf, err = p.nextInto(&slab[i], dataBuf)
		if err != nil {
			if errors.Is(err, io.EOF) && len(batch) > 0 {
				return batch, nil
			}
			return batch, err
		}
		batch = append(batch, &slab[i])
	}
	return batch, nil
}

// nextInto parses a FASTQ record into rec, appending sequence and quality
// data to dataBuf and slicing from it. Returns the updated dataBuf.
func (p *Parser) nextInto(rec *Record, dataBuf []byte) ([]byte, error) {
	// Line 1: Header (starts with @)
	line, err := p.readLine()
	if err != nil {
		return dataBuf, err
	}
	if len(line) == 0 || line[0] != '@' {
		return dataBuf, errors.New("invalid FASTQ: header line must start with @")
	}
	rec.Header = string(line[1:])

	// Line 2: Sequence — carve from backing buffer
	line, err = p.readLine()
	if err != nil {
		return dataBuf, err
	}
	seqStart := len(dataBuf)
	dataBuf = append(dataBuf, line...)
	rec.Sequence = dataBuf[seqStart:len(dataBuf):len(dataBuf)]

	// Line 3: Plus line (we ignore it)
	line, err = p.readLine()
	if err != nil {
		return dataBuf, err
	}
	if len(line) == 0 || line[0] != '+' {
		return dataBuf, errors.New("invalid FASTQ: separator line must start with +")
	}

	// Line 4: Quality scores — carve from backing buffer
	line, err = p.readLine()
	if err != nil {
		return dataBuf, err
	}
	qualStart := len(dataBuf)
	dataBuf = append(dataBuf, line...)
	rec.Quality = dataBuf[qualStart:len(dataBuf):len(dataBuf)]

	if len(rec.Sequence) != len(rec.Quality) {
		return dataBuf, errors.New("invalid FASTQ: sequence and quality lengths must match")
	}

	return dataBuf, nil
}

// readLine reads a line from the input, stripping the newline.
// Reuses an internal buffer to minimize allocations.
func (p *Parser) readLine() ([]byte, error) {
	p.line = p.line[:0]

	for {
		segment, isPrefix, err := p.reader.ReadLine()
		if err != nil {
			return nil, err
		}

		p.line = append(p.line, segment...)

		if !isPrefix {
			break
		}
	}

	// Trim any trailing CR (for Windows line endings)
	p.line = bytes.TrimSuffix(p.line, []byte{'\r'})

	return p.line, nil
}
