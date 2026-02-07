#!/usr/bin/env bash
#
# Benchmark fqpack only on the canonical 9GB FASTQ benchmark dataset.
#
# Usage:
#   ./scripts/benchmark_fqpack_9gb.sh [iterations]
#

set -euo pipefail

ITERATIONS="${1:-3}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
INPUT_FILE="$REPO_ROOT/benchmark_data/ERR532393_1.fastq"
FQPACK="$REPO_ROOT/bin/fqpack"

if [[ ! "$ITERATIONS" =~ ^[0-9]+$ ]] || [[ "$ITERATIONS" -lt 1 ]]; then
	echo "Error: iterations must be a positive integer"
	exit 1
fi

if [[ ! -f "$INPUT_FILE" ]]; then
	echo "Error: benchmark input not found: $INPUT_FILE"
	exit 1
fi

if [[ ! -x "$FQPACK" ]]; then
	echo "Building fqpack..."
	(
		cd "$REPO_ROOT"
		PATH=/Users/vertti/.local/share/mise/installs/go/1.25.7/bin:$PATH \
			GOCACHE=/tmp/fqpack-go-cache \
			GOTMPDIR=/tmp \
			make build
	)
fi

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

OUT_FILE="$TMPDIR/test.fqz"
DECOMP_FILE="$TMPDIR/test.fq"

input_size="$(stat -f%z "$INPUT_FILE" 2>/dev/null || stat -c%s "$INPUT_FILE")"

measure_ms() {
	local cmd="$1"
	local start end
	start="$(perl -MTime::HiRes=time -e 'printf "%.6f", time')"
	eval "$cmd" >/dev/null 2>&1
	end="$(perl -MTime::HiRes=time -e 'printf "%.6f", time')"
	echo "scale=3; ($end - $start) * 1000" | bc
}

average_ms() {
	local cmd="$1"
	local total=0
	local run_ms
	for ((i = 1; i <= ITERATIONS; i++)); do
		run_ms="$(measure_ms "$cmd")"
		total="$(echo "scale=6; $total + $run_ms" | bc)"
	done
	echo "scale=3; $total / $ITERATIONS" | bc
}

echo "=== fqpack 9GB benchmark ==="
echo "Input: $INPUT_FILE"
echo "Input bytes: $input_size"
echo "Iterations: $ITERATIONS"

# One full verified run for compressed size + correctness.
"$FQPACK" -i "$INPUT_FILE" -o "$OUT_FILE"
compressed_size="$(stat -f%z "$OUT_FILE" 2>/dev/null || stat -c%s "$OUT_FILE")"
"$FQPACK" -d -i "$OUT_FILE" -o "$DECOMP_FILE"
if ! cmp -s "$INPUT_FILE" "$DECOMP_FILE"; then
	echo "Error: round-trip verification failed"
	exit 1
fi

# Timed runs.
compress_ms="$(average_ms "\"$FQPACK\" -i \"$INPUT_FILE\" -o \"$OUT_FILE\"")"
decompress_ms="$(average_ms "\"$FQPACK\" -d -i \"$OUT_FILE\" -o \"$DECOMP_FILE\"")"

ratio="$(echo "scale=2; $input_size / $compressed_size" | bc)"
speed_mb_s="$(echo "scale=1; $input_size / ($compress_ms * 1000)" | bc)"

size_mb="$(echo "scale=0; $compressed_size / 1000000" | bc)"
compress_s="$(echo "scale=2; $compress_ms / 1000" | bc)"
decompress_s="$(echo "scale=2; $decompress_ms / 1000" | bc)"

echo ""
echo "Compressed bytes: $compressed_size"
echo "Ratio: ${ratio}x"
echo "Compress: ${compress_ms} ms"
echo "Decompress: ${decompress_ms} ms"
echo "Speed: ${speed_mb_s} MB/s"
echo ""
echo "README row:"
echo "| **fqpack** | **${size_mb} MB** | **${ratio}x** | **${compress_s}s** | **${decompress_s}s** | **${speed_mb_s} MB/s** |"
