package format

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileHeader_WriteRead(t *testing.T) {
	t.Parallel()

	header := FileHeader{
		Version:   1,
		BlockSize: 100000,
		Flags:     FlagPairedEnd,
	}

	var buf bytes.Buffer
	err := header.Write(&buf)
	require.NoError(t, err)

	// Check magic bytes
	assert.Equal(t, []byte{'F', 'Q', 'Z', 0x00}, buf.Bytes()[:4])

	// Read it back
	readHeader, err := ReadFileHeader(&buf)
	require.NoError(t, err)

	assert.Equal(t, header.Version, readHeader.Version)
	assert.Equal(t, header.BlockSize, readHeader.BlockSize)
	assert.Equal(t, header.Flags, readHeader.Flags)
}

func TestFileHeader_InvalidMagic(t *testing.T) {
	t.Parallel()

	buf := bytes.NewReader([]byte{'X', 'Y', 'Z', 0x00, 1, 0, 0, 0, 0, 0})
	_, err := ReadFileHeader(buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid magic")
}

func TestBlockHeader_WriteRead(t *testing.T) {
	t.Parallel()

	block := BlockHeader{
		NumRecords:       1000,
		SeqDataSize:      5000,
		QualDataSize:     8000,
		HeaderDataSize:   500,
		NPositionsSize:   100,
		SeqLengthsSize:   2000,
		OriginalSeqSize:  20000,
		OriginalQualSize: 20000,
	}

	var buf bytes.Buffer
	err := block.Write(&buf)
	require.NoError(t, err)

	readBlock, err := ReadBlockHeader(&buf)
	require.NoError(t, err)

	assert.Equal(t, block.NumRecords, readBlock.NumRecords)
	assert.Equal(t, block.SeqDataSize, readBlock.SeqDataSize)
	assert.Equal(t, block.QualDataSize, readBlock.QualDataSize)
	assert.Equal(t, block.HeaderDataSize, readBlock.HeaderDataSize)
	assert.Equal(t, block.NPositionsSize, readBlock.NPositionsSize)
	assert.Equal(t, block.SeqLengthsSize, readBlock.SeqLengthsSize)
}

func TestFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		flags    uint8
		isPaired bool
	}{
		{"no flags", 0, false},
		{"paired end", FlagPairedEnd, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.isPaired, tt.flags&FlagPairedEnd != 0)
		})
	}
}
