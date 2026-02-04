# FastQPacker

The fastest FASTQ compressor available, with better compression than gzip/pigz/zstd while being 10-80x faster. Specialized tools like DSRC or Spring compress smaller, but are 2-6x slower.

Pre-built binaries for macOS and Linux (both ARM and x86_64). Single binary, no dependencies.

## Benchmarks

On 188MB of Illumina paired-end reads:

| Tool | Size | Ratio | Compress | Decompress | Speed |
|------|------|-------|----------|------------|-------|
| **fqpack** | **28.7 MB** | **6.54x** | **292 ms** | **137 ms** | **644 MB/s** |
| gzip | 35.5 MB | 5.29x | 11,368 ms | 142 ms | 17 MB/s |
| pigz | 35.5 MB | 5.29x | 1,482 ms | 147 ms | 127 MB/s |
| repaq | 38.7 MB | 4.84x | 644 ms | 496 ms | 292 MB/s |
| repaq+xz | 23.1 MB | 8.12x | 5,902 ms | 794 ms | 32 MB/s |

**Fastest compression AND decompression. 23% smaller than gzip.**

### Large file benchmark

Tested on ERR532393_1 (8.9GB Illumina reads), M4 MacBook Pro:

| Tool | Size | Compress | Decompress |
|------|------|----------|------------|
| **fqpack** | **2,825 MB** | **6s** | **3s** |
| DSRC | 2,150 MB | 12s | 18s |
| pigz | 3,278 MB | 79s | 12s |
| zstd | 3,312 MB | 11s | 8s |
| 7z | 2,584 MB | 1,442s | 83s |
| repaq | 5,732 MB | 80s | 27s |
| repaq+xz | 2,761 MB | 388s | 40s |

fqpack is **14% smaller than pigz** with **13x faster compression** and **4x faster decompression**. DSRC compresses 24% smaller but is 2x slower to compress and 6x slower to decompress. FQSqueezer achieves the best known compression (1,511 MB) but is ~100x slower.

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

## Limitations

- Illumina 4-line FASTQ format only (no multi-line sequences)
- No streaming decompression (full block buffering)

Phred+33 and Phred+64 quality encodings are auto-detected.

## License

MIT
