.PHONY: test test-unit test-e2e test-stress test-coverage chat-example clean help docs docs-install

# Default target
help:
	@echo "Available commands:"
	@echo "  make test          - Run all tests"
	@echo "  make test-unit     - Run unit tests only"
	@echo "  make test-e2e      - Run end-to-end tests only"
	@echo "  make test-stress   - Run stress tests (requires high ulimit)"
	@echo "  make test-coverage - Run tests with coverage report"
	@echo "  make chat-example  - Run the JS chat example over wss:// (generates a dev cert)"
	@echo "  make docs          - Serve the documentation site locally (live reload)"
	@echo "  make clean         - Clean test cache and coverage files"

# Run all tests
test:
	go test ./tests/...

# Run unit tests only
test-unit:
	go test ./tests/unit/...

# Run e2e tests only
test-e2e:
	go test ./tests/e2e/...

# Run stress tests
test-stress:
	@echo "==> Running stress tests (this may take a while)..."
	@echo "==> Note: You may need to run 'ulimit -n 65536' first"
	cd tests/stress && go test -v -timeout 30m

# Run tests with coverage
test-coverage:
	@echo "==> Running tests with coverage..."
	go test ./tests/... -cover -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "==> Coverage report generated: coverage.html"

# Clean test cache and coverage files
clean:
	@echo "==> Cleaning test cache and coverage files..."
	go clean -testcache
	rm -f coverage.out coverage.html

fmt:
	go fmt ./...

lint:
	golangci-lint run ./...

chat:
	cd clients/js && npm install && npm run build
	cd examples/chat && go run .

# Install docs dependencies (mkdocs-material) into a local venv
docs-install:
	test -d .venv || python3 -m venv .venv
	.venv/bin/pip install -q -r docs-site/requirements.txt

# Serve the documentation site locally with live reload
docs: docs-install
	.venv/bin/mkdocs serve -a 127.0.0.1:8037
