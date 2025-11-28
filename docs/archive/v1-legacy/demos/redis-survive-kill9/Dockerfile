# Demo: Redis wrapped with DiffKeeper to protect /data
FROM golang:1.23-alpine AS builder
WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /build/diffkeeper .

FROM redis:7.4-alpine

COPY --from=builder /build/diffkeeper /usr/local/bin/diffkeeper
RUN mkdir -p /deltas /data

ENTRYPOINT ["/usr/local/bin/diffkeeper", \
  "--state-dir=/data", \
  "--store=/deltas/redis.bolt", \
  "--", \
  "docker-entrypoint.sh"]
CMD ["redis-server", "/usr/local/etc/redis/redis.conf"]

VOLUME /deltas
