#!/usr/bin/env bash
#
# Benchmark fqpack against tools from FQSqueezer paper (Table 1)
# Dataset: ERR532393_1 (9.0GB FASTQ)
# Paper: https://www.nature.com/articles/s41598-020-57452-6
#
# Usage: ./scripts/benchmark-paper-comparison.sh [data_dir]
#

set -euo pipefail

DATA_DIR="${1:-benchmark_data}"
INPUT="$DATA_DIR/ERR532393_1.fastq"
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

# Check input file
if [[ ! -f "$INPUT" ]]; then
    echo "Error: Dataset not found at $INPUT"
    echo "Run: ./scripts/download-benchmark-data.sh"
    exit 1
fi

INPUT_SIZE=$(stat -f%z "$INPUT" 2>/dev/null || stat -c%s "$INPUT" 2>/dev/null)
INPUT_SIZE_GB=$(echo "scale=1; $INPUT_SIZE / 1024 / 1024 / 1024" | bc)

echo "=== FQSqueezer Paper Comparison Benchmark ==="
echo "Dataset: ERR532393_1 (${INPUT_SIZE_GB}GB)"
echo "Reference: https://www.nature.com/articles/s41598-020-57452-6/tables/1"
echo ""

# Build fqpack if needed
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
FQPACK="$SCRIPT_DIR/../bin/fqpack"
if [[ ! -x "$FQPACK" ]]; then
    echo "Building fqpack..."
    (cd "$SCRIPT_DIR/.." && make build)
fi

# Results array
declare -a RESULTS

# Helper to format size in MB
format_mb() {
    echo "scale=0; $1 / 1024 / 1024" | bc
}

# --- fqpack ---
echo "Testing fqpack..."
COMP_START=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
"$FQPACK" -i "$INPUT" -o "$TMPDIR/test.fqz"
COMP_END=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
COMP_TIME=$(echo "scale=0; ($COMP_END - $COMP_START)" | bc)
COMP_SIZE=$(stat -f%z "$TMPDIR/test.fqz" 2>/dev/null || stat -c%s "$TMPDIR/test.fqz" 2>/dev/null)

DECOMP_START=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
"$FQPACK" -d -i "$TMPDIR/test.fqz" -o "$TMPDIR/test_fqpack.fq"
DECOMP_END=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
DECOMP_TIME=$(echo "scale=0; ($DECOMP_END - $DECOMP_START)" | bc)

# Verify round-trip
if ! diff -q "$INPUT" "$TMPDIR/test_fqpack.fq" >/dev/null 2>&1; then
    echo "ERROR: fqpack round-trip failed!"
    exit 1
fi
rm "$TMPDIR/test_fqpack.fq"

RESULTS+=("fqpack,$(format_mb $COMP_SIZE),$COMP_TIME,$DECOMP_TIME")
echo "  Size: $(format_mb $COMP_SIZE) MB, Compress: ${COMP_TIME}s, Decompress: ${DECOMP_TIME}s"

# --- pigz ---
if command -v pigz &>/dev/null; then
    echo "Testing pigz..."
    COMP_START=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
    pigz -c "$INPUT" > "$TMPDIR/test.gz"
    COMP_END=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
    COMP_TIME=$(echo "scale=0; ($COMP_END - $COMP_START)" | bc)
    COMP_SIZE=$(stat -f%z "$TMPDIR/test.gz" 2>/dev/null || stat -c%s "$TMPDIR/test.gz" 2>/dev/null)

    DECOMP_START=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
    pigz -dc "$TMPDIR/test.gz" > /dev/null
    DECOMP_END=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
    DECOMP_TIME=$(echo "scale=0; ($DECOMP_END - $DECOMP_START)" | bc)

    RESULTS+=("pigz,$(format_mb $COMP_SIZE),$COMP_TIME,$DECOMP_TIME")
    echo "  Size: $(format_mb $COMP_SIZE) MB, Compress: ${COMP_TIME}s, Decompress: ${DECOMP_TIME}s"
    rm "$TMPDIR/test.gz"
else
    echo "pigz not found, skipping (brew install pigz)"
fi

# --- zstd ---
if command -v zstd &>/dev/null; then
    echo "Testing zstd..."
    COMP_START=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
    zstd -q -c "$INPUT" > "$TMPDIR/test.zst"
    COMP_END=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
    COMP_TIME=$(echo "scale=0; ($COMP_END - $COMP_START)" | bc)
    COMP_SIZE=$(stat -f%z "$TMPDIR/test.zst" 2>/dev/null || stat -c%s "$TMPDIR/test.zst" 2>/dev/null)

    DECOMP_START=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
    zstd -dc "$TMPDIR/test.zst" > /dev/null
    DECOMP_END=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
    DECOMP_TIME=$(echo "scale=0; ($DECOMP_END - $DECOMP_START)" | bc)

    RESULTS+=("zstd,$(format_mb $COMP_SIZE),$COMP_TIME,$DECOMP_TIME")
    echo "  Size: $(format_mb $COMP_SIZE) MB, Compress: ${COMP_TIME}s, Decompress: ${DECOMP_TIME}s"
    rm "$TMPDIR/test.zst"
else
    echo "zstd not found, skipping (brew install zstd)"
fi

# --- 7z ---
if command -v 7z &>/dev/null; then
    echo "Testing 7z..."
    COMP_START=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
    7z a -mx=9 -bso0 -bsp0 "$TMPDIR/test.7z" "$INPUT"
    COMP_END=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
    COMP_TIME=$(echo "scale=0; ($COMP_END - $COMP_START)" | bc)
    COMP_SIZE=$(stat -f%z "$TMPDIR/test.7z" 2>/dev/null || stat -c%s "$TMPDIR/test.7z" 2>/dev/null)

    DECOMP_START=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
    7z x -bso0 -bsp0 -o"$TMPDIR/7z_out" "$TMPDIR/test.7z"
    DECOMP_END=$(perl -MTime::HiRes=time -e 'printf "%.3f", time')
    DECOMP_TIME=$(echo "scale=0; ($DECOMP_END - $DECOMP_START)" | bc)

    RESULTS+=("7z,$(format_mb $COMP_SIZE),$COMP_TIME,$DECOMP_TIME")
    echo "  Size: $(format_mb $COMP_SIZE) MB, Compress: ${COMP_TIME}s, Decompress: ${DECOMP_TIME}s"
    rm -rf "$TMPDIR/test.7z" "$TMPDIR/7z_out"
else
    echo "7z not found, skipping (brew install p7zip)"
fi

# --- Print results table ---
echo ""
echo "=== Results ==="
echo ""
printf "| %-10s | %12s | %12s | %14s |\n" "Tool" "Size [MB]" "Compress [s]" "Decompress [s]"
printf "|%s|%s|%s|%s|\n" "------------" "--------------" "--------------" "----------------"

for result in "${RESULTS[@]}"; do
    IFS=',' read -r tool size comp decomp <<< "$result"
    printf "| %-10s | %12s | %12s | %14s |\n" "$tool" "$size" "$comp" "$decomp"
done

echo ""
echo "=== FQSqueezer Paper Reference (Table 1) ==="
echo "Note: Paper used different hardware. Compare ratios, not absolute times."
echo ""
printf "| %-10s | %12s | %12s | %14s |\n" "Tool" "Size [MB]" "Compress [s]" "Decompress [s]"
printf "|%s|%s|%s|%s|\n" "------------" "--------------" "--------------" "----------------"
printf "| %-10s | %12s | %12s | %14s |\n" "pigz" "3,392" "128" "54"
printf "| %-10s | %12s | %12s | %14s |\n" "7z" "2,710" "2,438" "220"
printf "| %-10s | %12s | %12s | %14s |\n" "zstd" "3,335" "828" "35"
printf "| %-10s | %12s | %12s | %14s |\n" "DSRC 2" "2,273" "55" "56"
printf "| %-10s | %12s | %12s | %14s |\n" "FQZcomp" "1,990" "287" "385"
printf "| %-10s | %12s | %12s | %14s |\n" "Spring" "1,650" "159" "24"
printf "| %-10s | %12s | %12s | %14s |\n" "FQSqueezer" "1,511" "1,409" "1,501"
echo ""
