.PHONY: all check build test format check-format lint fix clean

# --- Full local workflow ---
all: format fix test build

# --- CI-friendly checks (no mutation) ---
check: check-format lint test build

# --- Build ---
build:
	@go build ./...

# --- Test ---
test:
	@go test ./...

coverage:
	@go test -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out

coverage-html: coverage
	@go tool cover -html=coverage.out -o coverage.html

# --- Format ---
format:
	@gofmt -w .

check-format:
	@test -z "$$(gofmt -l .)" || (echo "gofmt would change:"; gofmt -l .; exit 1)

# --- Lint ---
lint:
	@go vet ./...

# --- Fix (depends on format to avoid concurrent writes) ---
fix: format

# --- Clean ---
clean:
	@rm -f coverage.out coverage.html
