# Multi-stage build for MaxIOFS

# Stage 1: Build frontend — always runs on the build host (native, no QEMU)
FROM --platform=$BUILDPLATFORM node:24-alpine AS web-builder

RUN apk add --no-cache python3 make g++

WORKDIR /app/web/frontend

COPY web/frontend/package*.json ./
RUN npm ci

COPY web/frontend/ ./
RUN npm run build

# Stage 2: Build Go binary — always runs on the build host (native, cross-compiles for target)
FROM --platform=$BUILDPLATFORM golang:1.25.8 AS go-builder

RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*

# These are injected by buildx for the target platform
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=web-builder /app/web/frontend/dist ./web/frontend/dist

ARG VERSION=docker
ARG COMMIT=unknown
ARG BUILD_DATE
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags "-w -s -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${BUILD_DATE}" \
    -o maxiofs ./cmd/maxiofs

# Stage 3: Final runtime image — uses the target platform
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    curl \
    gosu \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user that will own the process after privilege drop
RUN groupadd -g 1001 maxiofs && useradd -u 1001 -g maxiofs -s /sbin/nologin -M maxiofs

WORKDIR /app

COPY --from=go-builder /app/maxiofs .
COPY config.example.yaml /app/config.example.yaml
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

RUN mkdir -p /data && chown -R maxiofs:maxiofs /data /app

VOLUME ["/data"]

EXPOSE 8080 8081

HEALTHCHECK --interval=30s --timeout=10s --start-period=15s --retries=3 \
    CMD curl -f http://localhost:8081/api/v1/health || exit 1

ENV MAXIOFS_LISTEN=":8080"
ENV MAXIOFS_CONSOLE_LISTEN=":8081"
ENV MAXIOFS_DATA_DIR="/data"
ENV MAXIOFS_LOG_LEVEL="info"

ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["--data-dir", "/data"]
