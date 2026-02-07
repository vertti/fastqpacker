# Performance Journal

This file tracks performance experiments so we do not repeat work.

## Experiment Protocol

For every optimization candidate:

1. Run microbenchmarks before the change.
2. Make one focused change.
3. Run the same microbenchmarks after the change.
4. Keep the change only if results are positive.
5. If kept: run tests + lint, verify coverage for touched logic, then commit.

## Standard Benchmark Commands

Use these commands consistently for comparability:

```bash
GOCACHE=/tmp/fqpack-go-cache GOTMPDIR=/tmp /Users/vertti/.local/share/mise/installs/go/1.25.7/bin/go test ./internal/compress -run '^$' -bench 'BenchmarkCompress$|BenchmarkDecompress$|BenchmarkCompressParallel/workers=8$' -benchmem -count=3
```

```bash
GOCACHE=/tmp/fqpack-go-cache GOTMPDIR=/tmp /Users/vertti/.local/share/mise/installs/go/1.25.7/bin/go test ./internal/compress -run '^$' -bench 'BenchmarkCompressParallel$|BenchmarkCompressBlock$' -benchmem -count=3
```

```bash
GOCACHE=/tmp/fqpack-go-cache GOTMPDIR=/tmp /Users/vertti/.local/share/mise/installs/go/1.25.7/bin/go test ./internal/encoder -run '^$' -bench 'Benchmark(AppendPackBases|AppendUnpackBases|DeltaEncode|DeltaDecode)$' -benchmem -count=3
```

```bash
GOCACHE=/tmp/fqpack-go-cache GOTMPDIR=/tmp /Users/vertti/.local/share/mise/installs/go/1.25.7/bin/go test ./internal/parser -run '^$' -bench 'Benchmark(ReadBatch|Parser)$' -benchmem -count=3
```

## Experiment Log

### 2026-02-07 - E001 - Force single-worker path when first batch hits EOF

- Hypothesis: if input fits in first parsed batch, bypass parallel pipeline overhead.
- Change:
  - `internal/compress/compress.go`: route to `compressSingleWorkerWithBatch` when `firstBatchEOF == true`.
- Before (3 runs):
  - `BenchmarkCompress`: ~4.77 ms/op
  - `BenchmarkDecompress`: ~2.31 ms/op
  - `BenchmarkCompressParallel/workers=8`: ~39.7 ms/op
- After (3 runs):
  - `BenchmarkCompress`: ~4.92 ms/op
  - `BenchmarkDecompress`: ~2.45 ms/op
  - `BenchmarkCompressParallel/workers=8`: ~40.9 ms/op (with one run ~40.1 ms/op)
- Result: regression across key benches.
- Decision: **discarded** (change reverted).

### 2026-02-07 - E002 - Remove per-block output copy in parallel decompression

- Hypothesis: avoid `make+copy` in `decompressJobToBytes` by passing pooled buffers to ordered writer.
- Change:
  - `internal/compress/compress.go`
  - Replaced `decompressResult.data []byte` with `decompressResult.buf *bytes.Buffer`.
  - Workers now return pooled buffers directly; collector writes and returns buffers to pool.
  - Added cleanup paths so pooled buffers are returned on error.
- Before (3 runs):
  - `BenchmarkCompress`: ~4.99 ms/op
  - `BenchmarkDecompress`: ~2.34 ms/op, ~12.7-13.9 MB/op, 273-274 allocs/op
  - `BenchmarkCompressParallel/workers=8`: ~41.2 ms/op
- After (3 runs):
  - `BenchmarkCompress`: ~4.89 ms/op
  - `BenchmarkDecompress`: ~2.25 ms/op, ~9.9-10.2 MB/op, 271-272 allocs/op
  - `BenchmarkCompressParallel/workers=8`: ~40.9 ms/op
- Result: clear decompression improvement with lower allocations; compression unchanged/slightly better.
- Decision: **accepted**.

## Notes

- Existing uncommitted changes in `internal/compress/compress.go` were present before this session and should be evaluated separately with the same protocol.
