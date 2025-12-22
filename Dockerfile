# Multi-stage build for MaxIOFS

# Stage 1: Build frontend
FROM node:24-alpine AS web-builder

# Install build dependencies for native modules
RUN apk add --no-cache python3 make g++

WORKDIR /app/web/frontend

# Copy package files
COPY web/frontend/package*.json ./

# Install dependencies
RUN npm ci

# Copy frontend source
COPY web/frontend/ ./

# Build frontend
RUN npm run build

# Stage 2: Build Go application
FROM golang:1.25-alpine AS go-builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Copy built web assets from previous stage
COPY --from=web-builder /app/web/frontend/dist ./web/frontend/dist

# Build the application
ARG VERSION=docker
ARG COMMIT=unknown
ARG BUILD_DATE
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-w -s -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${BUILD_DATE}" \
    -o maxiofs ./cmd/maxiofs

# Stage 3: Final runtime image
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata curl

# Create non-root user
RUN addgroup -g 1001 -S maxiofs && \
    adduser -u 1001 -S maxiofs -G maxiofs

WORKDIR /app

# Copy binary from builder stage
COPY --from=go-builder /app/maxiofs .

# Create data directory
RUN mkdir -p /data && \
    chown -R maxiofs:maxiofs /data /app

# Switch to non-root user
USER maxiofs

# Create volume for data
VOLUME ["/data"]

# Expose ports
EXPOSE 8080 8081

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8081/api/v1/health || exit 1

# Default configuration
ENV MAXIOFS_LISTEN=":8080"
ENV MAXIOFS_CONSOLE_LISTEN=":8081"
ENV MAXIOFS_DATA_DIR="/data"
ENV MAXIOFS_LOG_LEVEL="info"

# Default command
ENTRYPOINT ["./maxiofs"]
CMD ["--data-dir", "/data"]