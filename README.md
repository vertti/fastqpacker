# FastQPacker

Blazing fast parallel FASTQ compression. Single binary, no dependencies.

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

### Large file performance (M4 MacBook Pro)

| File | Compress | Decompress | Ratio |
|------|----------|------------|-------|
| 15 GB FASTQ | 10s (1.5 GB/s) | 5s (3 GB/s) | 8.3x |

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
