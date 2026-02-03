# FastQPacker Roadmap

## Current Status: v0.2.0 Released ✅

Fast FASTQ compression with excellent ratio/speed balance.

### Benchmarks (188MB test file)

| Tool      | Size    | Ratio | Compress | Speed     |
|-----------|---------|-------|----------|-----------|
| fqpack    | 28.7MB  | 6.54x | 359ms    | 523 MB/s  |
| pigz      | 35.5MB  | 5.29x | 1534ms   | 123 MB/s  |
| repaq     | 38.7MB  | 4.84x | 690ms    | 272 MB/s  |
| repaq+xz  | 23.1MB  | 8.12x | 5979ms   | 31 MB/s   |

## Completed Milestones

### Milestone 0: Project Setup ✅
- [x] Create mise.toml, go.mod, Makefile, .golangci.yml
- [x] Create CLAUDE.md and ROADMAP.md
- [x] Empty cmd/fqpack/main.go that compiles

### Milestone 1: Parser ✅
- [x] Write tests for FASTQ parsing (TDD)
- [x] Implement fast parser with bufio.Reader
- [x] Supports Illumina 4-line format

### Milestone 2: Encoders ✅
- [x] Write tests for 2-bit encoding and delta encoding
- [x] Implement PackBases() - ACGT → 2 bits, N positions separate
- [x] Implement DeltaEncode() / DeltaDecode() for quality scores

### Milestone 3: Single-threaded Compression ✅
- [x] Write integration test: compress → decompress → compare
- [x] Wire up: parser → encoders → zstd → output file
- [x] FQZ file format with magic, version, and block headers

### Milestone 4: Parallelism ✅
- [x] Add worker pool with errgroup + channels
- [x] Add sync.Pool for buffer reuse
- [x] Parallel block compression/decompression
- [x] -w flag for worker count control

### Milestone 5: CLI ✅
- [x] Full CLI with flags: -i, -o, -d, -c, -b
- [x] stdin/stdout support
- [x] Compress and decompress modes

### Milestone 6: Format Support ✅
- [x] Auto-detect Phred+64 (Illumina 1.3-1.7) vs Phred+33 (modern)
- [x] Normalize quality scores for optimal compression
- [x] Preserve original encoding on decompression

## Upcoming Milestones

### Milestone 7: Integrity & Robustness
- [ ] CRC32 checksums per block for data integrity verification
- [ ] Handle edge cases: spaces in headers, unusual characters

## Future Enhancements

### Performance
- Parallel decompression (currently single-threaded, ~980ms vs gzip's 143ms)
- Zstd dictionary training on representative FASTQ files
- Memory-mapped I/O for faster reads

### Compression Ratio
- Context modeling for quality scores
- K-mer frequency analysis for better sequence encoding
- Exploit pair correlation in R1+R2 files

### Platform Support
- Non-Illumina platforms (PacBio, Nanopore)
- Multi-line wrapped sequences

### Low Priority: Paired-end Interleaving
Paired-end data already works fine with fqpack - just compress R1 and R2 files separately,
or compress interleaved FASTQ as-is (standard 4-line records work unchanged).

Potential future enhancements:
- Interleave/deinterleave convenience commands (but seqkit already does this well)
- Compress R1+R2 together exploiting pair correlation (~5-10% better compression)
- These add complexity for marginal gains, so deprioritized

## Usage

```bash
# Compress
fqpack -i sample.fq -o sample.fqz

# Decompress
fqpack -d -i sample.fqz -o sample.fq

# Stdin/stdout
cat sample.fq | fqpack -c > sample.fqz
fqpack -d < sample.fqz > sample.fq
```

## Known Limitations
- Only supports Illumina 4-line FASTQ format
- No support for wrapped sequences (multi-line)
- No support for Solexa+64 encoding (Illumina 1.0-1.2, uses different formula)
