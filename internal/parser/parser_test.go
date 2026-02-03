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

	assert.Equal(t, "SEQ_ID description", rec.Header)
	assert.Equal(t, []byte("ACGTACGT"), rec.Sequence)
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
		assert.Equal(t, tt.header, rec.Header)
		assert.Equal(t, []byte(tt.seq), rec.Sequence)
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
	assert.Equal(t, "HWI-ST123:4:1101:14346:1976#0/1", rec.Header)
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
