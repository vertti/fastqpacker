package parser

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseRecord(t *testing.T) {
	input := `@SEQ_ID description
ACGTACGT
+
IIIIIIII
`
	p := New(strings.NewReader(input))
	rec, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.Header != "SEQ_ID description" {
		t.Errorf("header = %q, want %q", rec.Header, "SEQ_ID description")
	}
	if string(rec.Sequence) != "ACGTACGT" {
		t.Errorf("sequence = %q, want %q", rec.Sequence, "ACGTACGT")
	}
	if string(rec.Quality) != "IIIIIIII" {
		t.Errorf("quality = %q, want %q", rec.Quality, "IIIIIIII")
	}
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

	expected := []struct {
		header string
		seq    string
		qual   string
	}{
		{"SEQ_1", "AAAA", "!!!!"},
		{"SEQ_2", "CCCC", "####"},
		{"SEQ_3", "GGGG", "$$$$"},
	}

	for i, exp := range expected {
		rec, err := p.Next()
		if err != nil {
			t.Fatalf("record %d: unexpected error: %v", i, err)
		}
		if rec.Header != exp.header {
			t.Errorf("record %d: header = %q, want %q", i, rec.Header, exp.header)
		}
		if string(rec.Sequence) != exp.seq {
			t.Errorf("record %d: sequence = %q, want %q", i, rec.Sequence, exp.seq)
		}
		if string(rec.Quality) != exp.qual {
			t.Errorf("record %d: quality = %q, want %q", i, rec.Quality, exp.qual)
		}
	}

	// Should get EOF after all records
	_, err := p.Next()
	if err == nil {
		t.Error("expected EOF, got nil")
	}
}

func TestParseEmptyInput(t *testing.T) {
	p := New(strings.NewReader(""))
	_, err := p.Next()
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParseMalformedNoAt(t *testing.T) {
	input := `SEQ_ID
ACGT
+
IIII
`
	p := New(strings.NewReader(input))
	_, err := p.Next()
	if err == nil {
		t.Error("expected error for missing @")
	}
}

func TestParseMalformedMismatchedLength(t *testing.T) {
	input := `@SEQ_ID
ACGTACGT
+
III
`
	p := New(strings.NewReader(input))
	_, err := p.Next()
	if err == nil {
		t.Error("expected error for mismatched sequence/quality length")
	}
}

func TestParseWithNBases(t *testing.T) {
	input := `@SEQ_ID
ACNTNACGT
+
IIIIIIIII
`
	p := New(strings.NewReader(input))
	rec, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(rec.Sequence) != "ACNTNACGT" {
		t.Errorf("sequence = %q, want %q", rec.Sequence, "ACNTNACGT")
	}
}

func TestParseIlluminaHeader(t *testing.T) {
	// Real Illumina header format
	input := `@HWI-ST123:4:1101:14346:1976#0/1
ACGTACGTACGTACGTACGTACGTACGTACGTACGTACGT
+
IIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIIII
`
	p := New(strings.NewReader(input))
	rec, err := p.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Header != "HWI-ST123:4:1101:14346:1976#0/1" {
		t.Errorf("header = %q, want %q", rec.Header, "HWI-ST123:4:1101:14346:1976#0/1")
	}
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(batch) != 100 {
		t.Errorf("batch size = %d, want 100", len(batch))
	}
}

func BenchmarkParser(b *testing.B) {
	// Generate a reasonably sized input
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
