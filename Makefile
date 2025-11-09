.PHONY: build build-ebpf test demo clean docker docker-postgres release

CLANG    ?= clang
EBPF_OBJ ?= ebpf/diffkeeper.bpf.o
EBPF_SRC ?= ebpf/diffkeeper.bpf.c
EBPF_HDR := $(wildcard ebpf/*.h ebpf/include/bpf/*.h)

build: build-ebpf
	@echo "[build] Building DiffKeeper agent..."
	go build -ldflags="-w -s" -o bin/diffkeeper main.go
	@echo "[build] Done: bin/diffkeeper"

build-ebpf: $(EBPF_OBJ)

$(EBPF_OBJ): $(EBPF_SRC) $(EBPF_HDR)
	@if [ ! -f ebpf/vmlinux.h ]; then \
		echo "[ebpf] Missing ebpf/vmlinux.h (generate via: bpftool btf dump file /sys/kernel/btf/vmlinux > ebpf/vmlinux.h)"; \
		exit 1; \
	fi
	@echo "[ebpf] Compiling kernel probes..."
	$(CLANG) -O2 -g -target bpf -D__TARGET_ARCH_x86 -Iebpf -Iebpf/include -c $(EBPF_SRC) -o $(EBPF_OBJ)
	cp $(EBPF_OBJ) pkg/ebpf/diffkeeper.bpf.o
	@echo "[ebpf] Built: $(EBPF_OBJ) (embedded copy refreshed)"

# Run tests
test:
	@echo "[test] Running tests..."
	go test -v -race -coverprofile=coverage.out ./...
	@echo "[test] Tests complete"

# Build Postgres demo image
docker-postgres:
	@echo "[docker] Building DiffKeeper + Postgres demo..."
	docker build -t diffkeeper-postgres:latest -f Dockerfile.postgres .
	@echo "[docker] Built: diffkeeper-postgres:latest"

docker:
	@echo "[docker] Building multi-arch image..."
	docker buildx build --platform linux/amd64,linux/arm64 -t ghcr.io/yourorg/diffkeeper:latest .
	@echo "[docker] Multi-arch image ready (not pushed)"

# Run end-to-end demo
demo: docker-postgres
	@echo "[demo] Running demo..."
	bash demo.sh

# Clean build artifacts
clean:
	@echo "[clean] Removing artifacts..."
	rm -rf bin/ coverage.out
	docker rm -f diffkeeper-postgres-demo 2>/dev/null || true
	docker volume rm diffkeeper-deltas 2>/dev/null || true
	@echo "[clean] Done"

release: build
	@echo "[release] Generating snapshot artifacts..."
	goreleaser release --snapshot --clean
