.PHONY: proto test run-inventory run-orders demo

proto:         ## regenerate gen/ from proto/ (needs buf + protoc-gen-go[-grpc] in PATH)
	buf generate

lint-proto:
	buf lint

test:
	go test -race ./...

run-inventory:
	go run ./cmd/inventory

run-orders:
	go run ./cmd/orders

demo:          ## drives both running servers
	go run ./cmd/client
