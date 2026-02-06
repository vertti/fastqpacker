// Package encoder provides encoding functions for FASTQ components.
package encoder

// MaxSequenceLength is the maximum supported sequence length.
// Sequences longer than this will be truncated for N position tracking.
const MaxSequenceLength = 1 << 16 // 65536

var baseTable [256]byte

func init() {
	// Initialize base table
	// Default to 4 (N/other)
	for i := range baseTable {
		baseTable[i] = 4
	}
	// Set values for ACGT
	baseTable['A'] = 0
	baseTable['a'] = 0
	baseTable['C'] = 1
	baseTable['c'] = 1
	baseTable['G'] = 2
	baseTable['g'] = 2
	baseTable['T'] = 3
	baseTable['t'] = 3
}

// PackBases converts DNA sequence to 2-bit encoding.
// Returns packed bytes and positions of N bases.
// Encoding: A=00, C=01, G=10, T=11
// N bases are stored as A (00) in packed data, with positions tracked separately.
// Note: N position tracking is limited to sequences up to MaxSequenceLength.
func PackBases(seq []byte) (packed []byte, nPos []uint16) {
	if len(seq) == 0 {
		return nil, nil
	}
	// Use AppendPackBases with nil buffers to create new ones
	return AppendPackedBases(nil, nil, seq)
}

// AppendPackedBases appends 2-bit encoded sequence to dst and N positions to nPosDst.
// It avoids allocating new slices if dst/nPosDst have enough capacity.
func AppendPackedBases(dst []byte, nPosDst []uint16, seq []byte) ([]byte, []uint16) {
	if len(seq) == 0 {
		return dst, nPosDst
	}

	// Calculate needed size
	packedLen := (len(seq) + 3) / 4

	// Ensure dst has enough capacity
	startIdx := len(dst)
	needed := startIdx + packedLen
	if cap(dst) < needed {
		capNeeded := needed
		if cap(dst) > 0 {
			capNeeded = needed + needed/2 // Grow by 1.5x if extending
		}
		newDst := make([]byte, startIdx, capNeeded)
		copy(newDst, dst)
		dst = newDst
	}
	// Extend dst length
	dst = dst[:needed]

	// Initialize new bytes to 0
	for i := startIdx; i < len(dst); i++ {
		dst[i] = 0
	}

	for i, b := range seq {
		val := baseTable[b]
		if val == 4 { // N or other
			if i < MaxSequenceLength {
				nPosDst = append(nPosDst, uint16(i)) //nolint:gosec // bounds checked above
			}
			val = 0 // store as A
		}

		// Pack 4 bases per byte
		// dst is already 0-initialized for the new range
		dst[startIdx+i/4] |= val << ((i % 4) * 2)
	}

	return dst, nPosDst
}

// UnpackBases converts 2-bit encoded sequence back to DNA.
// Requires the original sequence length and N positions.
func UnpackBases(packed []byte, nPos []uint16, seqLen int) []byte {
	return AppendUnpackBases(nil, packed, nPos, seqLen)
}

// AppendUnpackBases appends unpacked DNA sequence to dst.
func AppendUnpackBases(dst []byte, packed []byte, nPos []uint16, seqLen int) []byte {
	if seqLen == 0 {
		return dst
	}

	start := len(dst)
	needed := start + seqLen
	if cap(dst) < needed {
		// If dst is nil or empty, allocate exact size to avoid waste
		capNeeded := needed
		if cap(dst) > 0 {
			capNeeded = needed + needed/2 // Grow by 1.5x if extending
		}
		newDst := make([]byte, start, capNeeded)
		copy(newDst, dst)
		dst = newDst
	}
	dst = dst[:needed]

	bases := [4]byte{'A', 'C', 'G', 'T'}

	for i := range seqLen {
		val := (packed[i/4] >> ((i % 4) * 2)) & 0x03
		dst[start+i] = bases[val] //nolint:gosec // val is masked to 0-3
	}

	// Restore N bases
	for _, pos := range nPos {
		if int(pos) < seqLen {
			dst[start+int(pos)] = 'N'
		}
	}

	return dst
}
