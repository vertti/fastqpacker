package encoder

// DeltaEncode encodes quality scores using delta encoding in-place.
// Each value (except the first) becomes the difference from the previous value.
// This produces many zeros and small values for typical quality data,
// which compresses very well with entropy coders like zstd.
func DeltaEncode(qual []byte) {
	if len(qual) <= 1 {
		return
	}

	// Encode backwards to allow in-place operation
	for i := len(qual) - 1; i > 0; i-- {
		qual[i] -= qual[i-1]
	}
}

// DeltaDecode decodes delta-encoded quality scores in-place.
// Reverses the operation of DeltaEncode.
func DeltaDecode(qual []byte) {
	if len(qual) <= 1 {
		return
	}

	for i := 1; i < len(qual); i++ {
		qual[i] += qual[i-1]
	}
}
