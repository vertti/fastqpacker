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

### Comparison with FQSqueezer paper tools

Tested on ERR532393_1 (8.9GB) from [FQSqueezer paper](https://www.nature.com/articles/s41598-020-57452-6/tables/1), M4 MacBook Pro:

| Tool | Size | Compress | Decompress |
|------|------|----------|------------|
| **fqpack** | **2,825 MB** | **7s** | **3s** |
| pigz | 3,278 MB | 81s | 12s |
| zstd | 3,312 MB | 12s | 8s |
| 7z | 2,584 MB | 1,486s | 87s |
| repaq | 5,734 MB | 83s | 27s |
| repaq+xz | 2,746 MB | 414s | 40s |

Paper results (different hardware, same dataset):

| Tool | Size | Compress | Decompress |
|------|------|----------|------------|
| pigz | 3,392 MB | 128s | 54s |
| 7z | 2,710 MB | 2,438s | 220s |
| zstd | 3,335 MB | 828s | 35s |
| Spring | 1,650 MB | 159s | 24s |
| FQSqueezer | 1,511 MB | 1,409s | 1,501s |

fqpack achieves **16% smaller than pigz** with **12x faster compression** and **4x faster decompression**. Only specialized FASTQ compressors (Spring, FQSqueezer) achieve better compression, at significant speed cost.

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
