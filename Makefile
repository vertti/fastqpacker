.PHONY: build test lint bench clean

build:
	go build -o bin/fqpack ./cmd/fqpack

test:
	go test -race -v ./...

lint:
	golangci-lint run

bench:
	go test -bench=. -benchmem ./...

clean:
	rm -rf bin/
