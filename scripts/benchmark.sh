#!/usr/bin/env bash
#
# Benchmark fqpack against gzip and repaq
#
# Usage: ./scripts/benchmark.sh <input.fq>
#

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Check arguments
if [[ $# -lt 1 ]]; then
    echo "Usage: $0 <input.fq> [iterations]"
    echo "  input.fq   - FASTQ file to benchmark"
    echo "  iterations - number of runs for timing (default: 3)"
    exit 1
fi

INPUT_FILE="$1"
ITERATIONS="${2:-3}"
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

# Verify input file exists
if [[ ! -f "$INPUT_FILE" ]]; then
    echo "Error: Input file '$INPUT_FILE' not found"
    exit 1
fi

# Get absolute path and size
INPUT_FILE=$(cd "$(dirname "$INPUT_FILE")" && pwd)/$(basename "$INPUT_FILE")
INPUT_SIZE=$(stat -f%z "$INPUT_FILE" 2>/dev/null || stat -c%s "$INPUT_FILE" 2>/dev/null)

echo -e "${BLUE}=== FASTQ Compression Benchmark ===${NC}"
echo ""
echo "Input file: $INPUT_FILE"
echo "Input size: $(numfmt --to=iec-i --suffix=B $INPUT_SIZE 2>/dev/null || echo "$INPUT_SIZE bytes")"
echo "Iterations: $ITERATIONS"
echo ""

# Build fqpack if needed
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
FQPACK="$SCRIPT_DIR/../bin/fqpack"
if [[ ! -x "$FQPACK" ]]; then
    echo -e "${YELLOW}Building fqpack...${NC}"
    (cd "$SCRIPT_DIR/.." && make build)
fi
FQPACK="$(cd "$(dirname "$FQPACK")" && pwd)/$(basename "$FQPACK")"

# Function to measure time (returns milliseconds)
measure_time() {
    local cmd="$1"
    local start end
    start=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
    eval "$cmd" >/dev/null 2>&1
    end=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
    echo "scale=3; ($end - $start) * 1000" | bc
}

# Function to run benchmark and get average time
benchmark() {
    local cmd="$1"
    local total=0
    local time_ms
    for ((i=1; i<=ITERATIONS; i++)); do
        time_ms=$(measure_time "$cmd")
        total=$(echo "scale=3; $total + $time_ms" | bc)
    done
    echo "scale=1; $total / $ITERATIONS" | bc
}

# Function to format ratio
format_ratio() {
    local original=$1
    local compressed=$2
    echo "scale=2; $original / $compressed" | bc
}

# Function to format speed (MB/s)
format_speed() {
    local size_bytes=$1
    local time_ms=$2
    echo "scale=1; $size_bytes / ($time_ms * 1000)" | bc
}

# Results file
RESULTS_FILE="$TMPDIR/results.txt"
echo "tool,size,compress_ms,decompress_ms" > "$RESULTS_FILE"

# --- fqpack ---
echo -e "${BLUE}--- Testing fqpack ---${NC}"
FQPACK_OUT="$TMPDIR/test.fqz"
FQPACK_DECOMP="$TMPDIR/test_fqpack.fq"

fqpack_compress_time=$(benchmark "\"$FQPACK\" -i \"$INPUT_FILE\" -o \"$FQPACK_OUT\"")
"$FQPACK" -i "$INPUT_FILE" -o "$FQPACK_OUT"
fqpack_size=$(stat -f%z "$FQPACK_OUT" 2>/dev/null || stat -c%s "$FQPACK_OUT" 2>/dev/null)

fqpack_decompress_time=$(benchmark "\"$FQPACK\" -d -i \"$FQPACK_OUT\" -o \"$FQPACK_DECOMP\"")

# Verify round-trip
if ! diff -q "$INPUT_FILE" "$FQPACK_DECOMP" >/dev/null 2>&1; then
    echo -e "${RED}ERROR: fqpack round-trip verification failed!${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Round-trip verified${NC}"
echo "fqpack,$fqpack_size,$fqpack_compress_time,$fqpack_decompress_time" >> "$RESULTS_FILE"

# --- gzip ---
echo -e "${BLUE}--- Testing gzip ---${NC}"
GZIP_OUT="$TMPDIR/test.fq.gz"
GZIP_DECOMP="$TMPDIR/test_gzip.fq"

gzip_compress_time=$(benchmark "gzip -c \"$INPUT_FILE\" > \"$GZIP_OUT\"")
gzip -c "$INPUT_FILE" > "$GZIP_OUT"
gzip_size=$(stat -f%z "$GZIP_OUT" 2>/dev/null || stat -c%s "$GZIP_OUT" 2>/dev/null)

gzip_decompress_time=$(benchmark "gzip -dc \"$GZIP_OUT\" > \"$GZIP_DECOMP\"")
echo -e "${GREEN}✓ gzip completed${NC}"
echo "gzip,$gzip_size,$gzip_compress_time,$gzip_decompress_time" >> "$RESULTS_FILE"

# --- pigz (if available) ---
if command -v pigz &>/dev/null; then
    echo -e "${BLUE}--- Testing pigz ---${NC}"
    PIGZ_OUT="$TMPDIR/test_pigz.fq.gz"
    PIGZ_DECOMP="$TMPDIR/test_pigz.fq"

    pigz_compress_time=$(benchmark "pigz -c \"$INPUT_FILE\" > \"$PIGZ_OUT\"")
    pigz -c "$INPUT_FILE" > "$PIGZ_OUT"
    pigz_size=$(stat -f%z "$PIGZ_OUT" 2>/dev/null || stat -c%s "$PIGZ_OUT" 2>/dev/null)

    pigz_decompress_time=$(benchmark "pigz -dc \"$PIGZ_OUT\" > \"$PIGZ_DECOMP\"")
    echo -e "${GREEN}✓ pigz completed${NC}"
    echo "pigz,$pigz_size,$pigz_compress_time,$pigz_decompress_time" >> "$RESULTS_FILE"
else
    echo -e "${YELLOW}pigz not found, skipping${NC}"
fi

# --- repaq (if available) ---
if command -v repaq &>/dev/null; then
    # Get thread count (same as fqpack uses by default)
    THREADS=$(sysctl -n hw.ncpu 2>/dev/null || nproc 2>/dev/null || echo 4)

    echo -e "${BLUE}--- Testing repaq (${THREADS} threads) ---${NC}"
    REPAQ_OUT="$TMPDIR/test.rfq"
    REPAQ_DECOMP="$TMPDIR/test_repaq.fq"

    repaq_compress_time=$(benchmark "repaq -c -i \"$INPUT_FILE\" -o \"$REPAQ_OUT\" -t $THREADS")
    repaq -c -i "$INPUT_FILE" -o "$REPAQ_OUT" -t "$THREADS"
    repaq_size=$(stat -f%z "$REPAQ_OUT" 2>/dev/null || stat -c%s "$REPAQ_OUT" 2>/dev/null)

    repaq_decompress_time=$(benchmark "repaq -d -i \"$REPAQ_OUT\" -o \"$REPAQ_DECOMP\" -t $THREADS")
    echo -e "${GREEN}✓ repaq completed${NC}"
    echo "repaq,$repaq_size,$repaq_compress_time,$repaq_decompress_time" >> "$RESULTS_FILE"

    # --- repaq + xz (best compression mode) ---
    echo -e "${BLUE}--- Testing repaq+xz (${THREADS} threads) ---${NC}"
    REPAQ_XZ_OUT="$TMPDIR/test.rfq.xz"
    REPAQ_XZ_DECOMP="$TMPDIR/test_repaq_xz.fq"

    # Compress: repaq then xz (xz -T for threads)
    repaq_xz_compress_time=$(benchmark "repaq -c -i \"$INPUT_FILE\" -o \"$TMPDIR/test_xz.rfq\" -t $THREADS && xz -9 -T $THREADS -c \"$TMPDIR/test_xz.rfq\" > \"$REPAQ_XZ_OUT\"")
    repaq -c -i "$INPUT_FILE" -o "$TMPDIR/test_xz.rfq" -t "$THREADS" && xz -9 -T "$THREADS" -c "$TMPDIR/test_xz.rfq" > "$REPAQ_XZ_OUT"
    repaq_xz_size=$(stat -f%z "$REPAQ_XZ_OUT" 2>/dev/null || stat -c%s "$REPAQ_XZ_OUT" 2>/dev/null)

    # Decompress: xz then repaq
    repaq_xz_decompress_time=$(benchmark "xz -dc -T $THREADS \"$REPAQ_XZ_OUT\" > \"$TMPDIR/test_xz_dec.rfq\" && repaq -d -i \"$TMPDIR/test_xz_dec.rfq\" -o \"$REPAQ_XZ_DECOMP\" -t $THREADS")
    echo -e "${GREEN}✓ repaq+xz completed${NC}"
    echo "repaq+xz,$repaq_xz_size,$repaq_xz_compress_time,$repaq_xz_decompress_time" >> "$RESULTS_FILE"
else
    echo -e "${YELLOW}repaq not found, skipping${NC}"
    echo -e "${YELLOW}Build from source: https://github.com/OpenGene/repaq${NC}"
fi

# --- Print results table ---
echo ""
echo -e "${BLUE}=== Results ===${NC}"
echo ""
printf "%-10s %12s %10s %14s %14s %12s\n" \
    "Tool" "Size" "Ratio" "Compress(ms)" "Decompress(ms)" "Speed(MB/s)"
printf "%-10s %12s %10s %14s %14s %12s\n" \
    "----" "----" "-----" "------------" "--------------" "-----------"

# Read results and print
while IFS=, read -r tool size compress_ms decompress_ms; do
    [[ "$tool" == "tool" ]] && continue  # Skip header

    ratio=$(format_ratio "$INPUT_SIZE" "$size")
    speed=$(format_speed "$INPUT_SIZE" "$compress_ms")

    # Format size nicely
    size_fmt=$(numfmt --to=iec-i --suffix=B "$size" 2>/dev/null || echo "${size}B")

    printf "%-10s %12s %9sx %14s %14s %12s\n" \
        "$tool" "$size_fmt" "$ratio" "$compress_ms" "$decompress_ms" "$speed"
done < "$RESULTS_FILE"

# --- Summary ---
echo ""
echo -e "${BLUE}=== Summary ===${NC}"
echo ""

# Calculate improvements vs gzip
size_improvement=$(echo "scale=2; $gzip_size / $fqpack_size" | bc)
echo "fqpack vs gzip compression: ${size_improvement}x smaller"

if (( $(echo "$fqpack_compress_time < $gzip_compress_time" | bc -l) )); then
    speed_improvement=$(echo "scale=2; $gzip_compress_time / $fqpack_compress_time" | bc)
    echo "fqpack vs gzip speed: ${speed_improvement}x faster"
else
    speed_ratio=$(echo "scale=2; $fqpack_compress_time / $gzip_compress_time" | bc)
    echo "fqpack vs gzip speed: ${speed_ratio}x slower"
fi

echo ""
