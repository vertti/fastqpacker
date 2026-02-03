# FastQPacker Development Guide

## Planning
- Use plan mode for any task larger than a few lines
- Reference ROADMAP.md for project priorities and status

## Development Approach
- TDD: Write tests first, then implementation
- Keep commits small and working - every commit should compile and pass tests
- Run `make lint` before committing

## Quick Commands
- `make test` - run all tests
- `make lint` - run linters
- `make build` - build binary
- `make bench` - run benchmarks

## Architecture Overview
- Block-based parallel compression (100k reads/block)
- Separate encoding for sequences (2-bit), quality (delta), headers (template)
- zstd for final compression of all streams
- sync.Pool for buffer reuse (critical for performance)

## File Format
- Magic: 'FQZ\x00'
- Block-based for parallel processing
- Each block: sequences + qualities + headers compressed separately
