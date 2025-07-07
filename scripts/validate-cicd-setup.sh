#!/bin/bash

# CI/CD Setup Validation Script for Letraz-Utils
# This script validates that all required components for the CI/CD pipeline are properly configured

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Counters
PASSED=0
FAILED=0
WARNINGS=0

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[PASS]${NC} $1"
    ((PASSED++))
}

log_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
    ((WARNINGS++))
}

log_error() {
    echo -e "${RED}[FAIL]${NC} $1"
    ((FAILED++))
}

# Header
echo -e "${BLUE}"
echo "=========================================="
echo " Letraz-Utils CI/CD Setup Validation"
echo "=========================================="
echo -e "${NC}"

# Check 1: Required files exist
log_info "Checking required CI/CD files..."

if [ -f ".circleci/config.yml" ]; then
    log_success "CircleCI setup config exists"
else
    log_error "CircleCI setup config missing: .circleci/config.yml"
fi

if [ -f ".circleci/continue-config.yml" ]; then
    log_success "CircleCI main config exists"
else
    log_error "CircleCI main config missing: .circleci/continue-config.yml"
fi

if [ -f ".circleci/deploy.sh" ]; then
    log_success "Deployment script exists"
    
    # Check if deployment script is executable
    if [ -x ".circleci/deploy.sh" ]; then
        log_success "Deployment script is executable"
    else
        log_warning "Deployment script is not executable (run: chmod +x .circleci/deploy.sh)"
    fi
else
    log_error "Deployment script missing: .circleci/deploy.sh"
fi

if [ -f "docs/CICD_SETUP.md" ]; then
    log_success "CI/CD setup documentation exists"
else
    log_warning "CI/CD setup documentation missing: docs/CICD_SETUP.md"
fi

# Check 2: Docker configuration
log_info "Checking Docker configuration..."

if [ -f "Dockerfile" ]; then
    log_success "Dockerfile exists"
    
    # Check if Dockerfile uses multi-stage build
    if grep -q "FROM.*AS builder" Dockerfile; then
        log_success "Dockerfile uses multi-stage build"
    else
        log_warning "Dockerfile should use multi-stage build for optimization"
    fi
    
    # Check if Dockerfile has health check
    if grep -q "HEALTHCHECK" Dockerfile; then
        log_success "Dockerfile includes health check"
    else
        log_warning "Dockerfile should include HEALTHCHECK instruction"
    fi
else
    log_error "Dockerfile missing"
fi

if [ -f ".dockerignore" ]; then
    log_success ".dockerignore exists"
else
    log_warning ".dockerignore missing (recommended for smaller build context)"
fi

# Check 3: Go application structure
log_info "Checking Go application structure..."

if [ -f "go.mod" ]; then
    log_success "go.mod exists"
    
    # Check Go version
    GO_VERSION=$(grep "^go " go.mod | awk '{print $2}')
    if [ ! -z "$GO_VERSION" ]; then
        log_success "Go version specified: $GO_VERSION"
    else
        log_warning "Go version not specified in go.mod"
    fi
else
    log_error "go.mod missing"
fi

if [ -f "go.sum" ]; then
    log_success "go.sum exists"
else
    log_warning "go.sum missing (run: go mod download)"
fi

if [ -d "cmd/server" ]; then
    log_success "Application entry point exists"
else
    log_error "Application entry point missing: cmd/server/"
fi

# Check 4: Configuration files
log_info "Checking configuration files..."

if [ -f "configs/config.yaml" ]; then
    log_success "Application config exists"
else
    log_warning "Application config missing: configs/config.yaml"
fi

if [ -f "env.example" ]; then
    log_success "Environment example file exists"
else
    log_warning "Environment example missing: env.example"
fi

# Check 5: CircleCI config validation
log_info "Validating CircleCI configuration..."

# Check if CircleCI CLI is available
if command -v circleci &> /dev/null; then
    log_success "CircleCI CLI is available"
    
    # Validate configuration
    if circleci config validate .circleci/config.yml &>/dev/null; then
        log_success "CircleCI setup config is valid"
    else
        log_error "CircleCI setup config validation failed"
    fi
    
    if circleci config validate .circleci/continue-config.yml &>/dev/null; then
        log_success "CircleCI main config is valid"
    else
        log_error "CircleCI main config validation failed"
    fi
else
    log_warning "CircleCI CLI not installed (optional but recommended for validation)"
    log_info "Install with: curl -fLSs https://raw.githubusercontent.com/CircleCI-Public/circleci-cli/master/install.sh | bash"
fi

# Check 6: Security considerations
log_info "Checking security considerations..."

if [ -f ".gitignore" ]; then
    log_success ".gitignore exists"
    
    # Check if sensitive files are ignored
    if grep -q "\.env" .gitignore; then
        log_success ".env files are gitignored"
    else
        log_warning ".env files should be added to .gitignore"
    fi
    
    if grep -q "\.pem\|\.key\|\.p12" .gitignore; then
        log_success "Key files are gitignored"
    else
        log_warning "Consider adding key file patterns to .gitignore"
    fi
else
    log_error ".gitignore missing"
fi

# Check if any sensitive files are tracked
SENSITIVE_FILES=$(git ls-files 2>/dev/null | grep -E "\.(env|pem|key|p12)$" | head -5 || true)
if [ ! -z "$SENSITIVE_FILES" ]; then
    log_error "Sensitive files found in git tracking: $SENSITIVE_FILES"
fi

# Check 7: Documentation
log_info "Checking documentation..."

if [ -f "README.md" ]; then
    log_success "README.md exists"
    
    # Check if README mentions CI/CD
    if grep -qi "ci\|cd\|circleci\|deployment" README.md; then
        log_success "README mentions CI/CD"
    else
        log_warning "Consider adding CI/CD information to README"
    fi
else
    log_warning "README.md missing"
fi

if [ -f "DEPLOYMENT.md" ]; then
    log_success "Deployment documentation exists"
else
    log_warning "Deployment documentation missing"
fi

# Check 8: Git configuration
log_info "Checking Git configuration..."

# Check if we're in a git repository
if git rev-parse --git-dir > /dev/null 2>&1; then
    log_success "Git repository detected"
    
    # Check current branch
    CURRENT_BRANCH=$(git branch --show-current)
    log_info "Current branch: $CURRENT_BRANCH"
    
    # Check if main branch exists
    if git show-ref --verify --quiet refs/heads/main; then
        log_success "Main branch exists"
    elif git show-ref --verify --quiet refs/heads/master; then
        log_warning "Using master branch (consider renaming to main)"
    else
        log_error "Neither main nor master branch found"
    fi
    
    # Check if origin remote exists
    if git remote | grep -q "origin"; then
        log_success "Origin remote configured"
        
        ORIGIN_URL=$(git remote get-url origin)
        if [[ $ORIGIN_URL == *"github.com"* ]]; then
            log_success "Origin points to GitHub"
        else
            log_warning "Origin does not point to GitHub"
        fi
    else
        log_error "Origin remote not configured"
    fi
else
    log_error "Not in a Git repository"
fi

# Check 9: Environment variables guidance
log_info "Checking environment variable requirements..."

log_info "Required environment variables for production:"
echo "  - GITHUB_TOKEN (GitHub Container Registry access)"
echo "  - GITHUB_USERNAME (GitHub username)"
echo "  - SSH_HOST (Production server IP/hostname)"
echo "  - SSH_USER (SSH username for production server)"
echo "  - LLM_API_KEY (Claude API key)"
echo "  - CAPTCHA_API_KEY (2captcha API key)"
echo "  - FIRECRAWL_API_KEY (Firecrawl API key)"

# Summary
echo ""
echo -e "${BLUE}=========================================="
echo " Validation Summary"
echo -e "==========================================${NC}"
echo -e "${GREEN}Passed: $PASSED${NC}"
echo -e "${YELLOW}Warnings: $WARNINGS${NC}"
echo -e "${RED}Failed: $FAILED${NC}"

if [ $FAILED -eq 0 ]; then
    echo ""
    echo -e "${GREEN}🎉 CI/CD setup validation completed successfully!${NC}"
    echo ""
    echo "Next steps:"
    echo "1. Set up CircleCI project and connect your repository"
    echo "2. Configure environment variables in CircleCI contexts"
    echo "3. Add SSH keys to CircleCI project settings"
    echo "4. Push to main branch to trigger first deployment"
    echo ""
    echo "For detailed setup instructions, see: docs/CICD_SETUP.md"
    exit 0
else
    echo ""
    echo -e "${RED}❌ CI/CD setup validation failed with $FAILED error(s)${NC}"
    echo ""
    echo "Please fix the errors above before proceeding with CI/CD setup."
    echo "For detailed setup instructions, see: docs/CICD_SETUP.md"
    exit 1
fi 