BINARY := bin/telehentai
PKG := ./cmd/telehentai

.PHONY: build test vet fmt run tidy

build:
	CGO_ENABLED=0 go build -o $(BINARY) $(PKG)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "以下文件未格式化："; echo "$$out"; exit 1; fi

run:
	go run $(PKG) run

tidy:
	go mod tidy
