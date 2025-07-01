# Letraz Job Scraper

A high-performance Go-based microservice for scraping job postings from various job boards with built-in rate limiting, circuit breaker patterns, and concurrent processing.

## Features

- **Concurrent Processing**: Goroutine-based worker pool for handling multiple scraping requests
- **Rate Limiting**: Per-domain rate limiting to prevent overwhelming target websites
- **Circuit Breaker**: Automatic failure detection and recovery for improved reliability
- **Multiple Engines**: Support for both headed (browser-based) and raw HTTP scraping
- **Real-time Monitoring**: Comprehensive statistics and health monitoring APIs
- **Configurable**: YAML-based configuration with environment variable overrides

## Architecture

- **Worker Pool**: Configurable number of worker goroutines processing jobs from a queue
- **Rate Limiter**: Domain-specific rate limiting with circuit breaker pattern
- **Job Queue**: In-memory job queue with configurable capacity
- **Scraper Factory**: Plugin-based scraper creation supporting multiple engines
- **API Layer**: RESTful APIs for job submission and monitoring

## API Endpoints

### Core Endpoints

#### Scrape Job
```http
POST /api/v1/scrape
Content-Type: application/json

{
    "url": "https://example.com/job/123",
    "options": {
        "engine": "headed",
        "timeout": "30s"
    }
}
```

### Health & Monitoring

#### Service Health
```http
GET /health                 # Basic health check
GET /health/ready          # Readiness probe
GET /health/live           # Liveness probe
GET /health/workers        # Worker pool health
```

#### Worker Pool Monitoring
```http
GET /api/v1/workers/stats          # Basic worker statistics
GET /api/v1/workers/status         # Detailed worker status
GET /api/v1/domains/{domain}/stats # Domain-specific rate limiting stats
```

### Response Examples

#### Successful Scrape Response
```json
{
    "success": true,
    "job": {
        "id": "uuid-here",
        "title": "Software Engineer",
        "company": "Tech Corp",
        "location": "San Francisco, CA",
        "remote": false,
        "description": "Job description...",
        "requirements": ["requirement1", "requirement2"],
        "skills": ["skill1", "skill2"],
        "salary": {
            "min": 120000,
            "max": 150000,
            "currency": "USD",
            "period": "yearly"
        },
        "application_url": "https://example.com/job/123",
        "processed_at": "2024-01-15T10:30:00Z"
    },
    "processing_time": "5.2s",
    "engine": "headed",
    "request_id": "req-uuid-here"
}
```

#### Worker Statistics Response
```json
{
    "success": true,
    "stats": {
        "initialized": true,
        "worker_count": 10,
        "queue_capacity": 100,
        "pool_stats": {
            "jobs_queued": 150,
            "jobs_processed": 145,
            "jobs_successful": 140,
            "jobs_failed": 5,
            "average_processing_time": "3.2s"
        },
        "rate_limiter_stats": {
            "example.com": {
                "requests": 25,
                "failures": 2,
                "circuit_state": "closed",
                "last_seen": "2024-01-15T10:29:00Z"
            }
        }
    }
}
```

## Configuration

### Worker Pool Configuration
```yaml
workers:
  pool_size: 10           # Number of worker goroutines
  queue_size: 100         # Job queue capacity
  rate_limit: 60          # Requests per minute per domain
  timeout: "30s"          # Job processing timeout
  max_retries: 3          # Maximum retry attempts per job
```

### Rate Limiting & Circuit Breaker
- **Rate Limiting**: Configurable requests per minute per domain
- **Circuit Breaker**: Opens after 5 consecutive failures, closes after 30 seconds
- **Automatic Cleanup**: Unused limiters are cleaned up every 5 minutes

## Installation & Usage

### Prerequisites
- Go 1.21 or higher
- Chrome/Chromium browser (for headed scraping)

### Build & Run
```bash
# Build the application
go build -o bin/server cmd/server/main.go

# Run with default configuration
./bin/server

# Run with custom configuration
CONFIG_PATH=configs/production.yaml ./bin/server
```

### Docker
```bash
# Build Docker image
docker build -t letraz-scrapper .

# Run container
docker run -p 8080:8080 letraz-scrapper
```

### Environment Variables
```bash
export PORT=8080
export HOST=0.0.0.0
export LLM_API_KEY=your-openai-api-key
export LOG_LEVEL=info
```

## Development Status

- ✅ **Phase 1**: Foundation (Project structure, API framework, health checks)
- ✅ **Phase 2**: Core Scraping Engine (Rod-based browser automation)
- ✅ **Phase 3**: Worker Pool & Rate Limiting (Concurrent processing, rate limiting, circuit breaker)
- 🚧 **Phase 4**: LLM Integration (In Progress)
- 📋 **Phase 5**: Raw HTTP Engine (Planned)
- 📋 **Phase 6**: Post-Processing Pipeline (Planned)

## Performance

- **Throughput**: 100+ requests per minute (configurable)
- **Concurrency**: Configurable worker pool (default: 10 workers)
- **Memory**: < 1GB under normal load
- **Response Time**: < 30 seconds for complex pages (configurable timeout)

## Monitoring

The service provides comprehensive monitoring through:
- **Health Checks**: Standard health check endpoints for container orchestration
- **Worker Statistics**: Real-time worker pool and job processing metrics
- **Rate Limiting Stats**: Per-domain rate limiting and circuit breaker status
- **Request Tracing**: Request ID tracking for debugging and monitoring

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details. 
