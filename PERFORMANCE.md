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

### 2026-02-07 - E009 - Pool per-block compressed input buffers in parallel decompression

- Hypothesis: reuse the 5 compressed stream input buffers per block to reduce allocations and GC churn in parallel decompression.
- Change:
  - Added `compressedBlockBuffers` + `compressedBlockPool`.
  - Changed `decompressJob.compressed` from `[][]byte` to pooled struct.
  - Producer now resizes pooled streams and returns buffers on read/context errors.
  - Worker returns pooled buffers after decode.
- Before (3 runs):
  - `BenchmarkCompress`: ~3.99-4.07 ms/op, 63-67 allocs/op
  - `BenchmarkDecompress`: ~2.17-2.20 ms/op, 175 allocs/op
  - `BenchmarkCompressParallel/workers=8`: ~38.4-40.3 ms/op
- After (3 runs):
  - `BenchmarkCompress`: ~4.02-4.08 ms/op, 64-66 allocs/op
  - `BenchmarkDecompress`: ~2.17-2.18 ms/op, 173 allocs/op
  - `BenchmarkCompressParallel/workers=8`: ~39.6-39.7 ms/op
- Result: decompression allocations improved slightly, but throughput remained neutral and some key compression benches were slightly worse.
- Decision: **discarded** (change reverted).

### 2026-02-07 - E010 - Reuse `DecodeAll` destination buffers per decompression worker

- Hypothesis: avoid per-block allocations by reusing decode destination slices (`seq`, `qual`, `headers`, `nPos`, `lengths`) in each worker.
- Change:
  - Added worker-local decode scratch buffers.
  - `decompressJobToPooledBuffer` switched from `DecodeAll(src, nil)` to `DecodeAll(src, scratch[:0])`.
- Before (3 runs):
  - `BenchmarkCompress`: ~3.84-3.86 ms/op
  - `BenchmarkDecompress`: ~2.05-2.07 ms/op
  - `BenchmarkCompressParallel/workers=8`: ~37.1-38.2 ms/op
- After (3 runs):
  - `BenchmarkCompress`: ~4.05-4.11 ms/op
  - `BenchmarkDecompress`: ~2.26-2.28 ms/op
  - `BenchmarkCompressParallel/workers=8`: ~39.2-39.4 ms/op
- Result: consistent regression on core throughput metrics.
- Decision: **discarded** (change reverted).

### 2026-02-07 - E011 - Prefetch first two blocks in decompression and short-circuit one-block files

- Hypothesis: avoid parallel worker/channel/map overhead when compressed input contains only one block.
- Change:
  - `Decompress` now prefetches first block (and attempts second) using `readNextDecompressJob`.
  - If second block is EOF, decode the prefetched first block directly with `decompressSinglePrefetchedJob`.
  - If second block exists, continue parallel pipeline with prefetched jobs and resume streaming remaining blocks.
  - Added `readCompressedStreams` helper and reused it in block reading paths.
- Before (3 runs):
  - `BenchmarkCompress`: ~4.13-4.14 ms/op
  - `BenchmarkDecompress`: ~2.21-2.22 ms/op, 174-175 allocs/op
  - `BenchmarkCompressParallel/workers=8`: ~39.0-39.9 ms/op
- After (3 runs):
  - `BenchmarkCompress`: ~4.05 ms/op
  - `BenchmarkDecompress`: ~2.06-2.09 ms/op, 39-40 allocs/op
  - `BenchmarkCompressParallel/workers=8`: ~38.9-39.2 ms/op
- Result: strong decompression improvement and major allocation reduction with no meaningful regressions.
- Decision: **accepted**.

### 2026-02-07 - E012 - Replace collector pending maps with slice windows

- Hypothesis: reduce map overhead in ordered result collectors by using a compact slice window keyed by `seqNum-nextSeqNum`.
- Change:
  - Rewrote `collectAndWriteResults` and `collectAndWriteDecompressResults` to use slice windows and explicit order checks.
- Before (3 runs):
  - `BenchmarkCompress`: ~4.10-4.20 ms/op
  - `BenchmarkDecompress`: ~2.08-2.09 ms/op
  - `BenchmarkCompressParallel/workers=8`: ~39.2-40.1 ms/op
- After (3 runs):
  - `BenchmarkCompress`: ~4.10-4.14 ms/op
  - `BenchmarkDecompress`: ~2.10-2.14 ms/op
  - `BenchmarkCompressParallel/workers=8`: ~39.4-39.6 ms/op
- Result: mixed and effectively neutral; slight decompression regression and added code complexity.
- Decision: **discarded** (change reverted).

### 2026-02-07 - E013 - Use one contiguous backing allocation for compressed block streams

- Hypothesis: reduce per-block allocation count in stream reads by allocating one backing byte slice and carving 5 stream slices from it.
- Change:
  - `readCompressedStreams` switched from 5 independent `make([]byte, size)` calls to one `make([]byte, totalSize)` plus slicing.
- Before (3 runs):
  - `BenchmarkDecompress`: ~2.105-2.128 ms/op, 39-40 allocs/op
- After (3 runs, plus 5-run validation):
  - `BenchmarkDecompress`: ~2.110-2.129 ms/op (3-run), ~2.154-2.222 ms/op (5-run), 35-36 allocs/op
- Result: allocation count improved, but no consistent throughput gain (possible slight slowdown).
- Decision: **discarded** (change reverted).

### 2026-02-07 - E014 - Use separate `SpeedFastest` encoder for metadata streams

- Hypothesis: compress `headers`, `N positions`, and `lengths` with faster zstd level while keeping sequence/quality at default.
- Change:
  - Added per-worker second encoder (`SpeedFastest`) for metadata streams.
  - Updated compression block functions to accept distinct data/metadata encoders.
- Before (3 runs):
  - `BenchmarkCompress`: ~4.06-4.13 ms/op, 64-67 allocs/op
  - `BenchmarkCompressParallel/workers=8`: ~39.6-40.0 ms/op, 151-166 allocs/op
- After (3 runs):
  - `BenchmarkCompress`: ~4.15-4.22 ms/op, 96-99 allocs/op
  - `BenchmarkCompressParallel/workers=8`: ~40.1-40.6 ms/op, 245-255 allocs/op
- Result: clear compression regression and significant allocation increase.
- Decision: **discarded** (change reverted).

### 2026-02-07 - E015 - Rewrite N-position serialization with pre-grown writes

- Hypothesis: reduce per-record overhead in sequence metadata serialization by writing N-count and positions into one pre-grown chunk.
- Change:
  - Replaced repeated `binary.LittleEndian.AppendUint16` calls with `slices.Grow` + `PutUint16` writes in-place.
- Before (3 runs):
  - `BenchmarkCompress`: ~4.08-4.11 ms/op
  - `BenchmarkCompressBlock`: ~19.25-19.31 ms/op
  - `BenchmarkCompressParallel/workers=8`: ~38.5-39.7 ms/op
- After (3 runs):
  - `BenchmarkCompress`: ~4.12-4.17 ms/op
  - `BenchmarkCompressBlock`: ~19.56-19.78 ms/op
  - `BenchmarkCompressParallel/workers=8`: ~39.8-40.5 ms/op
- Result: consistent compression-side regression.
- Decision: **discarded** (change reverted).

### 2026-02-07 - E016 - Enable `zstd.WithLowerEncoderMem(true)`

- Hypothesis: lower zstd encoder memory mode may reduce memory pressure and improve throughput.
- Change:
  - Added `zstd.WithLowerEncoderMem(true)` to shared encoder options.
- Before (3 runs):
  - `BenchmarkCompress`: ~4.09-4.12 ms/op
  - `BenchmarkCompressParallel/workers=8`: ~39.0-39.9 ms/op
- After (3 runs):
  - `BenchmarkCompress`: ~3.97-4.03 ms/op
  - `BenchmarkCompressParallel/workers=8`: ~48.1-50.2 ms/op
- Result: severe regression for parallel compression despite minor single-block gain.
- Decision: **discarded** (change reverted).

### 2026-02-07 - E017 - Increase parallel job/result channel depth (`*2` -> `*4`)

- Hypothesis: deeper channels might reduce scheduler backpressure in parallel compression/decompression pipelines.
- Change:
  - `compressParallelWithBatch`: jobs/results channel capacities from `workers*2` to `workers*4`.
  - `decompressParallel`: jobs/results channel capacities from `workers*2` to `workers*4`.
- Before (3 runs):
  - `BenchmarkCompress`: ~4.07-4.10 ms/op
  - `BenchmarkDecompress`: ~2.07-2.10 ms/op
  - `BenchmarkCompressParallel/workers=8`: ~39.5-39.9 ms/op
- After (3 runs):
  - `BenchmarkCompress`: ~4.07-4.14 ms/op
  - `BenchmarkDecompress`: ~2.08-2.11 ms/op
  - `BenchmarkCompressParallel/workers=8`: ~39.3-40.4 ms/op
- Result: no clear throughput win; effectively neutral with run-to-run variance.
- Decision: **discarded** (change reverted).

## Notes

- Existing uncommitted changes in `internal/compress/compress.go` were present before this session and should be evaluated separately with the same protocol.
