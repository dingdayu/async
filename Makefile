.PHONY: help test lint bench examples release-snapshot

GO ?= go

# Default target
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  test              Run all tests"
	@echo "  lint              Run golangci-lint"
	@echo "  bench             Run runtime benchmarks"
	@echo "  examples          Run all example programs"
	@echo "  release-snapshot  Run GoReleaser in snapshot mode"

test:
	$(GO) test ./...

lint:
	golangci-lint run

bench:
	$(GO) test ./... -run '^$$' -bench 'BenchmarkRuntime' -benchmem

examples:
	@echo "Running handle example..."
	$(GO) run ./examples/handle
	@echo "Running task example..."
	$(GO) run ./examples/task
	@echo "Running default example..."
	$(GO) run ./examples/default

release-snapshot:
	goreleaser release --snapshot --clean
