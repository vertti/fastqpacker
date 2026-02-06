package compress

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vertti/fastqpacker/internal/encoder"
	"github.com/vertti/fastqpacker/internal/parser"
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
	err = Decompress(&compressed, &decompressed, nil)
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
	err = Decompress(&compressed, &decompressed, nil)
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
	err = Decompress(&compressed, &decompressed, nil)
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
	err = Decompress(&compressed, &decompressed, nil)
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
	err = Decompress(&compressed, &decompressed, nil)
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
		err = Decompress(&compressed, &decompressed, nil)
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
	err = Decompress(&compressed, &decompressed, nil)
	require.NoError(t, err)

	assert.Equal(t, input, decompressed.String())
}

func TestCompressDecompress_ParallelMultipleBlocks(t *testing.T) {
	t.Parallel()

	// Generate enough records for multiple blocks
	var input bytes.Buffer
	seq := strings.Repeat("ACGT", 38) // 152bp
	qual := strings.Repeat("I", 152)
	for i := 0; i < 500; i++ {
		input.WriteString("@SEQ_" + string(rune('A'+i%26)) + "_" + string(rune('0'+i%10)) + "\n")
		input.WriteString(seq + "\n")
		input.WriteString("+\n")
		input.WriteString(qual + "\n")
	}

	originalData := input.String()

	// Compress with small block size to force multiple blocks
	opts := &Options{
		BlockSize: 100,
		Workers:   4,
	}

	var compressed bytes.Buffer
	err := Compress(strings.NewReader(originalData), &compressed, opts)
	require.NoError(t, err)

	// Decompress and verify
	var decompressed bytes.Buffer
	err = Decompress(&compressed, &decompressed, nil)
	require.NoError(t, err)

	assert.Equal(t, originalData, decompressed.String())
}

func TestCompressDecompress_SingleWorker(t *testing.T) {
	t.Parallel()

	input := `@SEQ_1
ACGTACGTACGTACGT
+
IIIIIIIIIIIIIIII
@SEQ_2
GGGGCCCCAAAATTTT
+
HHHHHHHHHHHHHHHH
`
	opts := &Options{
		Workers: 1,
	}

	var compressed bytes.Buffer
	err := Compress(strings.NewReader(input), &compressed, opts)
	require.NoError(t, err)

	var decompressed bytes.Buffer
	err = Decompress(&compressed, &decompressed, nil)
	require.NoError(t, err)

	assert.Equal(t, input, decompressed.String())
}

func TestCompressDecompress_ManyWorkersSmallInput(t *testing.T) {
	t.Parallel()

	// More workers than blocks - should handle gracefully
	input := `@SEQ_1
ACGTACGTACGTACGT
+
IIIIIIIIIIIIIIII
`
	opts := &Options{
		Workers: 16,
	}

	var compressed bytes.Buffer
	err := Compress(strings.NewReader(input), &compressed, opts)
	require.NoError(t, err)

	var decompressed bytes.Buffer
	err = Decompress(&compressed, &decompressed, nil)
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
		_ = Decompress(bytes.NewReader(compressedData), &decompressed, nil)
	}
}

func TestCompressDecompress_Phred64(t *testing.T) {
	t.Parallel()

	// Create Phred+64 encoded quality string
	// Phred+64: Q0='@' (64), Q10='J' (74), Q20='T' (84), Q30='^' (94), Q40='h' (104)
	phred64Qual := "@JT^h@JT^h@JT^h@J" // 17 bases with various qualities

	input := `@SEQ_PHRED64
ACGTACGTACGTACGTA
+
` + phred64Qual + `
`
	var compressed bytes.Buffer
	err := Compress(strings.NewReader(input), &compressed, nil)
	require.NoError(t, err)

	var decompressed bytes.Buffer
	err = Decompress(&compressed, &decompressed, nil)
	require.NoError(t, err)

	assert.Equal(t, input, decompressed.String())
}

func TestCompressDecompress_Phred64_MultipleRecords(t *testing.T) {
	t.Parallel()

	// Multiple records with Phred+64 encoding
	// All qualities >= '@' (64) will be detected as Phred+64
	input := `@SEQ_1
AAAAAAAAAAAAAAAA
+
@@@@@@@@@@@@@@@@
@SEQ_2
CCCCCCCCCCCCCCCC
+
hhhhhhhhhhhhhhhh
@SEQ_3
GGGGGGGGGGGGGGGG
+
TTTTTTTTTTTTTTTT
`
	var compressed bytes.Buffer
	err := Compress(strings.NewReader(input), &compressed, nil)
	require.NoError(t, err)

	var decompressed bytes.Buffer
	err = Decompress(&compressed, &decompressed, nil)
	require.NoError(t, err)

	assert.Equal(t, input, decompressed.String())
}

func TestCompressDecompress_Phred64_LargeBatch(t *testing.T) {
	t.Parallel()

	// Generate many Phred+64 records to test across blocks
	var input bytes.Buffer
	seq := strings.Repeat("ACGT", 38) // 152bp
	// Phred+64 quality: Q35-Q40 range (ASCII 99-104)
	qual := strings.Repeat("efgh", 38) // 152bp of high-quality Phred+64 scores

	for i := 0; i < 500; i++ {
		fmt.Fprintf(&input, "@SEQ_%d\n%s\n+\n%s\n", i, seq, qual)
	}

	originalData := input.String()

	// Use small block size to force multiple blocks
	opts := &Options{
		BlockSize: 100,
		Workers:   4,
	}

	var compressed bytes.Buffer
	err := Compress(strings.NewReader(originalData), &compressed, opts)
	require.NoError(t, err)

	var decompressed bytes.Buffer
	err = Decompress(&compressed, &decompressed, nil)
	require.NoError(t, err)

	assert.Equal(t, originalData, decompressed.String())
}

func TestDecompressParallel(t *testing.T) {
	t.Parallel()

	// Generate enough records for multiple blocks
	var input bytes.Buffer
	seq := strings.Repeat("ACGT", 38) // 152bp
	qual := strings.Repeat("I", 152)
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&input, "@SEQ_%d\n%s\n+\n%s\n", i, seq, qual)
	}

	originalData := input.String()

	// Compress with small block size to force multiple blocks
	compressOpts := &Options{
		BlockSize: 100,
		Workers:   4,
	}

	var compressed bytes.Buffer
	err := Compress(strings.NewReader(originalData), &compressed, compressOpts)
	require.NoError(t, err)

	// Decompress with multiple workers
	decompressOpts := &DecompressOptions{
		Workers: 4,
	}

	var decompressed bytes.Buffer
	err = Decompress(&compressed, &decompressed, decompressOpts)
	require.NoError(t, err)

	assert.Equal(t, originalData, decompressed.String())
}

func TestDecompressSingleWorker(t *testing.T) {
	t.Parallel()

	input := `@SEQ_1
ACGTACGTACGTACGT
+
IIIIIIIIIIIIIIII
@SEQ_2
GGGGCCCCAAAATTTT
+
HHHHHHHHHHHHHHHH
`
	var compressed bytes.Buffer
	err := Compress(strings.NewReader(input), &compressed, nil)
	require.NoError(t, err)

	// Decompress with single worker
	decompressOpts := &DecompressOptions{
		Workers: 1,
	}

	var decompressed bytes.Buffer
	err = Decompress(&compressed, &decompressed, decompressOpts)
	require.NoError(t, err)

	assert.Equal(t, input, decompressed.String())
}

func TestCompressDecompress_MixedPhredInSameFile(t *testing.T) {
	t.Parallel()

	// This tests that detection works correctly when first record has low quality
	// that clearly identifies it as Phred+33, even if later records could be ambiguous
	input := `@SEQ_WITH_LOW_QUAL
ACGTACGTACGTACGT
+
!!!!!!!!!!!!!!!!
@SEQ_WITH_HIGH_QUAL
GGGGGGGGGGGGGGGG
+
IIIIIIIIIIIIIIII
`
	var compressed bytes.Buffer
	err := Compress(strings.NewReader(input), &compressed, nil)
	require.NoError(t, err)

	var decompressed bytes.Buffer
	err = Decompress(&compressed, &decompressed, nil)
	require.NoError(t, err)

	assert.Equal(t, input, decompressed.String())
}

func BenchmarkCompressBlock(b *testing.B) {
	// Generate test records directly (bypasses parser + file header overhead)
	seq := []byte(strings.Repeat("ACGT", 38)) // 152bp
	qual := []byte(strings.Repeat("I", 152))
	header := []byte("HWI-ST123:4:1101:14346:1976#0/1")

	const numRecords = 100000
	records := make([]parser.Record, numRecords)
	for i := range records {
		records[i] = parser.Record{
			Header:   header,
			Sequence: seq,
			Quality:  qual,
		}
	}

	totalBytes := int64(numRecords) * int64(len(header)+len(seq)+len(qual)+4) // +4 for @/+/newlines

	zstdEnc, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	defer zstdEnc.Close() //nolint:errcheck

	b.ResetTimer()
	b.SetBytes(totalBytes)

	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		_ = compressBlock(records, &buf, zstdEnc, encoder.EncodingPhred33)
	}
}

func BenchmarkCompressParallel(b *testing.B) {
	// Generate test data - larger set to benefit from parallelism
	var input bytes.Buffer
	seq := strings.Repeat("ACGT", 38) // 152bp
	qual := strings.Repeat("I", 152)
	for i := 0; i < 100000; i++ {
		input.WriteString("@HWI-ST123:4:1101:14346:1976#0/1\n")
		input.WriteString(seq + "\n")
		input.WriteString("+\n")
		input.WriteString(qual + "\n")
	}
	data := input.Bytes()

	for _, workers := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			opts := &Options{Workers: workers}
			b.ResetTimer()
			b.SetBytes(int64(len(data)))

			for i := 0; i < b.N; i++ {
				var compressed bytes.Buffer
				_ = Compress(bytes.NewReader(data), &compressed, opts)
			}
		})
	}
}
