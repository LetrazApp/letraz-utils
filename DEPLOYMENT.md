# Letraz Scraper - Production Deployment

## Building After Code Updates

### 1. Build Docker Image
```bash
docker build -t letraz-scrapper:latest .
```

### 2. Tag for Registry (Optional)
```bash
docker tag letraz-scrapper:latest your-registry/letraz-scrapper:v1.0.0
docker push your-registry/letraz-scrapper:v1.0.0
```

## Server Deployment

### 1. Create Environment File
```bash
cat > .env << EOF
LLM_API_KEY=your-claude-api-key
CAPTCHA_API_KEY=your-2captcha-api-key
FIRECRAWL_API_KEY=your-firecrawl-api-key
DATA_DIR=/app/data
LOG_LEVEL=warn
WORKER_POOL_SIZE=20
WORKER_QUEUE_SIZE=200
WORKER_RATE_LIMIT=120
EOF
```

### 2. Create Persistent Directories
```bash
mkdir -p data logs tmp
```

### 3. Run Container
```bash
docker run -d \
  --name letraz-scrapper \
  --env-file .env \
  -p 8080:8080 \
  --restart unless-stopped \
  -v $(pwd)/data:/app/data \
  -v $(pwd)/logs:/app/logs \
  -v $(pwd)/tmp:/app/tmp \
  letraz-scrapper:latest
```

## Health Check
```bash
curl http://localhost:8080/health
```

## Container Management
```bash
# View logs
docker logs -f letraz-scrapper

# Stop container
docker stop letraz-scrapper && docker rm letraz-scrapper

# Update deployment
docker stop letraz-scrapper && docker rm letraz-scrapper
docker pull letraz-scrapper:latest  # or build new image
# Run container command again
```

## Important Notes
- **Persistent Data**: `data/` folder contains captcha domain intelligence - don't delete
- **Memory**: Minimum 2GB RAM recommended for browser automation
- **API Keys**: All three API keys required for full functionality
- **Ports**: Ensure port 8080 is accessible from your network 