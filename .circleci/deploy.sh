#!/bin/bash

# Production deployment script for letraz-utils
# This script handles the safe deployment of the new Docker image with rollback capabilities

set -e  # Exit on any error

# Configuration
DOCKER_TAG=${1:-"latest"}
CONTAINER_NAME="letraz-utils"
BACKUP_CONTAINER_NAME="letraz-utils-backup"
REGISTRY="ghcr.io/letrazapp"
IMAGE_NAME="letraz-utils"
FULL_IMAGE_NAME="${REGISTRY}/${IMAGE_NAME}:${DOCKER_TAG}"
HEALTH_CHECK_URL="http://localhost:8080/health"
HEALTH_CHECK_TIMEOUT=60
HEALTH_CHECK_INTERVAL=5

# PDF renderer settings
RENDERER_IMAGE_NAME="pdf-renderer"
RENDERER_FULL_IMAGE_NAME="${REGISTRY}/${RENDERER_IMAGE_NAME}:latest"
RENDERER_CONTAINER_NAME="letraz-pdf-renderer"
RENDERER_PORT=8999
RENDERER_HEALTH_URL="http://127.0.0.1:${RENDERER_PORT}/health"

# Shared Docker network
NETWORK_NAME="letraz-net"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Logging function
log() {
    echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')] $1${NC}"
}

warn() {
    echo -e "${YELLOW}[$(date +'%Y-%m-%d %H:%M:%S')] WARNING: $1${NC}"
}

error() {
    echo -e "${RED}[$(date +'%Y-%m-%d %H:%M:%S')] ERROR: $1${NC}"
}

# Function to check if container is running
is_container_running() {
    local container_name=$1
    docker ps --filter "name=${container_name}" --format "{{.Names}}" | grep -q "^${container_name}$"
}

# Function to check if container exists
container_exists() {
    local container_name=$1
    docker ps -a --filter "name=${container_name}" --format "{{.Names}}" | grep -q "^${container_name}$"
}

# Function to wait for health check
wait_for_health() {
    local timeout=$1
    local interval=$2
    local elapsed=0
    
    log "Waiting for health check to pass..."
    
    while [ $elapsed -lt $timeout ]; do
        if curl -f -s "$HEALTH_CHECK_URL" > /dev/null 2>&1; then
            log "Health check passed!"
            return 0
        fi
        
        sleep $interval
        elapsed=$((elapsed + interval))
        echo -n "."
    done
    
    error "Health check failed after ${timeout}s"
    return 1
}

# Ensure a docker network exists
ensure_network() {
    if ! docker network ls --format '{{.Name}}' | grep -q "^${NETWORK_NAME}$"; then
        log "Creating docker network ${NETWORK_NAME}..."
        docker network create ${NETWORK_NAME}
    else
        log "Docker network ${NETWORK_NAME} already exists"
    fi
}

# Ensure the pdf renderer is running and healthy
ensure_renderer() {
    log "Ensuring PDF renderer container is running..."
    if docker ps --format '{{.Names}}' | grep -q "^${RENDERER_CONTAINER_NAME}$"; then
        log "Renderer is already running"
        return 0
    fi

    # If container exists but stopped, remove it
    if docker ps -a --format '{{.Names}}' | grep -q "^${RENDERER_CONTAINER_NAME}$"; then
        warn "Removing stopped renderer container"
        docker rm -f "${RENDERER_CONTAINER_NAME}" || true
    fi

    # Ensure image exists (pull if needed)
    if ! docker image ls --format '{{.Repository}}:{{.Tag}}' | grep -q "^${RENDERER_FULL_IMAGE_NAME}$"; then
        log "Pulling renderer image: ${RENDERER_FULL_IMAGE_NAME}"
        docker pull "${RENDERER_FULL_IMAGE_NAME}"
    fi

    # Start renderer (bind to 127.0.0.1 only)
    log "Starting renderer container..."
    docker run -d \
        --name "${RENDERER_CONTAINER_NAME}" \
        --restart unless-stopped \
        --network "${NETWORK_NAME}" \
        -p 127.0.0.1:${RENDERER_PORT}:${RENDERER_PORT} \
        "${RENDERER_FULL_IMAGE_NAME}"

    # Simple health wait
    local timeout=60
    local interval=3
    local elapsed=0
    while [ $elapsed -lt $timeout ]; do
        if curl -fsS "${RENDERER_HEALTH_URL}" >/dev/null 2>&1; then
            log "Renderer health check passed!"
            return 0
        fi
        sleep $interval
        elapsed=$((elapsed + interval))
        echo -n "."
    done
    error "Renderer failed health check after ${timeout}s"
    return 1
}

# Function to rollback to previous version
rollback() {
    error "Deployment failed. Initiating rollback..."
    
    # Stop the failed container
    if is_container_running "$CONTAINER_NAME"; then
        log "Stopping failed container..."
        docker stop "$CONTAINER_NAME" || true
        docker rm "$CONTAINER_NAME" || true
    fi
    
    # Start the backup container if it exists
    if container_exists "$BACKUP_CONTAINER_NAME"; then
        log "Starting backup container..."
        docker rename "$BACKUP_CONTAINER_NAME" "$CONTAINER_NAME"
        docker start "$CONTAINER_NAME"
        
        # Wait for rollback to be healthy
        if wait_for_health $HEALTH_CHECK_TIMEOUT $HEALTH_CHECK_INTERVAL; then
            log "Rollback successful!"
            return 0
        else
            error "Rollback failed - manual intervention required!"
            return 1
        fi
    else
        error "No backup container found - manual intervention required!"
        return 1
    fi
}

# Function to cleanup old images
cleanup_old_images() {
    log "Cleaning up old Docker images..."
    
    # Keep only the last 3 images
    docker images "${REGISTRY}/${IMAGE_NAME}" --format "{{.Repository}}:{{.Tag}}" | \
        grep -v "latest" | \
        sort -V | \
        head -n -3 | \
        xargs -r docker rmi || true
}

# Main deployment function
deploy() {
    log "Starting deployment of ${FULL_IMAGE_NAME}..."
    
    # Check if .env file exists
    if [ ! -f ".env" ]; then
        error ".env file not found in current directory"
        return 1
    fi
    
    # Pull the new image
    log "Pulling new Docker image: ${FULL_IMAGE_NAME}..."
    if ! docker pull "$FULL_IMAGE_NAME"; then
        error "Failed to pull Docker image"
        return 1
    fi
    
    # Create backup of current container if it exists
    if is_container_running "$CONTAINER_NAME"; then
        log "Creating backup of current container..."
        
        # Remove old backup if it exists
        if container_exists "$BACKUP_CONTAINER_NAME"; then
            docker rm -f "$BACKUP_CONTAINER_NAME" || true
        fi
        
        # Stop current container and rename it as backup
        docker stop "$CONTAINER_NAME"
        docker rename "$CONTAINER_NAME" "$BACKUP_CONTAINER_NAME"
        
        log "Current container backed up as ${BACKUP_CONTAINER_NAME}"
    fi
    
    # Create necessary directories
    mkdir -p data logs tmp
    
    # Ensure shared network and renderer
    ensure_network
    ensure_renderer

    # Start new container
    log "Starting new container with image: ${FULL_IMAGE_NAME}..."
    docker run -d \
        --name "$CONTAINER_NAME" \
        --env-file .env \
        -p 8080:8080 \
        --network "${NETWORK_NAME}" \
        --restart unless-stopped \
        --memory=2g \
        --log-driver json-file \
        --log-opt max-size=10m \
        --log-opt max-file=3 \
        -e PDF_RENDERER_URL=http://${RENDERER_CONTAINER_NAME}:${RENDERER_PORT} \
        -v $(pwd)/data:/app/data \
        -v $(pwd)/logs:/app/logs \
        -v $(pwd)/tmp:/app/tmp \
        "$FULL_IMAGE_NAME"
    
    # Wait for the new container to be healthy
    if wait_for_health $HEALTH_CHECK_TIMEOUT $HEALTH_CHECK_INTERVAL; then
        log "New container is healthy!"
        
        # Remove backup container if deployment was successful
        if container_exists "$BACKUP_CONTAINER_NAME"; then
            log "Removing backup container..."
            docker rm "$BACKUP_CONTAINER_NAME"
        fi
        
        # Cleanup old images
        cleanup_old_images
        
        log "Deployment completed successfully!"
        return 0
    else
        error "New container failed health check"
        rollback
        return 1
    fi
}

# Function to display deployment status
show_status() {
    log "=== Deployment Status ==="
    log "Docker Tag: ${DOCKER_TAG}"
    log "Image: ${FULL_IMAGE_NAME}"
    log "Health Check URL: ${HEALTH_CHECK_URL}"
    
    if is_container_running "$CONTAINER_NAME"; then
        log "Container Status: Running"
        log "Container ID: $(docker ps --filter "name=${CONTAINER_NAME}" --format "{{.ID}}")"
    else
        warn "Container Status: Not Running"
    fi
    
    log "=========================="
}

# Function to show help
show_help() {
    echo "Usage: $0 [DOCKER_TAG]"
    echo ""
    echo "Deploy letraz-utils with the specified Docker tag"
    echo ""
    echo "Parameters:"
    echo "  DOCKER_TAG    Docker image tag to deploy (default: latest)"
    echo ""
    echo "Examples:"
    echo "  $0 latest"
    echo "  $0 abc1234"
    echo "  $0 v1.0.0"
    echo ""
    echo "Environment:"
    echo "  Requires .env file in current directory"
    echo "  Container will be named: $CONTAINER_NAME"
    echo ""
}

# Main execution
main() {
    if [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
        show_help
        exit 0
    fi
    
    if [ "$1" = "--status" ] || [ "$1" = "-s" ]; then
        show_status
        exit 0
    fi
    
    log "Starting letraz-utils deployment..."
    log "Docker tag: ${DOCKER_TAG}"
    
    # Ensure we're in the correct directory
    if [ ! -f ".env" ]; then
        error "Please run this script from the directory containing the .env file"
        exit 1
    fi
    
    # Login to GitHub Container Registry
    if [ -n "$GITHUB_TOKEN" ] && [ -n "$GITHUB_USERNAME" ]; then
        log "Logging in to GitHub Container Registry..."
        echo "$GITHUB_TOKEN" | docker login ghcr.io -u "$GITHUB_USERNAME" --password-stdin
    else
        warn "GitHub credentials not found. Make sure GITHUB_TOKEN and GITHUB_USERNAME are set."
    fi
    
    # Run deployment
    if deploy; then
        log "🎉 Deployment successful!"
        show_status
        exit 0
    else
        error "💥 Deployment failed!"
        exit 1
    fi
}

# Run main function with all arguments
main "$@" 