# Multi-stage build for MaxIOFS

# Stage 1: Build frontend
FROM node:24-alpine AS web-builder

RUN apk add --no-cache python3 make g++

WORKDIR /app/web/frontend

COPY web/frontend/package*.json ./
RUN npm ci

COPY web/frontend/ ./
RUN npm run build

# Stage 2: Build Go application
FROM golang:1.25.8-alpine AS go-builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=web-builder /app/web/frontend/dist ./web/frontend/dist

ARG VERSION=docker
ARG COMMIT=unknown
ARG BUILD_DATE
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-w -s -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${BUILD_DATE}" \
    -o maxiofs ./cmd/maxiofs

# Stage 3: Final runtime image
FROM alpine:latest

# su-exec: lightweight privilege-drop utility (replaces gosu, avoids setuid binary)
RUN apk --no-cache add ca-certificates tzdata curl su-exec

# Create non-root user that will own the process after privilege drop
RUN addgroup -g 1001 -S maxiofs && \
    adduser -u 1001 -S maxiofs -G maxiofs

WORKDIR /app

# Copy binary and entrypoint
COPY --from=go-builder /app/maxiofs .
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

# Pre-create data directory with correct ownership.
# For named volumes Docker will use this directory; for bind mounts the
# entrypoint will fix ownership at runtime (runs as root by default).
RUN mkdir -p /data && chown -R maxiofs:maxiofs /data /app

# Declare the default data volume.
# Users can override with a named volume or a bind mount — both work.
VOLUME ["/data"]

EXPOSE 8080 8081

HEALTHCHECK --interval=30s --timeout=10s --start-period=15s --retries=3 \
    CMD curl -f http://localhost:8081/api/v1/health || exit 1

ENV MAXIOFS_LISTEN=":8080"
ENV MAXIOFS_CONSOLE_LISTEN=":8081"
ENV MAXIOFS_DATA_DIR="/data"
ENV MAXIOFS_LOG_LEVEL="info"

# Run as root so the entrypoint can fix bind-mount permissions, then drops
# to the maxiofs user via su-exec before starting the server.
ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["--data-dir", "/data"]
