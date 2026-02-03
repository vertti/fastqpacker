// fqscramble scrambles FASTQ files to remove identifiable sequence information
// while preserving realistic characteristics for benchmarking.
//
// It shuffles bases within each read, which:
// - Preserves base composition (A/C/G/T/N ratios per read)
// - Preserves quality score distribution (scores stay with their positions)
// - Preserves read lengths and header formats
// - Destroys actual genomic sequences (no alignment possible)
package main

import (
	"bufio"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		inputFile  = flag.String("i", "", "input FASTQ file (supports .gz)")
		outputFile = flag.String("o", "", "output FASTQ file (default: stdout)")
		seed       = flag.Uint64("seed", 42, "random seed for reproducibility")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `fqscramble - Scramble FASTQ files for privacy

Shuffles bases within each read to destroy sequence information while
preserving base composition, quality distributions, and read lengths.

Usage:
  fqscramble -i input.fastq.gz -o output.fastq
  zcat input.fastq.gz | fqscramble > output.fastq

Options:
`)
		flag.PrintDefaults()
	}

	flag.Parse()

	// Handle positional argument
	if *inputFile == "" && flag.NArg() > 0 {
		*inputFile = flag.Arg(0)
	}

	reader, cleanup, err := openInput(*inputFile)
	if err != nil {
		return err
	}
	defer cleanup()

	writer, cleanup, err := openOutput(*outputFile)
	if err != nil {
		return err
	}
	defer cleanup()

	// Create deterministic RNG for reproducible scrambling
	//nolint:gosec // intentionally using math/rand for reproducibility, not security
	rng := rand.New(rand.NewPCG(*seed, *seed))

	return scramble(reader, writer, rng)
}

func openInput(path string) (io.Reader, func(), error) {
	if path == "" || path == "-" {
		return os.Stdin, func() {}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("opening input: %w", err)
	}

	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, nil, fmt.Errorf("creating gzip reader: %w", err)
		}
		return gz, func() { gz.Close(); f.Close() }, nil
	}

	return f, func() { f.Close() }, nil
}

func openOutput(path string) (io.Writer, func(), error) {
	if path == "" || path == "-" {
		return os.Stdout, func() {}, nil
	}

	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("creating output: %w", err)
	}
	return f, func() { f.Close() }, nil
}

func scramble(r io.Reader, w io.Writer, rng *rand.Rand) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	bw := bufio.NewWriter(w)
	defer bw.Flush()

	lineNum := 0
	var header, seq, plus, qual string

	for scanner.Scan() {
		line := scanner.Text()
		pos := lineNum % 4

		switch pos {
		case 0:
			header = line
		case 1:
			seq = line
		case 2:
			plus = line
		case 3:
			qual = line
			scrambledSeq := shuffleString(seq, rng)

			fmt.Fprintln(bw, header)
			fmt.Fprintln(bw, scrambledSeq)
			fmt.Fprintln(bw, plus)
			fmt.Fprintln(bw, qual)
		}

		lineNum++
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading input: %w", err)
	}

	return bw.Flush()
}

func shuffleString(s string, rng *rand.Rand) string {
	runes := []rune(s)
	rng.Shuffle(len(runes), func(i, j int) {
		runes[i], runes[j] = runes[j], runes[i]
	})
	return string(runes)
}
