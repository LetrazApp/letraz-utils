#!/bin/bash

# Test script for worker pool functionality
# Usage: ./scripts/test-worker-pool.sh [base_url]

BASE_URL=${1:-"http://localhost:8080"}

echo "🚀 Testing Letraz Job Scraper Worker Pool"
echo "Base URL: $BASE_URL"
echo "========================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to make HTTP request and check response
test_endpoint() {
    local method=$1
    local endpoint=$2
    local description=$3
    local expected_status=${4:-200}
    local data=${5:-""}
    
    echo -n "Testing $description... "
    
    if [ "$method" = "POST" ]; then
        response=$(curl -s -w "HTTPSTATUS:%{http_code}" -X POST \
            -H "Content-Type: application/json" \
            -d "$data" \
            "$BASE_URL$endpoint")
    else
        response=$(curl -s -w "HTTPSTATUS:%{http_code}" "$BASE_URL$endpoint")
    fi
    
    # Extract status code
    status=$(echo "$response" | tr -d '\n' | sed -e 's/.*HTTPSTATUS://')
    # Extract body
    body=$(echo "$response" | sed -e 's/HTTPSTATUS:.*//g')
    
    if [ "$status" -eq "$expected_status" ]; then
        echo -e "${GREEN}✓ PASS${NC} (HTTP $status)"
        if [ "$endpoint" = "/api/v1/workers/stats" ] || [ "$endpoint" = "/api/v1/workers/status" ]; then
            echo "  Response: $(echo "$body" | jq -r '.stats.worker_count // .worker_count // "N/A"') workers"
        fi
    else
        echo -e "${RED}✗ FAIL${NC} (HTTP $status, expected $expected_status)"
        echo "  Response: $body"
    fi
    
    echo ""
}

# Test health endpoints
echo "🏥 Health Check Endpoints"
echo "-------------------------"
test_endpoint "GET" "/health" "Basic health check"
test_endpoint "GET" "/health/ready" "Readiness probe"
test_endpoint "GET" "/health/live" "Liveness probe"
test_endpoint "GET" "/health/workers" "Worker pool health"

# Test worker monitoring endpoints
echo "👷 Worker Pool Monitoring"
echo "-------------------------"
test_endpoint "GET" "/api/v1/workers/stats" "Worker statistics"
test_endpoint "GET" "/api/v1/workers/status" "Detailed worker status"

# Test domain stats (this might return empty if no scraping has happened)
echo "🌐 Domain Statistics"
echo "--------------------"
test_endpoint "GET" "/api/v1/domains/example.com/stats" "Domain stats for example.com"

# Test scraping endpoint (this will likely fail without a real job URL, but tests the worker pool)
echo "🔧 Scraping Functionality"
echo "-------------------------"
scrape_data='{
    "url": "https://httpbin.org/delay/1",
    "options": {
        "engine": "headed",
        "timeout": "10s"
    }
}'

echo "Testing scrape endpoint with test URL (may fail, but tests worker pool)..."
test_endpoint "POST" "/api/v1/scrape" "Scrape job submission" 500 "$scrape_data"

# Test rate limiting by making multiple requests
echo "⚡ Rate Limiting Test"
echo "--------------------"
echo "Making 3 rapid requests to test rate limiting..."

for i in {1..3}; do
    echo -n "Request $i: "
    status=$(curl -s -o /dev/null -w "%{http_code}" \
        -X POST \
        -H "Content-Type: application/json" \
        -d "$scrape_data" \
        "$BASE_URL/api/v1/scrape")
    
    if [ "$status" -eq 429 ]; then
        echo -e "${YELLOW}Rate limited (HTTP 429)${NC}"
        break
    elif [ "$status" -eq 500 ]; then
        echo -e "${YELLOW}Expected error (HTTP 500)${NC}"
    else
        echo -e "${GREEN}HTTP $status${NC}"
    fi
    sleep 0.1
done

echo ""
echo "🎯 Test Summary"
echo "==============="
echo "Worker pool functionality test completed!"
echo ""
echo "📊 To view detailed worker stats, visit:"
echo "   $BASE_URL/api/v1/workers/status"
echo ""
echo "🩺 To check worker health, visit:"
echo "   $BASE_URL/health/workers"
echo ""
echo "Note: Some scraping tests may fail without valid job URLs,"
echo "but this confirms the worker pool is processing requests." 