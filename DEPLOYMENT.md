# Letraz Job Scraper - Production Deployment Guide

This guide covers deploying the Letraz Job Scraper service using Docker for production environments.

## Prerequisites

- Docker 20.10 or higher
- Docker Compose (optional, for easier management)
- 2GB+ RAM (recommended for browser automation)
- 1GB+ disk space

## Environment Configuration

### Required Environment Variables

Create a `.env` file or set these environment variables:

```bash
# Server Configuration
PORT=8080
HOST=0.0.0.0
LOG_LEVEL=info

# LLM Configuration (Required)
LLM_API_KEY=your-claude-api-key-here
LLM_PROVIDER=claude
LLM_MODEL=claude-3-haiku-20240307

# Scraper Configuration
CAPTCHA_API_KEY=your-2captcha-api-key-here
FIRECRAWL_API_KEY=your-firecrawl-api-key-here

# Optional: Custom timeouts
WORKER_TIMEOUT=30s
SCRAPER_TIMEOUT=30s
```

### Optional Environment Variables

```bash
# Worker Pool Configuration
WORKER_POOL_SIZE=10
WORKER_QUEUE_SIZE=100
WORKER_RATE_LIMIT=60

# LLM Configuration
LLM_MAX_TOKENS=4096
LLM_TEMPERATURE=0.1
LLM_TIMEOUT=30s

# Firecrawl Configuration
FIRECRAWL_API_URL=https://api.firecrawl.dev
FIRECRAWL_VERSION=v1
FIRECRAWL_TIMEOUT=60s
FIRECRAWL_MAX_RETRIES=3

# Logging
LOG_FORMAT=json
LOG_OUTPUT=stdout
```

## Docker Deployment

### 1. Build the Docker Image

```bash
# Clone the repository
git clone <repository-url>
cd letraz-scrapper

# Build the image
docker build -t letraz-scrapper:latest .

# Or build with a specific tag
docker build -t letraz-scrapper:v1.0.0 .
```

### 2. Run the Container

#### Simple Run (with environment file)
```bash
# Create environment file
cat > .env << EOF
PORT=8080
HOST=0.0.0.0
LOG_LEVEL=info
LLM_API_KEY=your-claude-api-key-here
CAPTCHA_API_KEY=your-2captcha-api-key-here
FIRECRAWL_API_KEY=your-firecrawl-api-key-here
EOF

# Run container
docker run -d \
  --name letraz-scrapper \
  --env-file .env \
  -p 8080:8080 \
  --restart unless-stopped \
  letraz-scrapper:latest
```

#### Run with Individual Environment Variables
```bash
docker run -d \
  --name letraz-scrapper \
  -e PORT=8080 \
  -e HOST=0.0.0.0 \
  -e LOG_LEVEL=info \
  -e LLM_API_KEY=your-claude-api-key-here \
  -e CAPTCHA_API_KEY=your-2captcha-api-key-here \
  -e FIRECRAWL_API_KEY=your-firecrawl-api-key-here \
  -p 8080:8080 \
  --restart unless-stopped \
  letraz-scrapper:latest
```

### 3. Verify Deployment

```bash
# Check container status
docker ps | grep letraz-scrapper

# Check logs
docker logs letraz-scrapper

# Test health endpoint
curl http://localhost:8080/health

# Test scraping endpoint
curl -X POST http://localhost:8080/api/v1/scrape \
  -H "Content-Type: application/json" \
  -d '{"url": "https://example.com/job-posting"}'
```

## Production Deployment Options

### Option 1: Single Container Deployment

```bash
# Create a production environment file
cat > production.env << EOF
PORT=8080
HOST=0.0.0.0
LOG_LEVEL=warn
LLM_API_KEY=${LLM_API_KEY}
CAPTCHA_API_KEY=${CAPTCHA_API_KEY}
FIRECRAWL_API_KEY=${FIRECRAWL_API_KEY}
WORKER_POOL_SIZE=20
WORKER_QUEUE_SIZE=200
WORKER_RATE_LIMIT=120
EOF

# Run production container
docker run -d \
  --name letraz-scrapper-prod \
  --env-file production.env \
  -p 8080:8080 \
  --memory=2g \
  --restart unless-stopped \
  --log-driver json-file \
  --log-opt max-size=10m \
  --log-opt max-file=3 \
  letraz-scrapper:latest
```

### Option 2: Using Docker Compose (Recommended)

Create a `docker-compose.prod.yml` file:

```yaml
version: '3.8'

services:
  letraz-scrapper:
    image: letraz-scrapper:latest
    container_name: letraz-scrapper
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      - PORT=8080
      - HOST=0.0.0.0
      - LOG_LEVEL=warn
      - LLM_API_KEY=${LLM_API_KEY}
      - CAPTCHA_API_KEY=${CAPTCHA_API_KEY}
      - FIRECRAWL_API_KEY=${FIRECRAWL_API_KEY}
      - WORKER_POOL_SIZE=20
      - WORKER_QUEUE_SIZE=200
    deploy:
      resources:
        limits:
          memory: 2G
        reservations:
          memory: 1G
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 40s
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
```

Deploy with:
```bash
docker-compose -f docker-compose.prod.yml up -d
```

## Monitoring & Maintenance

### Health Checks

The application provides several health check endpoints:

```bash
# Basic health check
curl http://localhost:8080/health

# Readiness probe
curl http://localhost:8080/health/ready

# Liveness probe
curl http://localhost:8080/health/live

# Worker pool health
curl http://localhost:8080/health/workers
```

### Monitoring Endpoints

```bash
# Worker statistics
curl http://localhost:8080/api/v1/workers/stats

# Detailed worker status
curl http://localhost:8080/api/v1/workers/status

# Domain-specific stats
curl http://localhost:8080/api/v1/domains/example.com/stats
```

### Log Management

```bash
# View live logs
docker logs -f letraz-scrapper

# View recent logs
docker logs --tail=100 letraz-scrapper

# Search logs
docker logs letraz-scrapper 2>&1 | grep "ERROR"
```

### Container Management

```bash
# Stop container
docker stop letraz-scrapper

# Start container
docker start letraz-scrapper

# Restart container
docker restart letraz-scrapper

# Update container
docker pull letraz-scrapper:latest
docker stop letraz-scrapper
docker rm letraz-scrapper
# Run new container with same configuration
```

## Performance Tuning

### Resource Limits

For production environments, consider these resource limits:

```bash
# High-traffic deployment
docker run -d \
  --name letraz-scrapper \
  --memory=4g \
  --cpus="2.0" \
  --env-file production.env \
  -p 8080:8080 \
  letraz-scrapper:latest
```

### Environment Variables for Performance

```bash
# High-performance configuration
WORKER_POOL_SIZE=50
WORKER_QUEUE_SIZE=500
WORKER_RATE_LIMIT=300
WORKER_TIMEOUT=60s
SCRAPER_TIMEOUT=45s
```

## Troubleshooting

### Common Issues

1. **Container won't start**: Check logs with `docker logs letraz-scrapper`
2. **Health check fails**: Ensure the service is running and port 8080 is accessible
3. **Scraping fails**: Verify API keys are set correctly
4. **High memory usage**: Reduce worker pool size or increase container memory limit

### Debug Mode

Run with debug logging:
```bash
docker run -d \
  --name letraz-scrapper-debug \
  -e LOG_LEVEL=debug \
  --env-file .env \
  -p 8080:8080 \
  letraz-scrapper:latest
```

## Security Considerations

1. **API Keys**: Never hardcode API keys in the image. Use environment variables.
2. **Non-root User**: The container runs as a non-root user for security.
3. **Network**: Consider using a reverse proxy (nginx, traefik) for SSL termination.
4. **Updates**: Regularly update the base image and dependencies.

## Scaling

For horizontal scaling, deploy multiple instances behind a load balancer:

```yaml
# docker-compose.scale.yml
version: '3.8'

services:
  letraz-scrapper:
    image: letraz-scrapper:latest
    environment:
      - PORT=8080
      - HOST=0.0.0.0
      - LLM_API_KEY=${LLM_API_KEY}
      - CAPTCHA_API_KEY=${CAPTCHA_API_KEY}
      - FIRECRAWL_API_KEY=${FIRECRAWL_API_KEY}
    deploy:
      replicas: 3
      resources:
        limits:
          memory: 2G
    ports:
      - "8080-8082:8080"
```

Deploy with:
```bash
docker-compose -f docker-compose.scale.yml up -d --scale letraz-scrapper=3
``` 