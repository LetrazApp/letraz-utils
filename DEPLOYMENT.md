# Letraz Scraper - Production Deployment

## Building After Code Updates

### 1. Build Docker Image
```bash
docker build -t letraz-scrapper:latest .
```

### 2. Push to GitHub Container Registry (Multi-Platform)
```bash
# Login to GHCR
echo $GITHUB_TOKEN | docker login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin

# Build and push multi-platform image (supports both ARM64 and AMD64)
docker buildx create --name multiplatform --driver docker-container --bootstrap
docker buildx use multiplatform
docker buildx build --platform linux/amd64,linux/arm64 -t ghcr.io/letrazapp/letraz-scrapper:latest -t ghcr.io/letrazapp/letraz-scrapper:v1.0.0 --push .
```

**Note**: Multi-platform builds ensure your image works on both Apple Silicon (M1/M2) and Intel/AMD servers.

## Server Deployment

### 1. Login to GitHub Container Registry
```bash
# Set your GitHub credentials
export GITHUB_TOKEN=your_personal_access_token
echo $GITHUB_TOKEN | docker login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin
```

### 2. Pull Docker Image
```bash
docker pull ghcr.io/letrazapp/letraz-scrapper:latest
```

### 3. Create Environment File
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

### 4. Create Persistent Directories
```bash
mkdir -p data logs tmp
```

### 5. Run Container
```bash
docker run -d \
  --name letraz-scrapper \
  --env-file .env \
  -p 8080:8080 \
  --restart unless-stopped \
  -v $(pwd)/data:/app/data \
  -v $(pwd)/logs:/app/logs \
  -v $(pwd)/tmp:/app/tmp \
  ghcr.io/letrazapp/letraz-scrapper:latest
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
docker pull ghcr.io/letrazapp/letraz-scrapper:latest
# Run container command again
```

## Important Notes
- **Persistent Data**: `data/` folder contains captcha domain intelligence - don't delete
- **Memory**: Minimum 2GB RAM recommended for browser automation
- **API Keys**: All three API keys required for full functionality
- **Ports**: Ensure port 8080 is accessible from your network 