// Package encoder provides encoding functions for FASTQ components.
package encoder

import "slices"

// MaxSequenceLength is the maximum supported sequence length.
// Sequences longer than this will be truncated for N position tracking.
const MaxSequenceLength = 1 << 16 // 65536

// baseLookup maps ASCII byte to 2-bit encoding. A=0, C=1, G=2, T=3.
// Unknown bases (including N) map to 0 (same as A).
var baseLookup [256]byte

// isNBase maps ASCII byte to 1 if it's an N/ambiguous base, 0 otherwise.
var isNBase [256]byte

func init() {
	// Default is 0 (A encoding) for all bytes
	baseLookup['A'] = 0
	baseLookup['a'] = 0
	baseLookup['C'] = 1
	baseLookup['c'] = 1
	baseLookup['G'] = 2
	baseLookup['g'] = 2
	baseLookup['T'] = 3
	baseLookup['t'] = 3

	// Mark all bytes as N by default, then clear known bases
	for i := range isNBase {
		isNBase[i] = 1
	}
	for _, b := range []byte("ACGTacgt") {
		isNBase[b] = 0
	}
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

	n := len(seq)
	packed = make([]byte, (n+3)>>2)

	// Process 4 bases at a time (one full output byte per iteration)
	fullBytes := n >> 2
	for i := 0; i < fullBytes; i++ {
		base := i << 2
		packed[i] = baseLookup[seq[base]] |
			(baseLookup[seq[base+1]] << 2) |
			(baseLookup[seq[base+2]] << 4) |
			(baseLookup[seq[base+3]] << 6)
	}

	// Handle remaining 1-3 bases
	remaining := n & 3
	if remaining > 0 {
		base := fullBytes << 2
		var b byte
		for j := 0; j < remaining; j++ {
			b |= baseLookup[seq[base+j]] << (j << 1)
		}
		packed[fullBytes] = b
	}

	// Separate pass for N detection (N bases are rare, branch almost always not-taken)
	limit := n
	if limit > MaxSequenceLength {
		limit = MaxSequenceLength
	}
	for i := 0; i < limit; i++ {
		if isNBase[seq[i]] != 0 {
			nPos = append(nPos, uint16(i)) //nolint:gosec // bounds checked above
		}
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

	// Process 4 bases at a time
	fullBytes := seqLen >> 2
	for i := 0; i < fullBytes; i++ {
		b := packed[i]
		base := i << 2
		seq[base] = bases[b&0x03]
		seq[base+1] = bases[(b>>2)&0x03]
		seq[base+2] = bases[(b>>4)&0x03]
		seq[base+3] = bases[(b>>6)&0x03]
	}

	// Handle remaining 1-3 bases
	remaining := seqLen & 3
	if remaining > 0 {
		b := packed[fullBytes]
		base := fullBytes << 2
		for j := 0; j < remaining; j++ {
			seq[base+j] = bases[(b>>(j<<1))&0x03]
		}
	}

	// Restore N bases
	for _, pos := range nPos {
		seq[pos] = 'N'
	}

	return seq
}

// AppendPackedBases appends 2-bit encoded bases to dst, returning the
// updated slice. Like PackBases but avoids allocation when dst has capacity.
func AppendPackedBases(dst []byte, seq []byte, nPos *[]uint16) []byte {
	n := len(seq)
	if n == 0 {
		return dst
	}

	packedLen := (n + 3) >> 2

	start := len(dst)
	dst = slices.Grow(dst, packedLen)[:start+packedLen]
	packed := dst[start:]

	// Process 4 bases at a time (one full output byte per iteration)
	fullBytes := n >> 2
	for i := 0; i < fullBytes; i++ {
		base := i << 2
		packed[i] = baseLookup[seq[base]] |
			(baseLookup[seq[base+1]] << 2) |
			(baseLookup[seq[base+2]] << 4) |
			(baseLookup[seq[base+3]] << 6)
	}

	// Handle remaining 1-3 bases
	remaining := n & 3
	if remaining > 0 {
		base := fullBytes << 2
		var b byte
		for j := 0; j < remaining; j++ {
			b |= baseLookup[seq[base+j]] << (j << 1)
		}
		packed[fullBytes] = b
	}

	// Separate pass for N detection
	limit := n
	if limit > MaxSequenceLength {
		limit = MaxSequenceLength
	}
	for i := 0; i < limit; i++ {
		if isNBase[seq[i]] != 0 {
			*nPos = append(*nPos, uint16(i)) //nolint:gosec // bounds checked above
		}
	}

	return dst
}

// AppendUnpackBases appends unpacked DNA bases to dst, returning the
// updated slice. Like UnpackBases but avoids allocation when dst has capacity.
func AppendUnpackBases(dst []byte, packed []byte, nPos []uint16, seqLen int) []byte {
	if seqLen == 0 {
		return dst
	}

	start := len(dst)
	dst = slices.Grow(dst, seqLen)[:start+seqLen]
	seq := dst[start:]

	bases := [4]byte{'A', 'C', 'G', 'T'}

	// Process 4 bases at a time
	fullBytes := seqLen >> 2
	for i := 0; i < fullBytes; i++ {
		b := packed[i]
		base := i << 2
		seq[base] = bases[b&0x03]
		seq[base+1] = bases[(b>>2)&0x03]
		seq[base+2] = bases[(b>>4)&0x03]
		seq[base+3] = bases[(b>>6)&0x03]
	}

	// Handle remaining 1-3 bases
	remaining := seqLen & 3
	if remaining > 0 {
		b := packed[fullBytes]
		base := fullBytes << 2
		for j := 0; j < remaining; j++ {
			seq[base+j] = bases[(b>>(j<<1))&0x03]
		}
	}

	// Restore N bases
	for _, pos := range nPos {
		seq[pos] = 'N'
	}

	return dst
}
