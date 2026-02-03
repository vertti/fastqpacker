# FastQPacker Roadmap

## Current Status: Milestone 0 - Project Setup ✅

## Completed Milestones

### Milestone 0: Project Setup
- [x] Create mise.toml, go.mod, Makefile, .golangci.yml
- [x] Create CLAUDE.md and ROADMAP.md
- [x] Empty cmd/fqpack/main.go that compiles

## In Progress

### Milestone 1: Parser
- [ ] Write tests for FASTQ parsing (TDD)
- [ ] Implement fast parser with bufio.Reader + bytes.IndexByte
- [ ] Target: Parse Illumina 4-line format, >500 MB/s

## Upcoming Milestones

### Milestone 2: Encoders
- [ ] Write tests for 2-bit encoding and delta encoding
- [ ] Implement PackBases() - ACGT → 2 bits, N positions separate
- [ ] Implement DeltaEncode() / DeltaDecode() for quality scores

### Milestone 3: Single-threaded Compression
- [ ] Write integration test: compress → decompress → compare
- [ ] Wire up: parser → encoders → zstd → output file
- [ ] Simple file format with magic + version + single block

### Milestone 4: Block Format + Parallelism
- [ ] Refactor to block-based format (100k reads/block)
- [ ] Add worker pool with errgroup + channels
- [ ] Add sync.Pool for buffer reuse

### Milestone 5: CLI + Paired-End
- [ ] Full CLI with flags: -1, -2, -o, -t, -d
- [ ] stdin/stdout support (-c flag)
- [ ] Interleaved paired-end support

### Milestone 6: Polish
- [ ] CRC32 integrity per block
- [ ] Benchmark against gzip/pigz on real Illumina data
- [ ] Handle edge cases: Phred+64, spaces in headers

## Future Enhancements (Post-Weekend)
- Zstd dictionary training on representative FASTQ files
- Statistical analysis of sequence patterns
- Custom lookup tables based on k-mer frequencies
- Context modeling for quality scores
- Non-Illumina platform support

## Performance Targets

| Metric | gzip | pigz | Target | Stretch |
|--------|------|------|--------|---------|
| Compression ratio | 1.0x | 1.0x | 3-4x | 5x |
| Compress speed (MB/s) | 50 | 300 | 400+ | 600+ |
| Decompress speed (MB/s) | 100 | 400 | 800+ | 1000+ |
| Memory | 10MB | 100MB | 1-2GB | <1GB |

## Known Limitations
- Currently only supports Illumina 4-line FASTQ format
- No support for wrapped sequences (multi-line)
- Assumes standard Phred+33 quality encoding
