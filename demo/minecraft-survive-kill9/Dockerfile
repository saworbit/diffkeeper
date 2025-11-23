# Demo: Minecraft server with DiffKeeper protecting /data
FROM golang:1.23-alpine AS builder
WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /build/diffkeeper .

FROM itzg/minecraft-server:java17

COPY --from=builder /build/diffkeeper /usr/local/bin/diffkeeper
RUN mkdir -p /deltas

# Wrap the stock /start script with DiffKeeper
ENTRYPOINT ["/usr/local/bin/diffkeeper", \
  "--state-dir=/data", \
  "--store=/deltas/mc.bolt", \
  "--", \
  "/start"]

VOLUME /deltas
