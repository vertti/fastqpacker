package encoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeltaEncode_Simple(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "constant quality",
			input:    []byte{40, 40, 40, 40},
			expected: []byte{40, 0, 0, 0},
		},
		{
			name:     "increasing",
			input:    []byte{30, 31, 32, 33},
			expected: []byte{30, 1, 1, 1},
		},
		{
			name:     "decreasing",
			input:    []byte{40, 39, 38, 37},
			expected: []byte{40, 255, 255, 255}, // -1 as unsigned byte
		},
		{
			name:     "single value",
			input:    []byte{35},
			expected: []byte{35},
		},
		{
			name:     "empty",
			input:    []byte{},
			expected: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := make([]byte, len(tt.input))
			copy(input, tt.input)

			DeltaEncode(input)
			assert.Equal(t, tt.expected, input)
		})
	}
}

func TestDeltaDecode_Simple(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "constant quality (all zeros after first)",
			input:    []byte{40, 0, 0, 0},
			expected: []byte{40, 40, 40, 40},
		},
		{
			name:     "increasing",
			input:    []byte{30, 1, 1, 1},
			expected: []byte{30, 31, 32, 33},
		},
		{
			name:     "decreasing",
			input:    []byte{40, 255, 255, 255}, // -1 as unsigned byte
			expected: []byte{40, 39, 38, 37},
		},
		{
			name:     "single value",
			input:    []byte{35},
			expected: []byte{35},
		},
		{
			name:     "empty",
			input:    []byte{},
			expected: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := make([]byte, len(tt.input))
			copy(input, tt.input)

			DeltaDecode(input)
			assert.Equal(t, tt.expected, input)
		})
	}
}

func TestDeltaEncodeDecode_RoundTrip(t *testing.T) {
	t.Parallel()

	// Realistic quality score patterns
	qualityPatterns := [][]byte{
		// Constant high quality
		{40, 40, 40, 40, 40, 40, 40, 40},
		// Declining at ends (common pattern)
		{20, 30, 38, 40, 40, 40, 38, 30, 20},
		// Noisy quality
		{35, 38, 32, 40, 37, 39, 33, 36},
		// Low quality
		{10, 12, 8, 15, 11, 9, 13, 10},
		// Illumina-like pattern: starts lower, rises, stays high
		{28, 32, 36, 38, 40, 40, 40, 40, 40, 40, 38, 35, 30},
	}

	for i, original := range qualityPatterns {
		t.Run("pattern_"+string(rune('A'+i)), func(t *testing.T) {
			t.Parallel()

			// Make a copy
			data := make([]byte, len(original))
			copy(data, original)

			// Encode
			DeltaEncode(data)

			// Decode
			DeltaDecode(data)

			// Verify round-trip
			assert.Equal(t, original, data)
		})
	}
}

func TestDeltaEncode_LargeSequence(t *testing.T) {
	t.Parallel()

	// 152bp with realistic quality pattern
	original := make([]byte, 152)
	for i := range original {
		// Simulate quality that starts lower, rises, then falls at end
		switch {
		case i < 10:
			original[i] = byte(25 + i)
		case i > 140:
			original[i] = byte(40 - (i - 140))
		default:
			original[i] = 40
		}
	}

	data := make([]byte, len(original))
	copy(data, original)

	DeltaEncode(data)
	DeltaDecode(data)

	assert.Equal(t, original, data)
}

func BenchmarkDeltaEncode(b *testing.B) {
	// 152bp quality scores
	qual := make([]byte, 152)
	for i := range qual {
		qual[i] = byte(30 + (i % 10))
	}

	b.ResetTimer()
	b.SetBytes(int64(len(qual)))

	for i := 0; i < b.N; i++ {
		data := make([]byte, len(qual))
		copy(data, qual)
		DeltaEncode(data)
	}
}

func BenchmarkDeltaDecode(b *testing.B) {
	// 152bp delta-encoded quality scores
	qual := make([]byte, 152)
	qual[0] = 35
	for i := 1; i < len(qual); i++ {
		qual[i] = byte((i % 5) - 2) //nolint:gosec // intentional wrap for test delta values
	}

	b.ResetTimer()
	b.SetBytes(int64(len(qual)))

	for i := 0; i < b.N; i++ {
		data := make([]byte, len(qual))
		copy(data, qual)
		DeltaDecode(data)
	}
}

func TestDetectEncoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		quals    [][]byte
		expected QualityEncoding
	}{
		{
			name:     "Phred+33 low quality",
			quals:    [][]byte{{'!', '"', '#', '$'}}, // ASCII 33-36
			expected: EncodingPhred33,
		},
		{
			name:     "Phred+33 typical Illumina 1.8+ with some low qual",
			quals:    [][]byte{{'5', 'I', 'I', 'I'}}, // ASCII 53-73, includes Q20
			expected: EncodingPhred33,
		},
		{
			name:     "Phred+33 mixed",
			quals:    [][]byte{{'!', '5', 'I', '?'}}, // ASCII 33-73 range
			expected: EncodingPhred33,
		},
		{
			name:     "Phred+64 Illumina 1.3-1.7",
			quals:    [][]byte{{'@', 'A', 'B', 'h'}}, // ASCII 64-104 (Q0-40)
			expected: EncodingPhred64,
		},
		{
			name:     "Phred+64 high quality only",
			quals:    [][]byte{{'e', 'f', 'g', 'h'}}, // ASCII 101-104 (Q37-40)
			expected: EncodingPhred64,
		},
		{
			name:     "Phred+64 multiple records",
			quals:    [][]byte{{'h', 'h', 'h'}, {'@', 'A', 'B'}},
			expected: EncodingPhred64,
		},
		{
			name:     "empty input defaults to Phred+33",
			quals:    [][]byte{},
			expected: EncodingPhred33,
		},
		{
			name:     "empty quality string defaults to Phred+33",
			quals:    [][]byte{{}},
			expected: EncodingPhred33,
		},
		{
			name:     "ambiguous range (59-63) defaults to Phred+33",
			quals:    [][]byte{{';', '<', '=', '>'}}, // ASCII 59-62
			expected: EncodingPhred33,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := DetectEncoding(tt.quals)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeQuality(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []byte
		encoding QualityEncoding
		expected []byte
	}{
		{
			name:     "Phred+33 normalization",
			input:    []byte{'!', '#', 'I'}, // Q0, Q2, Q40
			encoding: EncodingPhred33,
			expected: []byte{0, 2, 40},
		},
		{
			name:     "Phred+64 normalization",
			input:    []byte{'@', 'B', 'h'}, // Q0, Q2, Q40
			encoding: EncodingPhred64,
			expected: []byte{0, 2, 40},
		},
		{
			name:     "empty slice",
			input:    []byte{},
			encoding: EncodingPhred33,
			expected: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := make([]byte, len(tt.input))
			copy(input, tt.input)
			NormalizeQuality(input, tt.encoding)
			assert.Equal(t, tt.expected, input)
		})
	}
}

func TestDenormalizeQuality(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []byte
		encoding QualityEncoding
		expected []byte
	}{
		{
			name:     "Phred+33 denormalization",
			input:    []byte{0, 2, 40},
			encoding: EncodingPhred33,
			expected: []byte{'!', '#', 'I'}, // Q0, Q2, Q40
		},
		{
			name:     "Phred+64 denormalization",
			input:    []byte{0, 2, 40},
			encoding: EncodingPhred64,
			expected: []byte{'@', 'B', 'h'}, // Q0, Q2, Q40
		},
		{
			name:     "empty slice",
			input:    []byte{},
			encoding: EncodingPhred33,
			expected: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := make([]byte, len(tt.input))
			copy(input, tt.input)
			DenormalizeQuality(input, tt.encoding)
			assert.Equal(t, tt.expected, input)
		})
	}
}

func TestNormalizeDenormalize_RoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []byte
		encoding QualityEncoding
	}{
		{
			name:     "Phred+33 round trip",
			input:    []byte{'!', '5', 'I', '?', '#'},
			encoding: EncodingPhred33,
		},
		{
			name:     "Phred+64 round trip",
			input:    []byte{'@', 'P', 'h', 'Z', 'B'},
			encoding: EncodingPhred64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			original := make([]byte, len(tt.input))
			copy(original, tt.input)

			data := make([]byte, len(tt.input))
			copy(data, tt.input)

			NormalizeQuality(data, tt.encoding)
			DenormalizeQuality(data, tt.encoding)

			assert.Equal(t, original, data)
		})
	}
}
