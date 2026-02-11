// Package format defines the FQZ file format for compressed FASTQ data.
package format

import (
	"encoding/binary"
	"errors"
	"io"
)

// Magic bytes identifying FQZ format.
var Magic = [4]byte{'F', 'Q', 'Z', 0x00}

// Format flags.
const (
	FlagPairedEnd uint8 = 1 << 0 // File contains interleaved paired-end data
	FlagPhred64   uint8 = 1 << 1 // Quality scores are Phred+64 encoded
)

// Supported file format versions.
const (
	Version1 uint8 = 1
	Version2 uint8 = 2

	CurrentVersion = Version2
)

// FileHeader is written at the start of every FQZ file.
type FileHeader struct {
	Version   uint8  // Format version
	BlockSize uint32 // Number of records per block
	Flags     uint8  // Format flags (e.g., paired-end)
}

// Write serializes the file header to the writer.
func (h *FileHeader) Write(w io.Writer) error {
	if _, err := w.Write(Magic[:]); err != nil {
		return err
	}
	buf := make([]byte, 6)
	buf[0] = h.Version
	binary.LittleEndian.PutUint32(buf[1:5], h.BlockSize)
	buf[5] = h.Flags
	_, err := w.Write(buf)
	return err
}

// ReadFileHeader reads and validates a file header.
func ReadFileHeader(r io.Reader) (*FileHeader, error) {
	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return nil, err
	}
	if magic != Magic {
		return nil, errors.New("invalid magic bytes: not an FQZ file")
	}

	buf := make([]byte, 6)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}

	return &FileHeader{
		Version:   buf[0],
		BlockSize: binary.LittleEndian.Uint32(buf[1:5]),
		Flags:     buf[5],
	}, nil
}

// BlockHeader precedes each compressed block.
type BlockHeader struct {
	NumRecords       uint32 // Number of FASTQ records in this block
	SeqDataSize      uint32 // Compressed sequence data size
	QualDataSize     uint32 // Compressed quality data size
	HeaderDataSize   uint32 // Compressed header data size
	PlusDataSize     uint32 // Compressed plus-line payload size (v2+)
	NPositionsSize   uint32 // Compressed N positions size
	SeqLengthsSize   uint32 // Compressed sequence lengths size
	OriginalSeqSize  uint32 // Original uncompressed sequence size
	OriginalQualSize uint32 // Original uncompressed quality size
}

// Write serializes the block header to the writer.
func (b *BlockHeader) Write(w io.Writer, version uint8) error {
	switch version {
	case Version1:
		buf := make([]byte, 32)
		binary.LittleEndian.PutUint32(buf[0:4], b.NumRecords)
		binary.LittleEndian.PutUint32(buf[4:8], b.SeqDataSize)
		binary.LittleEndian.PutUint32(buf[8:12], b.QualDataSize)
		binary.LittleEndian.PutUint32(buf[12:16], b.HeaderDataSize)
		binary.LittleEndian.PutUint32(buf[16:20], b.NPositionsSize)
		binary.LittleEndian.PutUint32(buf[20:24], b.SeqLengthsSize)
		binary.LittleEndian.PutUint32(buf[24:28], b.OriginalSeqSize)
		binary.LittleEndian.PutUint32(buf[28:32], b.OriginalQualSize)
		_, err := w.Write(buf)
		return err
	case Version2:
		buf := make([]byte, 36)
		binary.LittleEndian.PutUint32(buf[0:4], b.NumRecords)
		binary.LittleEndian.PutUint32(buf[4:8], b.SeqDataSize)
		binary.LittleEndian.PutUint32(buf[8:12], b.QualDataSize)
		binary.LittleEndian.PutUint32(buf[12:16], b.HeaderDataSize)
		binary.LittleEndian.PutUint32(buf[16:20], b.PlusDataSize)
		binary.LittleEndian.PutUint32(buf[20:24], b.NPositionsSize)
		binary.LittleEndian.PutUint32(buf[24:28], b.SeqLengthsSize)
		binary.LittleEndian.PutUint32(buf[28:32], b.OriginalSeqSize)
		binary.LittleEndian.PutUint32(buf[32:36], b.OriginalQualSize)
		_, err := w.Write(buf)
		return err
	default:
		return errors.New("unsupported block header version")
	}
}

// ReadBlockHeader reads a block header from the reader.
func ReadBlockHeader(r io.Reader, version uint8) (*BlockHeader, error) {
	switch version {
	case Version1:
		buf := make([]byte, 32)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return &BlockHeader{
			NumRecords:       binary.LittleEndian.Uint32(buf[0:4]),
			SeqDataSize:      binary.LittleEndian.Uint32(buf[4:8]),
			QualDataSize:     binary.LittleEndian.Uint32(buf[8:12]),
			HeaderDataSize:   binary.LittleEndian.Uint32(buf[12:16]),
			NPositionsSize:   binary.LittleEndian.Uint32(buf[16:20]),
			SeqLengthsSize:   binary.LittleEndian.Uint32(buf[20:24]),
			OriginalSeqSize:  binary.LittleEndian.Uint32(buf[24:28]),
			OriginalQualSize: binary.LittleEndian.Uint32(buf[28:32]),
		}, nil
	case Version2:
		buf := make([]byte, 36)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return &BlockHeader{
			NumRecords:       binary.LittleEndian.Uint32(buf[0:4]),
			SeqDataSize:      binary.LittleEndian.Uint32(buf[4:8]),
			QualDataSize:     binary.LittleEndian.Uint32(buf[8:12]),
			HeaderDataSize:   binary.LittleEndian.Uint32(buf[12:16]),
			PlusDataSize:     binary.LittleEndian.Uint32(buf[16:20]),
			NPositionsSize:   binary.LittleEndian.Uint32(buf[20:24]),
			SeqLengthsSize:   binary.LittleEndian.Uint32(buf[24:28]),
			OriginalSeqSize:  binary.LittleEndian.Uint32(buf[28:32]),
			OriginalQualSize: binary.LittleEndian.Uint32(buf[32:36]),
		}, nil
	default:
		return nil, errors.New("unsupported block header version")
	}
}
