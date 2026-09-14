.PHONY: test

.PHONY: test test-unit test-e2e test-stress test-docker-stress test-docker-e2e test-coverage chat-example clean help docs docs-install

# Default target
help:
	@echo "Available commands:"
	@echo "  make test          - Run all tests"
	@echo "  make test-unit     - Run unit tests only"
	@echo "  make test-e2e      - Run end-to-end tests only"
	@echo "  make test-stress   - Run stress tests (requires high ulimit)"
	@echo "  make test-docker-stress - Run stress tests against the Dockerized server"
	@echo "  make test-docker-e2e    - Run full Docker e2e (up, test, down)"
	@echo "  make test-coverage - Run tests with coverage report"
	@echo "  make chat-example  - Run the JS chat example over wss:// (generates a dev cert)"
	@echo "  make docs          - Serve the documentation site locally (live reload)"
	@echo "  make clean         - Clean test cache and coverage files"

# Run all tests
test:
	@echo "==> Running all tests..."
	go test ./tests/... -v

# Run unit tests
test-unit:
	@echo "==> Running unit tests..."
	go test ./tests/unit/... -v

# Run end-to-end tests
test-e2e:
	@echo "==> Running end-to-end tests..."
	go test ./tests/e2e/... -v

# Run stress tests
test-stress:
	@echo "==> Running stress tests (this may take a while)..."
	@echo "==> Note: You may need to run 'ulimit -n 65536' first"
	cd tests/stress && go test -v -timeout 30m

# Run stress tests against the server running in Docker (requires: docker compose up -d --build)
test-docker-stress:
	@echo "==> Running Docker stress tests..."
	cd tests/stress && go test -tags docker -run TestDockerStress -timeout 5m -v ./...

# Full Docker e2e: up, wait healthy, round-trip, down
test-docker-e2e:
	@echo "==> Running Docker e2e..."
	./tests/docker-e2e.sh

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
	cd examples/chat && go run .

# Install docs dependencies (mkdocs-material) into a local venv
docs-install:
	test -d .venv || python3 -m venv .venv
	.venv/bin/pip install -q -r docs-site/requirements.txt

# Serve the documentation site locally with live reload
docs: docs-install
	.venv/bin/mkdocs serve
