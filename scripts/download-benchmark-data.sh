#!/usr/bin/env bash
#
# Download ERR532393_1 dataset used in FQSqueezer paper benchmarks
# Source: https://www.nature.com/articles/s41598-020-57452-6
#
# Usage: ./scripts/download-benchmark-data.sh [output_dir]
#

set -euo pipefail

OUTPUT_DIR="${1:-benchmark_data}"
URL="ftp://ftp.sra.ebi.ac.uk/vol1/fastq/ERR532/ERR532393/ERR532393_1.fastq.gz"
FILENAME="ERR532393_1.fastq"

mkdir -p "$OUTPUT_DIR"

if [[ -f "$OUTPUT_DIR/$FILENAME" ]]; then
    echo "Dataset already exists: $OUTPUT_DIR/$FILENAME"
    ls -lh "$OUTPUT_DIR/$FILENAME"
    exit 0
fi

echo "Downloading ERR532393_1 dataset (~3.2GB compressed, ~9GB uncompressed)..."
echo "This may take a few minutes depending on your connection."
echo ""

curl -o "$OUTPUT_DIR/${FILENAME}.gz" "$URL"

echo ""
echo "Decompressing..."
gunzip -f "$OUTPUT_DIR/${FILENAME}.gz"

echo ""
echo "Done!"
ls -lh "$OUTPUT_DIR/$FILENAME"
