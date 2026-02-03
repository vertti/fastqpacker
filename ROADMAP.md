# FastQPacker Roadmap

## Current Status: MVP Complete ✅

Working FASTQ compression tool with CLI.

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

### Milestone 5: CLI ✅
- [x] Full CLI with flags: -i, -o, -d, -c, -b
- [x] stdin/stdout support
- [x] Compress and decompress modes

## Upcoming Milestones

### Milestone 4: Parallelism (Future)
- [ ] Add worker pool with errgroup + channels
- [ ] Add sync.Pool for buffer reuse
- [ ] Parallel block compression/decompression

### Milestone 6: Polish (Future)
- [ ] CRC32 integrity per block
- [ ] Benchmark against gzip/pigz on real Illumina data
- [ ] Handle edge cases: Phred+64, spaces in headers
- [ ] Paired-end interleaving support

## Future Enhancements
- Zstd dictionary training on representative FASTQ files
- Statistical analysis of sequence patterns
- Custom lookup tables based on k-mer frequencies
- Context modeling for quality scores
- Non-Illumina platform support

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
- Single-threaded (parallelism not yet implemented)
- Only supports Illumina 4-line FASTQ format
- No support for wrapped sequences (multi-line)
- Assumes standard Phred+33 quality encoding
