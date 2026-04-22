.PHONY: run seed test test-race vet build tidy clean

BIN := zv-server
DB  := zv.db
ARRANGER ?= boss:s3cret

run:
	go run ./cmd/server

seed:
	go run ./cmd/server -seed-arranger $(ARRANGER)

build:
	go build -o $(BIN) ./cmd/server

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -f $(BIN) $(DB)
