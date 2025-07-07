# Letraz Utils

A high-performance Go-based utility service for web scraping and content extraction with built-in AI processing, rate limiting, and concurrent processing capabilities.

## 🚀 Features

- **🔄 Concurrent Processing**: Goroutine-based worker pool for handling multiple requests simultaneously
- **⚡ Rate Limiting**: Intelligent per-domain rate limiting to prevent overwhelming target websites
- **🛡️ Circuit Breaker**: Automatic failure detection and recovery for improved reliability
- **🧠 AI-Powered**: LLM integration for intelligent content extraction and processing
- **🌐 Multiple Engines**: Support for browser-based, API-based, and hybrid scraping approaches
- **📊 Real-time Monitoring**: Comprehensive statistics and health monitoring APIs
- **⚙️ Highly Configurable**: YAML-based configuration with environment variable overrides
- **🐳 Container Ready**: Docker support with multi-platform builds

## 📋 Table of Contents

- [Quick Start](#quick-start)
- [Installation](#installation)
- [Configuration](#configuration)
- [Development](#development)
- [License](#license)

## 🚀 Quick Start

### Prerequisites

- Go 1.23 or higher
- Docker (optional, for containerized deployment)
- Chrome/Chromium browser (for browser-based scraping)

### 1. Clone the Repository

```bash
git clone https://github.com/letrazapp/letraz-utils.git
cd letraz-utils
```

### 2. Install Dependencies

```bash
go mod download
```

### 3. Configure Environment

```bash
# Copy environment template
cp env.example .env

# Edit .env with your API keys
nano .env
```

### 4. Run the Service

```bash
# Development mode
make dev

# Or build and run
make build
make run
```

### 5. Test the API

```bash
# Health check
curl http://localhost:8080/health

# Test scraping
curl -X POST http://localhost:8080/api/v1/scrape \
  -H "Content-Type: application/json" \
  -d '{"url": "https://example.com", "options": {"engine": "firecrawl"}}'
```

## 🛠️ Installation

### From Source

```bash
# Clone repository
git clone https://github.com/letrazapp/letraz-utils.git
cd letraz-utils

# Install dependencies
go mod download

# Build binary
go build -o bin/letraz-utils cmd/server/main.go

# Run
./bin/letraz-utils
```

### Using Docker

```bash
# Build image
docker build -t letraz-utils .

# Run container
docker run -p 8080:8080 --env-file .env letraz-utils
```

### Using Docker Compose

```yaml
version: '3.8'
services:
  letraz-utils:
    build: .
    ports:
      - "8080:8080"
    env_file:
      - .env
    volumes:
      - ./data:/app/data
      - ./logs:/app/logs
```

## ⚙️ Configuration

The service can be configured via YAML files and environment variables.

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | Server port | `8080` |
| `HOST` | Server host | `0.0.0.0` |
| `LLM_API_KEY` | Claude API key | Required |
| `FIRECRAWL_API_KEY` | Firecrawl API key | Optional |
| `CAPTCHA_API_KEY` | 2captcha API key | Optional |
| `LOG_LEVEL` | Log level (debug, info, warn, error) | `info` |
| `WORKER_POOL_SIZE` | Number of worker goroutines | `10` |
| `WORKER_RATE_LIMIT` | Requests per minute | `60` |

### Configuration File

Create `configs/config.yaml`:

```yaml
server:
  port: 8080
  host: "0.0.0.0"

workers:
  pool_size: 10
  queue_size: 100
  rate_limit: 60

llm:
  provider: "claude"
  model: "claude-3-haiku-20240307"
  max_tokens: 4096

scraper:
  headless_mode: true
  stealth_mode: true
  request_timeout: "30s"
```

## 🔧 Development

### Prerequisites

- Go 1.23+
- Make
- Docker (optional)

### Setup Development Environment

```bash
# Clone repository
git clone https://github.com/letrazapp/letraz-utils.git
cd letraz-utils

# Install development tools
make install

# Run development server with hot reload
make dev
```

### Available Make Commands

```bash
make help          # Show all available commands
make dev           # Start development server
make build         # Build binary
make test          # Run tests
make test-coverage # Run tests with coverage
make lint          # Run linter
make fmt           # Format code
make docker-build  # Build Docker image
```

### Testing

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# Run specific test
go test -v ./internal/scraper/...
```

### Code Quality

```bash
# Format code
make fmt

# Run linter
make lint

# Check for security issues
gosec ./...
```

## 🐳 Docker Deployment

### Build Multi-Platform Image

```bash
# Setup buildx (one-time)
make docker-setup-buildx

# Build and push
make docker-push
```

### Production Deployment

```bash
# Create environment file
cp env.example .env
# Edit .env with production values

# Run container
docker run -d \
  --name letraz-utils \
  --env-file .env \
  -p 8080:8080 \
  --restart unless-stopped \
  -v $(pwd)/data:/app/data \
  -v $(pwd)/logs:/app/logs \
  ghcr.io/letrazapp/letraz-utils:latest
```

## 📈 Monitoring

### Metrics

The service exposes various metrics:

- Worker pool statistics
- Request processing times
- Error rates
- Rate limiting status
- Health check status

### Logging

Structured JSON logging with configurable levels:

```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "level": "info",
  "msg": "Processing scrape request",
  "url": "https://example.com",
  "engine": "firecrawl",
  "duration": "2.3s"
}
```

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
