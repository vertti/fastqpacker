.PHONY: build build-pgo test lint bench benchmark clean

build:
	go build -o bin/fqpack ./cmd/fqpack
	go build -o bin/fqscramble ./cmd/fqscramble

build-pgo:
	go test -bench=BenchmarkCompressParallel/workers=8 -benchtime=5s -cpuprofile=cmd/fqpack/default.pgo ./internal/compress/
	go build -pgo=cmd/fqpack/default.pgo -o bin/fqpack ./cmd/fqpack
	go build -o bin/fqscramble ./cmd/fqscramble

test:
	go test -race -v ./...

lint:
	golangci-lint run

bench:
	go test -bench=. -benchmem ./...

benchmark: build
	@if [ ! -f testdata/benchmark.fq ]; then \
		if [ -f testdata/benchmark.fq.gz ]; then \
			echo "Decompressing testdata/benchmark.fq.gz..."; \
			gzip -dk testdata/benchmark.fq.gz; \
		else \
			echo "Error: testdata/benchmark.fq.gz not found"; \
			exit 1; \
		fi \
	fi
	PATH="$(CURDIR)/bin:$(PATH)" ./scripts/benchmark.sh testdata/benchmark.fq

clean:
	rm -rf bin/
