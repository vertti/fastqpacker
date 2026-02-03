package encoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackBases_Simple(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantNPos []uint16
	}{
		{
			name:     "ACGT pattern",
			input:    "ACGT",
			wantNPos: nil,
		},
		{
			name:     "all A",
			input:    "AAAA",
			wantNPos: nil,
		},
		{
			name:     "all T",
			input:    "TTTT",
			wantNPos: nil,
		},
		{
			name:     "lowercase",
			input:    "acgt",
			wantNPos: nil,
		},
		{
			name:     "with N bases",
			input:    "ACNGT",
			wantNPos: []uint16{2},
		},
		{
			name:     "multiple N bases",
			input:    "NACTGN",
			wantNPos: []uint16{0, 5},
		},
		{
			name:     "all N",
			input:    "NNNN",
			wantNPos: []uint16{0, 1, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			packed, nPos := PackBases([]byte(tt.input))
			assert.Equal(t, tt.wantNPos, nPos)

			// Verify round-trip
			unpacked := UnpackBases(packed, nPos, len(tt.input))
			// Normalize to uppercase for comparison
			expected := make([]byte, len(tt.input))
			for i, b := range []byte(tt.input) {
				switch b {
				case 'a':
					expected[i] = 'A'
				case 'c':
					expected[i] = 'C'
				case 'g':
					expected[i] = 'G'
				case 't':
					expected[i] = 'T'
				case 'n':
					expected[i] = 'N'
				default:
					expected[i] = b
				}
			}
			assert.Equal(t, expected, unpacked)
		})
	}
}

func TestPackBases_RoundTrip(t *testing.T) {
	t.Parallel()

	sequences := []string{
		"ACGTACGTACGTACGT",
		"AAAAAAAAAAAAAAAA",
		"CCCCCCCCCCCCCCCC",
		"GGGGGGGGGGGGGGGG",
		"TTTTTTTTTTTTTTTT",
		"ACGTNNNNACGTNNNN",
		"N",
		"A",
		"ACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGT", // 100bp
	}

	for _, seq := range sequences {
		t.Run(seq[:min(10, len(seq))], func(t *testing.T) {
			t.Parallel()

			packed, nPos := PackBases([]byte(seq))
			unpacked := UnpackBases(packed, nPos, len(seq))
			assert.Equal(t, seq, string(unpacked))
		})
	}
}

func TestPackBases_PackedSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		seqLen       int
		expectedSize int
	}{
		{1, 1},
		{2, 1},
		{3, 1},
		{4, 1},
		{5, 2},
		{8, 2},
		{9, 3},
		{100, 25},
		{152, 38}, // typical Illumina read
	}

	for _, tt := range tests {
		seq := make([]byte, tt.seqLen)
		for i := range seq {
			seq[i] = "ACGT"[i%4]
		}
		packed, _ := PackBases(seq)
		assert.Len(t, packed, tt.expectedSize, "seqLen=%d", tt.seqLen)
	}
}

func TestUnpackBases_EmptyInput(t *testing.T) {
	t.Parallel()

	result := UnpackBases(nil, nil, 0)
	assert.Empty(t, result)
}

func BenchmarkPackBases(b *testing.B) {
	// 152bp typical Illumina read
	seq := []byte("ACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGT")

	b.ResetTimer()
	b.SetBytes(int64(len(seq)))

	for i := 0; i < b.N; i++ {
		PackBases(seq)
	}
}

func BenchmarkUnpackBases(b *testing.B) {
	seq := []byte("ACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGTACGT")
	packed, nPos := PackBases(seq)
	seqLen := len(seq)

	b.ResetTimer()
	b.SetBytes(int64(seqLen))

	for i := 0; i < b.N; i++ {
		UnpackBases(packed, nPos, seqLen)
	}
}

func TestPackBasesWithPool(t *testing.T) {
	t.Parallel()

	seq := []byte("ACGTACGT")
	packed, nPos := PackBases(seq)

	require.NotNil(t, packed)
	assert.Empty(t, nPos)

	unpacked := UnpackBases(packed, nPos, len(seq))
	assert.Equal(t, seq, unpacked)
}
