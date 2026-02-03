package encoder

// Phred encoding offsets.
const (
	Phred33Offset = 33
	Phred64Offset = 64
)

// QualityEncoding represents the quality score encoding scheme.
type QualityEncoding uint8

// Quality encoding schemes.
const (
	EncodingPhred33 QualityEncoding = iota // Sanger/Illumina 1.8+ (offset 33)
	EncodingPhred64                        // Illumina 1.3-1.7 (offset 64)
)

// DetectEncoding scans quality bytes and returns the likely encoding.
// If any quality byte < 59 (';'), it's definitely Phred+33.
// If minimum byte >= 64 ('@'), it's Phred+64.
// Otherwise (ambiguous 59-63 range), defaults to Phred+33.
func DetectEncoding(qualities [][]byte) QualityEncoding {
	minByte := byte(255)

	for _, qual := range qualities {
		for _, b := range qual {
			if b < minByte {
				minByte = b
			}
			// Early exit: anything below ASCII 59 is definitely Phred+33
			if b < 59 {
				return EncodingPhred33
			}
		}
	}

	// If we found no bytes, default to Phred+33
	if minByte == 255 {
		return EncodingPhred33
	}

	// If minimum is >= 64 ('@'), it's Phred+64
	if minByte >= 64 {
		return EncodingPhred64
	}

	// Ambiguous range (59-63), default to Phred+33
	return EncodingPhred33
}

// NormalizeQuality converts quality bytes to 0-based values in-place.
// This normalizes both Phred+33 and Phred+64 to 0-based quality scores.
func NormalizeQuality(qual []byte, enc QualityEncoding) {
	offset := byte(Phred33Offset)
	if enc == EncodingPhred64 {
		offset = byte(Phred64Offset)
	}

	for i := range qual {
		qual[i] -= offset
	}
}

// DenormalizeQuality converts 0-based values back to ASCII in-place.
// Restores quality bytes to the original encoding scheme.
func DenormalizeQuality(qual []byte, enc QualityEncoding) {
	offset := byte(Phred33Offset)
	if enc == EncodingPhred64 {
		offset = byte(Phred64Offset)
	}

	for i := range qual {
		qual[i] += offset
	}
}

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
