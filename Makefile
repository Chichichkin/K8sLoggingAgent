.PHONY: test test-unit test-integration coverage test-race test-unit-race

test:
	go test ./... -v

test-unit:
	go test ./internal/... -v -short

test-race:
	go test -race ./... -v

test-unit-race:
	go test -race ./internal/... -v -short
