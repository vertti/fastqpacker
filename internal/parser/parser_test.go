package parser

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRecord(t *testing.T) {
	input := `@SEQ_ID description
ACGTACGT
+
IIIIIIII
`
	p := New(strings.NewReader(input))
	rec, err := p.Next()
	require.NoError(t, err)

	assert.Equal(t, []byte("SEQ_ID description"), rec.Header)
	assert.Equal(t, []byte("ACGTACGT"), rec.Sequence)
	assert.Empty(t, rec.PlusLine)
	assert.Equal(t, []byte("IIIIIIII"), rec.Quality)
}

func TestParseMultipleRecords(t *testing.T) {
	input := `@SEQ_1
AAAA
+
!!!!
@SEQ_2
CCCC
+
####
@SEQ_3
GGGG
+
$$$$
`
	p := New(strings.NewReader(input))

	tests := []struct {
		header string
		seq    string
		qual   string
	}{
		{"SEQ_1", "AAAA", "!!!!"},
		{"SEQ_2", "CCCC", "####"},
		{"SEQ_3", "GGGG", "$$$$"},
	}

	for _, tt := range tests {
		rec, err := p.Next()
		require.NoError(t, err)
		assert.Equal(t, []byte(tt.header), rec.Header)
		assert.Equal(t, []byte(tt.seq), rec.Sequence)
		assert.Empty(t, rec.PlusLine)
		assert.Equal(t, []byte(tt.qual), rec.Quality)
	}

	// Should get EOF after all records
	_, err := p.Next()
	assert.ErrorIs(t, err, io.EOF)
}

func TestParseEmptyInput(t *testing.T) {
	p := New(strings.NewReader(""))
	_, err := p.Next()
	assert.Error(t, err)
}

func TestParseMalformedNoAt(t *testing.T) {
	input := `SEQ_ID
ACGT
+
IIII
`
	p := New(strings.NewReader(input))
	_, err := p.Next()
	assert.Error(t, err)
}

func TestParseMalformedMismatchedLength(t *testing.T) {
	input := `@SEQ_ID
ACGTACGT
+
III
`
	p := New(strings.NewReader(input))
	_, err := p.Next()
	assert.Error(t, err)
}

func TestParseWithNBases(t *testing.T) {
	input := `@SEQ_ID
ACNTNACGT
+
IIIIIIIII
`
	p := New(strings.NewReader(input))
	rec, err := p.Next()
	require.NoError(t, err)
	assert.Equal(t, []byte("ACNTNACGT"), rec.Sequence)
}

func TestParseIlluminaHeader(t *testing.T) {
	input := `@HWI-ST123:4:1101:14346:1976#0/1
ACGTACGTACGTACGTACGTACGTACGTACGTACGTACGT
+
IIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIII
`
	p := New(strings.NewReader(input))
	rec, err := p.Next()
	require.NoError(t, err)
	assert.Equal(t, []byte("HWI-ST123:4:1101:14346:1976#0/1"), rec.Header)
	assert.Empty(t, rec.PlusLine)
}

func TestParsePlusLinePayload(t *testing.T) {
	input := `@SEQ_1
ACGTACGT
+SEQ_1 comment
IIIIIIII
`
	p := New(strings.NewReader(input))
	rec, err := p.Next()
	require.NoError(t, err)
	assert.Equal(t, []byte("SEQ_1 comment"), rec.PlusLine)
}

func TestParseBatch(t *testing.T) {
	var buf bytes.Buffer
	for i := 0; i < 1000; i++ {
		buf.WriteString("@SEQ_" + string(rune('A'+i%26)) + "\n")
		buf.WriteString("ACGTACGTACGTACGT\n")
		buf.WriteString("+\n")
		buf.WriteString("IIIIIIIIIIIIIIII\n")
	}

	p := New(&buf)
	batch, err := p.NextBatch(100)
	require.NoError(t, err)
	assert.Len(t, batch, 100)
}

func TestReadBatch(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	for i := 0; i < 250; i++ {
		buf.WriteString("@SEQ_" + string(rune('A'+i%26)) + "\n")
		buf.WriteString("ACGTACGTACGTACGT\n")
		buf.WriteString("+\n")
		buf.WriteString("IIIIIIIIIIIIIIII\n")
	}

	p := New(&buf)
	batch := NewRecordBatch(100)

	// First batch: 100 records
	err := p.ReadBatch(batch)
	require.NoError(t, err)
	assert.Equal(t, 100, batch.Len())
	assert.Equal(t, []byte("ACGTACGTACGTACGT"), batch.Records[0].Sequence)

	// Second batch: 100 records
	err = p.ReadBatch(batch)
	require.NoError(t, err)
	assert.Equal(t, 100, batch.Len())

	// Third batch: 50 records (remaining)
	err = p.ReadBatch(batch)
	require.NoError(t, err)
	assert.Equal(t, 50, batch.Len())

	// Fourth batch: EOF
	err = p.ReadBatch(batch)
	assert.ErrorIs(t, err, io.EOF)
}

func BenchmarkReadBatch(b *testing.B) {
	var buf bytes.Buffer
	seq := strings.Repeat("ACGT", 38) // 152 bp
	qual := strings.Repeat("I", 152)
	for i := 0; i < 10000; i++ {
		buf.WriteString("@HWI-ST123:4:1101:14346:1976#0/1\n")
		buf.WriteString(seq + "\n")
		buf.WriteString("+\n")
		buf.WriteString(qual + "\n")
	}
	input := buf.Bytes()

	b.ResetTimer()
	b.SetBytes(int64(len(input)))

	for i := 0; i < b.N; i++ {
		p := New(bytes.NewReader(input))
		batch := NewRecordBatch(1000)
		for {
			err := p.ReadBatch(batch)
			if err != nil {
				break
			}
		}
	}
}

func BenchmarkParser(b *testing.B) {
	var buf bytes.Buffer
	seq := strings.Repeat("ACGT", 38) // 152 bp typical Illumina read
	qual := strings.Repeat("I", 152)
	for i := 0; i < 10000; i++ {
		buf.WriteString("@HWI-ST123:4:1101:14346:1976#0/1\n")
		buf.WriteString(seq + "\n")
		buf.WriteString("+\n")
		buf.WriteString(qual + "\n")
	}
	input := buf.Bytes()

	b.ResetTimer()
	b.SetBytes(int64(len(input)))

	for i := 0; i < b.N; i++ {
		p := New(bytes.NewReader(input))
		for {
			_, err := p.Next()
			if err != nil {
				break
			}
		}
	}
}
