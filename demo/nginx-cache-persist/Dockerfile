# Demo: Nginx cache + static assets protected by DiffKeeper
FROM golang:1.23-alpine AS builder
WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /build/diffkeeper .

FROM nginx:1.27-alpine

# Agent
COPY --from=builder /build/diffkeeper /usr/local/bin/diffkeeper

# Demo assets and cache-friendly config
COPY demo/nginx-cache-persist/default.conf /etc/nginx/conf.d/default.conf
COPY demo/nginx-cache-persist/index.html /data/index.html

RUN mkdir -p /data/cache /deltas

ENV NGINX_ENTRYPOINT_QUIET_LOGS=1

# Wrap nginx with DiffKeeper
ENTRYPOINT ["/usr/local/bin/diffkeeper", \
  "--state-dir=/data", \
  "--store=/deltas/nginx.bolt", \
  "--", \
  "/docker-entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]

VOLUME /data
VOLUME /deltas
