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

### 2026-02-07 - E003 - Set zstd internal concurrency to 1 per worker

- Hypothesis: fqpack already parallelizes at block-worker level, so zstd internal concurrency adds nested overhead.
- Change:
  - Added shared zstd options in `internal/compress/compress.go`:
    - `zstd.WithEncoderConcurrency(1)`
    - `zstd.WithDecoderConcurrency(1)`
  - Applied to all encoder/decoder constructions in compress/decompress workers.
- Before (3 runs):
  - `BenchmarkCompress`: ~4.80 ms/op, ~106-111 MB/op, 279-291 allocs/op
  - `BenchmarkDecompress`: ~2.21 ms/op, ~9.7-10.1 MB/op, 271-272 allocs/op
  - `BenchmarkCompressParallel/workers=8`: ~40.2-40.9 ms/op, ~201-235 MB/op, 310-353 allocs/op
  - `BenchmarkCompressBlock`: ~19.1-19.4 ms/op
- After (3 runs):
  - `BenchmarkCompress`: ~4.24-4.28 ms/op, ~35.7-39.2 MB/op, 151-157 allocs/op
  - `BenchmarkDecompress`: ~2.17 ms/op, ~9.4-9.6 MB/op, 174-175 allocs/op
  - `BenchmarkCompressParallel/workers=8`: ~39.7-40.5 ms/op, ~89.8-124.5 MB/op, 170-198 allocs/op
  - `BenchmarkCompressBlock`: ~19.7-19.8 ms/op
- Result: strong end-to-end compression and allocation win; tiny regression in isolated block benchmark.
- Decision: **accepted**.

### 2026-02-07 - E004 - Inline quality encoding detection over records

- Hypothesis: avoid temporary `[][]byte` allocation in `Compress` quality-encoding detection.
- Change:
  - Replaced `encoder.DetectEncoding(qualities)` setup with direct scan over `Record.Quality`.
- Before (3 runs):
  - `BenchmarkCompress`: ~4.15-4.20 ms/op
  - `BenchmarkCompressParallel/workers=8`: ~38.5-39.2 ms/op
- After (3 runs):
  - `BenchmarkCompress`: ~4.24-4.25 ms/op
  - `BenchmarkCompressParallel/workers=8`: ~39.1-39.5 ms/op
- Result: no meaningful win; main compression benchmark regressed.
- Decision: **discarded** (change reverted).

### 2026-02-07 - E005 - Replace looped stream writes with direct writes

- Hypothesis: avoid small temporary `[][]byte` allocation in `compressBlockWithBuffers`.
- Change:
  - Replaced `for _, data := range [][]byte{...}` write loop with five direct `w.Write(...)` calls.
- Before (3 runs):
  - `BenchmarkCompress`: ~4.18-4.21 ms/op
  - `BenchmarkCompressParallel/workers=8`: ~39.3-39.8 ms/op
- After (3 runs):
  - `BenchmarkCompress`: ~4.25-4.29 ms/op
  - `BenchmarkCompressParallel/workers=8`: ~38.9-39.5 ms/op
- Result: mixed/neutral but `BenchmarkCompress` regressed.
- Decision: **discarded** (change reverted).

### 2026-02-07 - E006 - Use fixed `[5][]byte` instead of `[][]byte` for block stream buffers

- Hypothesis: remove one small slice-header allocation per block in decompression job setup.
- Change:
  - `decompressJob.compressed` changed to `[5][]byte`.
  - `produceDecompressJobs` and `readAndDecompressBlock` switched temporary stream buffers to arrays.
- Before (3 runs):
  - `BenchmarkCompress`: ~4.18-4.20 ms/op
  - `BenchmarkDecompress`: ~2.13-2.15 ms/op
- After (3 runs):
  - `BenchmarkCompress`: ~4.21-4.23 ms/op
  - `BenchmarkDecompress`: ~2.15-2.17 ms/op
- Result: slight regressions on primary benchmarks.
- Decision: **discarded** (change reverted).

### 2026-02-07 - E007 - Disable zstd decoder low-memory mode

- Hypothesis: `WithDecoderLowmem(false)` may improve decode throughput at the cost of memory.
- Change:
  - Added `zstd.WithDecoderLowmem(false)` to shared decoder options.
- Before (3 runs):
  - `BenchmarkCompress`: ~4.20-4.23 ms/op
  - `BenchmarkDecompress`: ~2.15-2.16 ms/op
- After (3 runs):
  - `BenchmarkCompress`: ~4.26-4.30 ms/op
  - `BenchmarkDecompress`: ~2.17-2.18 ms/op
- Result: throughput regressed on both paths.
- Decision: **discarded** (change reverted).

### 2026-02-07 - E008 - Single-block fast path for small inputs

- Hypothesis: if `firstBatch.Len() < BlockSize`, input fits in one block and parallel pipeline overhead is unnecessary.
- Change:
  - `Compress` now routes to `compressSingleWorkerWithBatch` when:
    - `opts.Workers == 1`, or
    - `firstBatch.Len() < int(opts.BlockSize)`.
- Before (3 runs):
  - `BenchmarkCompress`: ~4.16-4.25 ms/op, 149-156 allocs/op
  - `BenchmarkCompressParallel/workers=8`: ~38.6-39.0 ms/op
- After (3 runs):
  - `BenchmarkCompress`: ~4.04-4.06 ms/op, 63-66 allocs/op
  - `BenchmarkCompressParallel/workers=8`: ~38.9-39.2 ms/op
- Result: strong win for small/single-block compression, no meaningful regression in parallel block benchmark.
- Decision: **accepted**.

## Notes

- Existing uncommitted changes in `internal/compress/compress.go` were present before this session and should be evaluated separately with the same protocol.
