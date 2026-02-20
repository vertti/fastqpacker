# FastQPacker Roadmap (Reset)

Date: 2026-02-09
Scope: Replace the previous milestone list with a PR-driven roadmap focused on correctness, robustness, and measurable performance.

## What FastQPacker Is

- A speed-first FASTQ compressor/decompressor for 4-line Illumina-style FASTQ.
- A block-based codec optimized for high throughput and good compression ratio.
- A CLI intended for pipeline use (stdin/stdout, file in/file out, worker control).

## What FastQPacker Is Not (Yet)

- Not full FASTQ dialect coverage (wrapped multi-line records are out of scope for now).
- Not yet hardened against malicious/corrupt containers with strict allocation guards.
- Not optimized yet for ultra-long reads and very large N-position vectors.

## Roadmap Principles

- Correctness before compression ratio experiments.
- Small PRs only: each PR should be independently releasable and verifiable.
- No speculative optimization merges: benchmark before/after required.
- Keep the speed advantage while removing silent data-risk paths.
- No backward compatibility with old container versions. The tool has no users yet — just improve the format in place. Files produced by older versions should be re-compressed.

## Required Verification For Every PR

Every PR in this roadmap must include all of:

1. Tests
- Run full test suite:
  - `GOCACHE=/tmp/fqpack-go-cache GOTMPDIR=/tmp mise x -- go test ./...`
- Add/adjust unit + integration tests for changed behavior.

2. Benchmarks (before and after, same machine, same commands)
- Compress/decompress core:
  - `GOCACHE=/tmp/fqpack-go-cache GOTMPDIR=/tmp mise x -- go test ./internal/compress -run '^$' -bench 'BenchmarkCompress$|BenchmarkDecompress$|BenchmarkCompressParallel/workers=8$' -benchmem -count=3`
- Parser:
  - `GOCACHE=/tmp/fqpack-go-cache GOTMPDIR=/tmp mise x -- go test ./internal/parser -run '^$' -bench 'Benchmark(ReadBatch|Parser)$' -benchmem -count=3`
- Encoder:
  - `GOCACHE=/tmp/fqpack-go-cache GOTMPDIR=/tmp mise x -- go test ./internal/encoder -run '^$' -bench 'Benchmark(AppendPackBases|AppendUnpackBases|DeltaEncode|DeltaDecode)$' -benchmem -count=3`

3. Performance journal update
- Add one entry to `PERFORMANCE.md`:
  - Hypothesis
  - Before numbers
  - After numbers
  - Decision (accepted/rejected)

4. Merge gate
- If key compress/decompress throughput regresses by more than 2% without clear product value, do not merge.
- Correctness/security fixes can accept performance cost, but the cost must be documented.

## PR Roadmap

## Phase 0: Correctness And Data Safety (Do First)

### PR-001: Preserve line-3 FASTQ payload ✅ DONE
- Value: Fix lossless round-trip for valid FASTQ where line 3 is `+<comment or id>`.
- Scope:
  - Extend container format to store plus-line payload.
  - Preserve payload in decompress output.
- Tests:
  - Round-trip tests with non-empty plus-line payload.
- Bench:
  - Full required benchmark suite before/after.

### PR-002: Fail fast on unsupported long-read N-position overflow
- Value: Eliminate silent corruption risk for sequences with N beyond current `uint16` tracking range.
- Scope:
  - Detect overflow condition and return explicit error on compress.
  - Improve error messaging with sequence length and record context.
- Tests:
  - Repro test for >65535 sequence length with trailing `N`.
  - Error-path tests for clear diagnostics.
- Bench:
  - Full required benchmark suite before/after (expect near-neutral).

### PR-003: Implement long-read-safe N-position encoding
- Value: Remove the overflow limitation introduced in PR-002 by adding safe encoding.
- Scope:
  - Replace `uint16`-bounded N position stream with long-read-safe representation.
- Tests:
  - Round-trip tests for long reads with many N positions.
  - Fuzz/property tests for random long sequences.
- Bench:
  - Full required benchmark suite before/after.

### PR-004: Strict header/version validation
- Value: Prevent decoding unknown/unsupported file versions silently.
- Scope:
  - Validate `FileHeader.Version`.
  - Validate reserved/unknown flags.
  - Return typed errors with actionable text.
- Tests:
  - Corrupt/unsupported header fixtures.
  - Compatibility tests for current version.
- Bench:
  - Full required benchmark suite before/after (expect neutral).

### PR-005: Block and stream size guards on decode
- Value: Prevent unbounded allocations from malformed container headers.
- Scope:
  - Add hard limits for per-stream and per-block allocations.
  - Validate consistency between metadata and decoded sizes.
- Tests:
  - Malformed block-header fixtures that should fail safely.
  - Regression tests for valid data.
- Bench:
  - Full required benchmark suite before/after.

### PR-006: Fuzzing harness for parser + container decode
- Value: Raise confidence and catch parser/container edge crashes early.
- Scope:
  - Add Go fuzz targets for parser line handling and block header/decode path.
  - Add CI fuzz-smoke job (time-bounded).
- Tests:
  - `go test` fuzz targets with smoke corpus.
- Bench:
  - Full required benchmark suite before/after (fuzz PR should be runtime-neutral).

## Phase 1: Interop And Operator UX

### PR-007: Transparent `.gz` input support in `fqpack` ✅ DONE
- Value: Remove common pipeline friction; most FASTQ arrives as gzip.
- Scope:
  - Auto-detect gzip by extension and/or magic bytes.
  - Stream decompress input before parsing.
- Tests:
  - `.fq.gz` round-trip integration tests.
  - Stdin + gz pipeline tests.
- Bench:
  - Full required benchmark suite before/after.
  - Add one end-to-end benchmark on gz input path.

### PR-008: `fqpack check` command
- Value: Fast integrity verification for archives without writing full output file.
- Scope:
  - New command to validate structure, decode streams, and report pass/fail.
  - Exit codes suitable for CI/pipelines.
- Tests:
  - Pass/fail fixtures including corrupt headers/blocks.
- Bench:
  - Full required benchmark suite before/after.
  - Add benchmark for `check` throughput.

### PR-009: `fqpack info` command
- Value: Quick introspection for ops and debugging.
- Scope:
  - Print version, flags, block count, estimated record count, stream sizes.
  - Optional JSON output mode for scripting.
- Tests:
  - Snapshot tests for text/JSON output.
- Bench:
  - Full required benchmark suite before/after (neutral expected).

### PR-010: Better malformed-input diagnostics
- Value: Faster debugging and support turnaround.
- Scope:
  - Include record index/block index in parser/decode errors where possible.
  - Standardize error prefixes.
- Tests:
  - Assertions on structured error text.
- Bench:
  - Full required benchmark suite before/after.

## Phase 2: Performance And Compression Controls

### PR-011: Compression presets (`speed`, `balanced`, `ratio`)
- Value: Let users trade speed vs ratio explicitly while preserving current default behavior.
- Scope:
  - New CLI flag and mapped zstd + block settings.
  - Document expected tradeoffs.
- Tests:
  - Option parsing tests and round-trip tests per preset.
- Bench:
  - Full required benchmark suite before/after.
  - Add preset comparison table in docs.

### PR-012: Auto block-size tuning heuristic
- Value: Better out-of-box performance on tiny and medium datasets.
- Scope:
  - Lightweight heuristic based on first batch/estimated input profile.
  - Keep deterministic and easy to disable.
- Tests:
  - Behavior tests for small/medium/large synthetic inputs.
- Bench:
  - Full required benchmark suite before/after.

### PR-013: Ordered collector micro-optimizations (guarded)
- Value: Reduce coordination overhead in parallel paths.
- Scope:
  - Evaluate alternatives to map-based pending collectors only if data supports it.
  - Keep code complexity bounded; revert if neutral/regression.
- Tests:
  - Existing parallel ordering tests + stress tests.
- Bench:
  - Full required benchmark suite before/after.

### PR-014: Optional dictionary-training workflow
- Value: Potential ratio wins for repeated/similar datasets.
- Scope:
  - Add offline training tool/script and optional use at compress time.
  - Keep default path dictionary-free.
- Tests:
  - Dictionary load/apply tests and compatibility tests.
- Bench:
  - Full required benchmark suite before/after.
  - Include ratio-focused dataset benchmarks.

## Phase 3: Advanced Features (Only After Phases 0-2)

### PR-015: Paired-end joint compression (experimental)
- Value: Potential 5-10% ratio improvement on R1/R2 paired datasets.
- Scope:
  - Experimental mode behind explicit flag.
  - Do not change default single-file behavior.
- Tests:
  - Paired dataset round-trip + compatibility tests.
- Bench:
  - Full required benchmark suite before/after.
  - Paired-end-specific ratio/speed benchmarks.

### PR-016: Streaming-friendly decompression mode
- Value: Lower peak memory for large archives and pipeline use.
- Scope:
  - Reduce full-block buffering where feasible.
  - Preserve output ordering guarantees.
- Tests:
  - Large multi-block integration tests and memory-usage checks.
- Bench:
  - Full required benchmark suite before/after.
  - Add memory profile comparison.

## Release Strategy

- v0.8.x target: PR-001 through PR-006 (correctness + hardening).
- v0.9.x target: PR-007 through PR-010 (interop + UX).
- v1.0 target: PR-011 through PR-014 (stable controls + measured perf work).
- Post-v1.0 experimental track: PR-015 and PR-016.

## Definition Of Done For This Roadmap File

- This roadmap replaces the old milestone checklist.
- Work should now be planned and tracked by PR IDs above.
- Any new roadmap item must include explicit test + benchmark gates.
