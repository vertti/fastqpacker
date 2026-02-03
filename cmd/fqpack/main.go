// fqpack compresses and decompresses FASTQ files.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/vertti/fastqpacker/internal/compress"
)

var version = "dev"

const (
	exitSuccess = 0
	exitError   = 1
)

type config struct {
	decompress bool
	inputFile  string
	outputFile string
	toStdout   bool
	blockSize  uint
	workers    int
}

func main() {
	os.Exit(run())
}

func run() int {
	cfg, done := parseFlags()
	if done {
		return exitSuccess
	}

	input, cleanup, err := openInput(cfg.inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitError
	}
	defer cleanup()

	output, cleanup, err := openOutput(cfg.outputFile, cfg.toStdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitError
	}
	defer cleanup()

	if err := execute(cfg, input, output); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitError
	}

	return exitSuccess
}

func parseFlags() (config, bool) {
	var cfg config
	var showVersion, showHelp bool

	flag.BoolVar(&cfg.decompress, "d", false, "decompress mode")
	flag.StringVar(&cfg.inputFile, "i", "", "input file (default: stdin)")
	flag.StringVar(&cfg.outputFile, "o", "", "output file (default: stdout)")
	flag.BoolVar(&cfg.toStdout, "c", false, "write to stdout (compress mode)")
	flag.UintVar(&cfg.blockSize, "b", compress.DefaultBlockSize, "records per block")
	flag.IntVar(&cfg.workers, "w", 0, "compression workers (default: NumCPU)")
	flag.BoolVar(&showVersion, "version", false, "show version and exit")
	flag.BoolVar(&showHelp, "h", false, "show help")

	flag.Usage = usage
	flag.Parse()

	if showHelp {
		flag.Usage()
		return cfg, true
	}

	if showVersion {
		fmt.Printf("fqpack version %s\n", version)
		return cfg, true
	}

	// Handle positional arguments
	args := flag.Args()
	if len(args) > 0 && cfg.inputFile == "" {
		cfg.inputFile = args[0]
	}
	if len(args) > 1 && cfg.outputFile == "" {
		cfg.outputFile = args[1]
	}

	return cfg, false
}

func usage() {
	fmt.Fprintf(os.Stderr, `fqpack - Fast FASTQ compression tool

Usage:
  fqpack [options] [-i input.fq] [-o output.fqz]   Compress FASTQ
  fqpack -d [-i input.fqz] [-o output.fq]          Decompress

Options:
`)
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, `
Examples:
  fqpack -i sample.fq -o sample.fqz          Compress file
  fqpack -d -i sample.fqz -o sample.fq       Decompress file
  cat sample.fq | fqpack -c > sample.fqz     Compress from stdin
  fqpack -d < sample.fqz > sample.fq         Decompress to stdout
`)
}

func openInput(path string) (io.Reader, func(), error) {
	if path == "" || path == "-" {
		return os.Stdin, func() {}, nil
	}

	f, err := os.Open(path) //nolint:gosec // CLI tool needs to open user-specified files
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open input: %w", err)
	}
	return f, func() { _ = f.Close() }, nil
}

func openOutput(path string, toStdout bool) (io.Writer, func(), error) {
	if path == "" || path == "-" || toStdout {
		return os.Stdout, func() {}, nil
	}

	f, err := os.Create(path) //nolint:gosec // CLI tool needs to create user-specified files
	if err != nil {
		return nil, nil, fmt.Errorf("cannot create output: %w", err)
	}
	return f, func() { _ = f.Close() }, nil
}

func execute(cfg config, input io.Reader, output io.Writer) error {
	if cfg.decompress {
		opts := &compress.DecompressOptions{
			Workers: cfg.workers,
		}
		return compress.Decompress(input, output, opts)
	}

	opts := &compress.Options{
		BlockSize: uint32(cfg.blockSize), //nolint:gosec // bounded by flag default
		Workers:   cfg.workers,
	}
	return compress.Compress(input, output, opts)
}
