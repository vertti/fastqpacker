# FastQPacker

[![CI](https://github.com/vertti/fastqpacker/actions/workflows/ci.yml/badge.svg)](https://github.com/vertti/fastqpacker/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/vertti/fastqpacker)](https://goreportcard.com/report/github.com/vertti/fastqpacker)
[![Release](https://img.shields.io/github/v/release/vertti/fastqpacker)](https://github.com/vertti/fastqpacker/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/vertti/fastqpacker.svg)](https://pkg.go.dev/github.com/vertti/fastqpacker)

The fastest FASTQ compressor available, with better compression than gzip/pigz/zstd while being 10-80x faster. Specialized tools like DSRC or Spring compress smaller, but are 2-6x slower.

Pre-built binaries for macOS and Linux (both ARM and x86_64). Single binary, no dependencies.

## Benchmarks

Tested on ERR532393_1 (8.9GB Illumina reads), M4 MacBook Pro:

| Tool | Size | Ratio | Compress | Decompress | Speed |
|------|------|-------|----------|------------|-------|
| **fqpack** | **2,961 MB** | **3.25x** | **3.24s** | **2.95s** | **2,967.3 MB/s** |
| DSRC | 2,150 MB | 4.1x | 12s | 18s | 742 MB/s |
| zstd | 3,312 MB | 2.7x | 11s | 8s | 809 MB/s |
| pigz | 3,278 MB | 2.7x | 79s | 12s | 113 MB/s |
| repaq | 5,732 MB | 1.6x | 80s | 27s | 111 MB/s |
| repaq+xz | 2,761 MB | 3.2x | 388s | 40s | 23 MB/s |
| 7z | 2,584 MB | 3.4x | 1,442s | 83s | 6 MB/s |

fqpack is **14% smaller than pigz** with **24x faster compression** and **4x faster decompression**. DSRC compresses 24% smaller but is 3.6x slower to compress and 6x slower to decompress. FQSqueezer achieves the best known compression (1,511 MB, 5.9x ratio) but is ~100x slower.

Re-run fqpack-only 9GB benchmark:
```bash
./scripts/benchmark_fqpack_9gb.sh 3
```

## Installation

```bash
curl -fsSL https://raw.githubusercontent.com/vertti/fastqpacker/main/install.sh | sh
```

Or with Go:

```bash
go install github.com/vertti/fastqpacker/cmd/fqpack@latest
```

## Usage

```bash
# Compress
fqpack -i reads.fq -o reads.fqz

# Decompress
fqpack -d -i reads.fqz -o reads.fq

# Stdin/stdout (Unix pipes)
cat reads.fq | fqpack -c > reads.fqz
fqpack -d < reads.fqz > reads.fq

# Control parallelism
fqpack -w 4 -i reads.fq -o reads.fqz
```

## How It Works

- **2-bit sequence encoding**: ACGT packed 4 bases per byte (N positions stored separately)
- **Delta-encoded quality scores**: Adjacent quality scores are similar, deltas compress well
- **zstd compression**: Modern entropy coding beats gzip's DEFLATE
- **Parallel block processing**: Scales across all CPU cores
- **Built-in integrity verification**: CRC32 checksums detect corruption on decompress
- **Auto-detected quality encoding**: Phred+33 and Phred+64 handled transparently
- **Lossless FASTQ record preservation**: Optional line-3 plus payload (`+...`) is preserved

## Limitations

- Illumina 4-line FASTQ format only (no multi-line sequences)
- No streaming decompression (full block buffering)

## License

MIT
