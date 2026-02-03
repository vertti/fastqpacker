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
	batch := make([]*Record, 0, n)
	for i := 0; i < n; i++ {
		rec, err := p.Next()
		if err != nil {
			if err == io.EOF && len(batch) > 0 {
				return batch, nil
			}
			return batch, err
		}
		batch = append(batch, rec)
	}
	return batch, nil
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
