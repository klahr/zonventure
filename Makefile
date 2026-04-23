.PHONY: run seed test test-race vet build tidy clean

BIN := zv-server
DB  := zv.db
ADMIN    ?= admin:admin
ARRANGER ?= boss:s3cret

run:
	go run ./cmd/server

seed:
	go run ./cmd/server -db $(DB) -seed-admin $(ADMIN) -seed-arranger $(ARRANGER) -seed-demo-adventure

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
