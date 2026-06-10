.PHONY: all ui build test vet clean run

BIN := pglockr

all: build

## ui: build the embedded React UI into web/dist
ui:
	cd web && npm install && npm run build

## build: build the UI then the single binary with embedded assets
build: ui
	go build -o $(BIN) ./cmd/pglockr

## build-go: build the binary only (assumes web/dist already built)
build-go:
	go build -o $(BIN) ./cmd/pglockr

test:
	go test ./...

vet:
	go vet ./...

run: build-go
	./$(BIN)

clean:
	rm -f $(BIN)
	rm -rf web/dist web/node_modules
