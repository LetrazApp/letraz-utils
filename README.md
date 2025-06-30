# Letraz Job Scraper

A Go-based microservice for scraping job postings from various job boards, processing them through LLM APIs, and returning structured job data.

## Overview

This service is part of the Letraz ecosystem and provides:
- Real-time job posting scraping from various job boards
- AI-powered job data extraction and structuring
- Dual scraping engines (headed browser and raw HTTP)
- Rate limiting and worker pool management
- No database dependencies - stateless operation

## Quick Start

### Prerequisites

- Go 1.21 or later
- Make (optional, for convenience commands)

### Installation

1. Clone the repository:
```bash
git clone https://github.com/LetrazApp/letraz-scrapper.git
cd letraz-scrapper
```

2. Install dependencies:
```bash
go mod tidy
```

3. Copy environment configuration:
```bash
cp .env.example .env
```

4. Edit `.env` with your configuration values (especially LLM API key)

5. **One-time setup** (adds Go tools to PATH permanently):
```bash
make setup
source ~/.zshrc    # or restart terminal
```

### Running the Service

Now you can use convenient Make commands (similar to npm scripts):

1. **Quick start** (development server):
```bash
make dev
```

2. **Hot reload** (auto-restart on changes):
```bash
make hot    # Requires: make install-tools
```

3. **Build and run** (production-like):
```bash
make run
```

4. **See all available commands**:
```bash
make help
```

The service will start on `http://localhost:8080`

#### Available Commands

| Command | Description |
|---------|-------------|
| `make setup` | **One-time setup** (adds Go tools to PATH) |
| `make dev` | Start development server |
| `make hot` | Start with hot reload (like nodemon) |
| `make build` | Build the application |
| `make test` | Run tests |
| `make health` | Check service health |
| `make test-scrape` | Test scraping endpoint |
| `make clean` | Clean build artifacts |
| `make install-tools` | Install dev tools (air, linter) |

### API Endpoints

#### Health Checks
- `GET /health` - Basic health check
- `GET /health/ready` - Readiness probe
- `GET /health/live` - Liveness probe
- `GET /status` - Detailed service status

#### Main API
- `POST /api/v1/scrape` - Scrape a job posting

#### Example Request
```bash
curl -X POST http://localhost:8080/api/v1/scrape \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://example.com/job/123",
    "options": {
      "engine": "auto",
      "timeout": "30s",
      "llm_provider": "openai"
    }
  }'
```

## Configuration

The service can be configured via:
1. YAML configuration file (`configs/config.yaml`)
2. Environment variables (see `.env.example`)

Key configuration options:
- Server settings (port, timeouts)
- Worker pool configuration
- LLM provider settings
- Scraper engine options
- Logging configuration

## Development

### Project Structure

```
letraz-scrapper/
├── cmd/server/          # Application entry point
├── internal/            # Private application code
│   ├── api/            # HTTP handlers and routes
│   ├── scraper/        # Scraping engines
│   ├── llm/            # LLM integration
│   └── config/         # Configuration management
├── pkg/                # Public packages
│   ├── models/         # Data models
│   └── utils/          # Utilities
└── configs/            # Configuration files
```

### Current Status

✅ **Phase 1 Complete**: Foundation
- Echo API framework setup
- Basic health check endpoints
- Configuration management
- Logging system
- Request/response models

🚧 **Next**: Phase 2 - Core Scraping Engine
- Rod + stealth plugin integration
- Browser management
- HTML extraction

## Contributing

This project follows the implementation plan outlined in `IMPLEMENTATION_PLAN.md`.

## License

MIT License - see LICENSE file for details. 
