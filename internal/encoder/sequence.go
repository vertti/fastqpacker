// Package encoder provides encoding functions for FASTQ components.
package encoder

// MaxSequenceLength is the maximum supported sequence length.
// Sequences longer than this will be truncated for N position tracking.
const MaxSequenceLength = 1 << 16 // 65536

// PackBases converts DNA sequence to 2-bit encoding.
// Returns packed bytes and positions of N bases.
// Encoding: A=00, C=01, G=10, T=11
// N bases are stored as A (00) in packed data, with positions tracked separately.
// Note: N position tracking is limited to sequences up to MaxSequenceLength.
func PackBases(seq []byte) (packed []byte, nPos []uint16) {
	if len(seq) == 0 {
		return nil, nil
	}

	packed = make([]byte, (len(seq)+3)/4)

	for i, b := range seq {
		var val byte
		switch b {
		case 'A', 'a':
			val = 0
		case 'C', 'c':
			val = 1
		case 'G', 'g':
			val = 2
		case 'T', 't':
			val = 3
		default: // N or other
			if i < MaxSequenceLength {
				nPos = append(nPos, uint16(i)) //nolint:gosec // bounds checked above
			}
			val = 0 // store as A, will be restored from nPos
		}
		// Pack 4 bases per byte, first base in lowest bits
		packed[i/4] |= val << ((i % 4) * 2)
	}

	return packed, nPos
}

// UnpackBases converts 2-bit encoded sequence back to DNA.
// Requires the original sequence length and N positions.
func UnpackBases(packed []byte, nPos []uint16, seqLen int) []byte {
	if seqLen == 0 {
		return nil
	}

	seq := make([]byte, seqLen)
	bases := [4]byte{'A', 'C', 'G', 'T'}

	for i := range seqLen {
		val := (packed[i/4] >> ((i % 4) * 2)) & 0x03
		seq[i] = bases[val] //nolint:gosec // val is masked to 0-3, always valid index
	}

	// Restore N bases
	for _, pos := range nPos {
		seq[pos] = 'N'
	}

	return seq
}
