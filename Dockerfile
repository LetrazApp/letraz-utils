# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o bin/letraz-utils cmd/server/main.go

# Production stage
FROM alpine:latest

# Install runtime dependencies including Chrome for headed scraping
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    chromium \
    chromium-chromedriver \
    && rm -rf /var/cache/apk/*

# Create non-root user
RUN addgroup -g 1001 -S utilsuser && \
    adduser -u 1001 -S utilsuser -G utilsuser

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/bin/letraz-utils /app/letraz-utils

# Copy configuration files
COPY --from=builder /app/configs/ /app/configs/

# Create necessary directories
RUN mkdir -p /app/tmp /app/data && \
    chown -R utilsuser:utilsuser /app

# Switch to non-root user
USER utilsuser

# Set environment variables for Chrome
ENV CHROME_BIN=/usr/bin/chromium-browser
ENV CHROME_PATH=/usr/bin/chromium-browser
ENV PATH="/usr/bin:$PATH"

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Expose port
EXPOSE 8080

# Run the application
CMD ["./letraz-utils"] 