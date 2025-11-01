.PHONY: build test demo clean docker docker-postgres

# Build the agent binary
build:
	@echo "🔨 Building DiffKeeper agent..."
	go build -ldflags="-w -s" -o bin/diffkeeper main.go
	@echo "✅ Built: bin/diffkeeper"

# Run tests
test:
	@echo "🧪 Running tests..."
	go test -v -race -coverprofile=coverage.out ./...
	@echo "✅ Tests complete"

# Build Postgres demo image
docker-postgres:
	@echo "🐳 Building DiffKeeper + Postgres demo..."
	docker build -t diffkeeper-postgres:latest -f Dockerfile.postgres .
	@echo "✅ Built: diffkeeper-postgres:latest"

# Run end-to-end demo
demo: docker-postgres
	@echo "🎬 Running demo..."
	bash demo.sh

# Clean build artifacts
clean:
	@echo "🧹 Cleaning..."
	rm -rf bin/ coverage.out
	docker rm -f diffkeeper-postgres-demo 2>/dev/null || true
	docker volume rm diffkeeper-deltas 2>/dev/null || true
	@echo "✅ Clean complete"
