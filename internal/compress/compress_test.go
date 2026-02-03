package compress

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompressDecompress_SingleRecord(t *testing.T) {
	t.Parallel()

	input := `@SEQ_1
ACGTACGTACGTACGT
+
IIIIIIIIIIIIIIII
`
	var compressed bytes.Buffer
	err := Compress(strings.NewReader(input), &compressed, nil)
	require.NoError(t, err)

	var decompressed bytes.Buffer
	err = Decompress(&compressed, &decompressed)
	require.NoError(t, err)

	assert.Equal(t, input, decompressed.String())
}

func TestCompressDecompress_MultipleRecords(t *testing.T) {
	t.Parallel()

	input := `@SEQ_1
AAAAAAAAAAAAAAAA
+
!!!!!!!!!!!!!!!!
@SEQ_2
CCCCCCCCCCCCCCCC
+
################
@SEQ_3
GGGGGGGGGGGGGGGG
+
$$$$$$$$$$$$$$$$
`
	var compressed bytes.Buffer
	err := Compress(strings.NewReader(input), &compressed, nil)
	require.NoError(t, err)

	var decompressed bytes.Buffer
	err = Decompress(&compressed, &decompressed)
	require.NoError(t, err)

	assert.Equal(t, input, decompressed.String())
}

func TestCompressDecompress_WithNBases(t *testing.T) {
	t.Parallel()

	input := `@SEQ_WITH_N
ACNTNACGTNNNNACGT
+
IIIIIIIIIIIIIIIII
`
	var compressed bytes.Buffer
	err := Compress(strings.NewReader(input), &compressed, nil)
	require.NoError(t, err)

	var decompressed bytes.Buffer
	err = Decompress(&compressed, &decompressed)
	require.NoError(t, err)

	assert.Equal(t, input, decompressed.String())
}

func TestCompressDecompress_IlluminaFormat(t *testing.T) {
	t.Parallel()

	// 152bp sequence with matching quality string
	seq := strings.Repeat("ACGT", 38)
	qual := strings.Repeat("I", 152)
	input := "@HWI-ST123:4:1101:14346:1976#0/1\n" + seq + "\n+\n" + qual + "\n"
	var compressed bytes.Buffer
	err := Compress(strings.NewReader(input), &compressed, nil)
	require.NoError(t, err)

	var decompressed bytes.Buffer
	err = Decompress(&compressed, &decompressed)
	require.NoError(t, err)

	assert.Equal(t, input, decompressed.String())
}

func TestCompressDecompress_LargeBatch(t *testing.T) {
	t.Parallel()

	// Generate 1000 records
	var input bytes.Buffer
	seq := strings.Repeat("ACGT", 38) // 152bp
	qual := strings.Repeat("I", 152)
	for i := 0; i < 1000; i++ {
		input.WriteString("@SEQ_" + string(rune('A'+i%26)) + "\n")
		input.WriteString(seq + "\n")
		input.WriteString("+\n")
		input.WriteString(qual + "\n")
	}

	// Save original for comparison (buffer is consumed by Compress)
	originalData := input.String()
	originalSize := len(originalData)

	var compressed bytes.Buffer
	err := Compress(strings.NewReader(originalData), &compressed, nil)
	require.NoError(t, err)

	// Check compression ratio
	compressedSize := compressed.Len()
	t.Logf("Original: %d bytes, Compressed: %d bytes, Ratio: %.2fx",
		originalSize, compressedSize, float64(originalSize)/float64(compressedSize))

	// Decompress and verify
	var decompressed bytes.Buffer
	err = Decompress(&compressed, &decompressed)
	require.NoError(t, err)

	assert.Equal(t, originalData, decompressed.String())
}

func TestCompressDecompress_EmptyInput(t *testing.T) {
	t.Parallel()

	var compressed bytes.Buffer
	err := Compress(strings.NewReader(""), &compressed, nil)
	// Empty input should produce an error or empty output
	// Depending on implementation choice
	if err == nil {
		var decompressed bytes.Buffer
		err = Decompress(&compressed, &decompressed)
		require.NoError(t, err)
		assert.Empty(t, decompressed.String())
	}
}

func TestCompressWithOptions(t *testing.T) {
	t.Parallel()

	input := `@SEQ_1
ACGTACGTACGTACGT
+
IIIIIIIIIIIIIIII
`
	opts := &Options{
		BlockSize: 100,
	}

	var compressed bytes.Buffer
	err := Compress(strings.NewReader(input), &compressed, opts)
	require.NoError(t, err)

	var decompressed bytes.Buffer
	err = Decompress(&compressed, &decompressed)
	require.NoError(t, err)

	assert.Equal(t, input, decompressed.String())
}

func BenchmarkCompress(b *testing.B) {
	// Generate test data
	var input bytes.Buffer
	seq := strings.Repeat("ACGT", 38) // 152bp
	qual := strings.Repeat("I", 152)
	for i := 0; i < 10000; i++ {
		input.WriteString("@HWI-ST123:4:1101:14346:1976#0/1\n")
		input.WriteString(seq + "\n")
		input.WriteString("+\n")
		input.WriteString(qual + "\n")
	}
	data := input.Bytes()

	b.ResetTimer()
	b.SetBytes(int64(len(data)))

	for i := 0; i < b.N; i++ {
		var compressed bytes.Buffer
		_ = Compress(bytes.NewReader(data), &compressed, nil)
	}
}

func BenchmarkDecompress(b *testing.B) {
	// Generate and compress test data
	var input bytes.Buffer
	seq := strings.Repeat("ACGT", 38)
	qual := strings.Repeat("I", 152)
	for i := 0; i < 10000; i++ {
		input.WriteString("@HWI-ST123:4:1101:14346:1976#0/1\n")
		input.WriteString(seq + "\n")
		input.WriteString("+\n")
		input.WriteString(qual + "\n")
	}

	var compressed bytes.Buffer
	_ = Compress(&input, &compressed, nil)
	compressedData := compressed.Bytes()

	b.ResetTimer()
	b.SetBytes(int64(input.Len()))

	for i := 0; i < b.N; i++ {
		var decompressed bytes.Buffer
		_ = Decompress(bytes.NewReader(compressedData), &decompressed)
	}
}
