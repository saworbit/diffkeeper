# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy full source after deps so local packages are available
COPY . .

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o diffkeeper \
    .

# Minimal runtime
FROM alpine:3.19

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

WORKDIR /

# Copy agent binary
COPY --from=builder /build/diffkeeper /usr/local/bin/diffkeeper

# Create directories
RUN mkdir -p /data /deltas

# Set as entrypoint
ENTRYPOINT ["/usr/local/bin/diffkeeper"]
CMD ["--help"]
