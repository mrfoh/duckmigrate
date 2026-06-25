BINARY := duckmigrate
PKG := ./cmd/duckmigrate
export CGO_ENABLED := 1

.PHONY: build install test vet lint tidy example clean

build:
	go build -o bin/$(BINARY) $(PKG)

install:
	go install $(PKG)

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run

tidy:
	go mod tidy

example:
	go run ./examples/embed

clean:
	rm -rf bin
