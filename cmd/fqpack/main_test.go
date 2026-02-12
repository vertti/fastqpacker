package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenInputPlainFASTQ(t *testing.T) {
	t.Parallel()

	want := []byte("@r1\nACGT\n+\n!!!!\n")
	path := filepath.Join(t.TempDir(), "reads.fastq")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	r, cleanup, err := openInput(path, false)
	if err != nil {
		t.Fatalf("openInput: %v", err)
	}
	defer cleanup()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read input: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("content mismatch: got %q want %q", got, want)
	}
}

func TestOpenInputGzipByExtension(t *testing.T) {
	t.Parallel()

	want := []byte("@r1\nACGT\n+\n!!!!\n")
	path := filepath.Join(t.TempDir(), "reads.fastq.gz")
	writeGzipFile(t, path, want)

	r, cleanup, err := openInput(path, false)
	if err != nil {
		t.Fatalf("openInput: %v", err)
	}
	defer cleanup()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read input: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("content mismatch: got %q want %q", got, want)
	}
}

func TestOpenInputGzipByMagicBytes(t *testing.T) {
	t.Parallel()

	want := []byte("@r1\nACGT\n+\n!!!!\n")
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "reads.bin")
	gzPath := filepath.Join(tmpDir, "reads.fastq.gz")
	writeGzipFile(t, gzPath, want)

	rawGz, err := os.ReadFile(gzPath)
	if err != nil {
		t.Fatalf("read gzip fixture: %v", err)
	}
	if err := os.WriteFile(path, rawGz, 0o600); err != nil {
		t.Fatalf("write raw gzip fixture: %v", err)
	}

	r, cleanup, err := openInput(path, false)
	if err != nil {
		t.Fatalf("openInput: %v", err)
	}
	defer cleanup()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read input: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("content mismatch: got %q want %q", got, want)
	}
}

func TestOpenInputDecompressModeDoesNotAutoGunzip(t *testing.T) {
	t.Parallel()

	want := []byte("@r1\nACGT\n+\n!!!!\n")
	path := filepath.Join(t.TempDir(), "reads.fastq.gz")
	writeGzipFile(t, path, want)

	rawGz, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw gzip: %v", err)
	}

	r, cleanup, err := openInput(path, true)
	if err != nil {
		t.Fatalf("openInput: %v", err)
	}
	defer cleanup()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read input: %v", err)
	}

	if !bytes.Equal(got, rawGz) {
		t.Fatalf("decompress mode should return raw bytes")
	}
}

func TestOpenInputStdinGzipMagicDetection(t *testing.T) {
	t.Parallel()

	want := []byte("@r1\nACGT\n+\n!!!!\n")
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() { _ = pr.Close() }()

	var gzData bytes.Buffer
	gz := gzip.NewWriter(&gzData)
	if _, err := gz.Write(want); err != nil {
		t.Fatalf("write gzip payload: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}

	go func() {
		_, _ = pw.Write(gzData.Bytes())
		_ = pw.Close()
	}()

	originalStdin := os.Stdin
	os.Stdin = pr
	defer func() { os.Stdin = originalStdin }()

	r, cleanup, err := openInput("-", false)
	if err != nil {
		t.Fatalf("openInput: %v", err)
	}
	defer cleanup()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read input: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("content mismatch: got %q want %q", got, want)
	}
}

func writeGzipFile(t *testing.T, path string, data []byte) {
	t.Helper()

	f, err := os.Create(path) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatalf("create gzip file: %v", err)
	}
	defer func() { _ = f.Close() }()

	gz := gzip.NewWriter(f)
	if _, err := gz.Write(data); err != nil {
		t.Fatalf("write gzip data: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
}
