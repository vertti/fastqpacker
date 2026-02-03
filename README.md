# FastQPacker

Blazing fast parallel FASTQ compression. Single binary, no dependencies.

## Benchmarks

On 179MB of Illumina paired-end reads (500k reads):

| Tool | Size | Ratio | Compress | Speed |
|------|------|-------|----------|-------|
| **fqpack** | **28.7 MB** | **6.54x** | **344 ms** | **546 MB/s** |
| gzip | 35.5 MB | 5.29x | 11,068 ms | 17 MB/s |
| pigz | 35.5 MB | 5.29x | 1,471 ms | 128 MB/s |
| repaq | 38.7 MB | 4.84x | 641 ms | 293 MB/s |
| repaq+xz | 23.1 MB | 8.12x | 5,802 ms | 32 MB/s |

**32x faster than gzip. 23% smaller files.**

## Installation

```bash
go install github.com/vertti/fastqpacker/cmd/fqpack@latest
```

Or build from source:

```bash
git clone https://github.com/vertti/fastqpacker
cd fastqpacker
go build -o fqpack ./cmd/fqpack
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

## Limitations

- Illumina 4-line FASTQ format only (no multi-line sequences)
- Single-end files only (paired-end interleaving planned)
- No streaming decompression yet (full block buffering)

## License

MIT
