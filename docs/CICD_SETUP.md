# CI/CD Pipeline Setup Guide for Letraz-Utils

This guide walks you through setting up a complete CI/CD pipeline for the letraz-utils application using CircleCI.

## Overview

The CI/CD pipeline provides:
- ✅ **Automated Testing**: Go tests and linting on every commit
- ✅ **Multi-Platform Builds**: Native ARM64 and AMD64 Docker images
- ✅ **Secure Deployment**: SSH-based deployment with proper secret management
- ✅ **Health Checks**: Automated verification and rollback capabilities
- ✅ **Notifications**: Success/failure notifications via webhooks
- ✅ **Docker Layer Caching**: Faster builds with intelligent caching

## Pipeline Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Git Push to   │    │   CircleCI      │    │   Production    │
│   Main Branch   │ -> │   Pipeline      │ -> │   Server        │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │ Test & Lint     │
                    │ (Go 1.23)       │
                    └─────────────────┘
                              │
                    ┌─────────┴─────────┐
                    │                   │
                    ▼                   ▼
          ┌─────────────────┐ ┌─────────────────┐
          │ Build AMD64     │ │ Build ARM64     │
          │ (Docker x86)    │ │ (Native ARM64)  │
          └─────────────────┘ └─────────────────┘
                    │                   │
                    └─────────┬─────────┘
                              ▼
                    ┌─────────────────┐
                    │ Create Multi-   │
                    │ Platform        │
                    │ Manifest        │
                    └─────────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │ Deploy to       │
                    │ Production      │
                    │ (SSH + Health)  │
                    └─────────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │ Send            │
                    │ Notifications   │
                    └─────────────────┘
```

## Prerequisites

1. **CircleCI Account**: [Sign up at CircleCI](https://circleci.com/signup/)
2. **GitHub Repository**: This repository connected to CircleCI
3. **Production Server**: Linux server with Docker installed
4. **GitHub Personal Access Token**: For GHCR access
5. **SSH Key Pair**: For secure server access

## Step 1: CircleCI Project Setup

### 1.1 Connect Repository to CircleCI

1. Log in to [CircleCI](https://app.circleci.com/)
2. Click "Set Up Project" for your `letraz-utils` repository
3. Choose "Fast" setup to use existing config files
4. CircleCI will automatically detect the `.circleci/config.yml` file

### 1.2 Enable Dynamic Configuration

1. Go to **Project Settings** → **Advanced**
2. Enable **"Enable dynamic config using setup workflows"**
3. This allows the pipeline to make decisions based on changes

## Step 2: Generate Required Keys and Tokens

### 2.1 GitHub Personal Access Token

1. Go to [GitHub Settings](https://github.com/settings/tokens) → **Developer settings** → **Personal access tokens** → **Tokens (classic)**
2. Click **"Generate new token (classic)"**
3. Set expiration to **"No expiration"** (or 1 year)
4. Select scopes:
   - `write:packages` (for GHCR push)
   - `read:packages` (for GHCR pull)
   - `repo` (for repository access)
5. Copy the token immediately (you won't see it again)

### 2.2 SSH Key Pair Generation

Generate a new SSH key pair for deployment:

```bash
# Generate SSH key pair (don't use a passphrase for automation)
ssh-keygen -t ed25519 -C "circleci-deployment" -f ~/.ssh/circleci_deployment

# Display the public key (add this to your server)
cat ~/.ssh/circleci_deployment.pub

# Display the private key (add this to CircleCI)
cat ~/.ssh/circleci_deployment
```

### 2.3 Production Server Setup

On your production server:

```bash
# Add the public key to authorized_keys
mkdir -p ~/.ssh
echo "your-public-key-here" >> ~/.ssh/authorized_keys
chmod 600 ~/.ssh/authorized_keys
chmod 700 ~/.ssh

# Install Docker (if not installed)
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
sudo usermod -aG docker $USER

# Create deployment directory
mkdir -p ~/app
cd ~/app

# Create production .env file
cp /path/to/your/.env.example .env
# Edit .env with your production values
```

## Step 3: Configure CircleCI Secrets

### 3.1 Create a Context

1. Go to **Organization Settings** → **Contexts**
2. Click **"Create Context"**
3. Name it: `production-deployment`

### 3.2 Add Environment Variables to Context

Add these environment variables to the `production-deployment` context:

| Variable Name | Value | Description |
|---------------|-------|-------------|
| `GITHUB_TOKEN` | `your-github-token` | GitHub PAT for GHCR access |
| `GITHUB_USERNAME` | `your-github-username` | Your GitHub username |
| `SSH_HOST` | `your-server-ip` | Production server IP/hostname |
| `SSH_USER` | `your-server-user` | SSH username (e.g., `ubuntu`, `root`) |
| `WEBHOOK_URL` | `your-webhook-url` | (Optional) Slack/Discord webhook for notifications |

### 3.3 Add SSH Key to CircleCI

1. Go to **Project Settings** → **SSH Keys**
2. Click **"Add SSH Key"**
3. **Hostname**: Your server IP or hostname
4. **Private Key**: Paste the contents of `~/.ssh/circleci_deployment`
5. Click **"Add SSH Key"**
6. Copy the **fingerprint** shown after adding

### 3.4 Update SSH Fingerprint

1. Go back to **Organization Settings** → **Contexts** → `production-deployment`
2. Add one more environment variable:
   - **Variable Name**: `SSH_FINGERPRINT`
   - **Value**: The fingerprint from step 3.3 (remove any colons)

## Step 4: Production Environment Setup

### 4.1 Environment Variables

Create a `.env` file on your production server:

```bash
# Required API Keys (get these from respective providers)
LLM_API_KEY=your-claude-api-key-here
CAPTCHA_API_KEY=your-2captcha-api-key-here  
FIRECRAWL_API_KEY=your-firecrawl-api-key-here

# GitHub Container Registry credentials
GITHUB_TOKEN=your-github-token
GITHUB_USERNAME=your-github-username

# Application Configuration
PORT=8080
HOST=0.0.0.0
LOG_LEVEL=warn
DATA_DIR=/app/data

# Worker Pool Configuration  
WORKER_POOL_SIZE=20
WORKER_QUEUE_SIZE=200
WORKER_RATE_LIMIT=120

# Scraper Configuration
SCRAPER_HEADLESS_MODE=true
SCRAPER_STEALTH_MODE=true
```

### 4.2 Directory Structure

```bash
# Create required directories
mkdir -p ~/app/{data,logs,tmp}
cd ~/app

# Verify structure
ls -la
# Should show: .env, data/, logs/, tmp/
```

## Step 5: Test the Pipeline

### 5.1 Trigger First Deployment

1. Make a small change to the `main` branch (e.g., update README)
2. Push to GitHub:
   ```bash
   git add .
   git commit -m "feat: trigger first CI/CD deployment"
   git push origin main
   ```

### 5.2 Monitor the Pipeline

1. Go to [CircleCI Dashboard](https://app.circleci.com/)
2. Watch the pipeline execute:
   - **Setup**: Dynamic configuration
   - **Test**: Go tests and linting
   - **Build AMD64**: x86 Docker build
   - **Build ARM64**: Native ARM64 build
   - **Create Manifest**: Multi-platform image
   - **Deploy**: SSH deployment to production
   - **Notify**: Success notification

### 5.3 Verify Deployment

Check your production server:

```bash
# Check if container is running
docker ps | grep letraz-utils

# Check health endpoint
curl http://localhost:8080/health

# Check logs
docker logs letraz-utils

# Check deployment status
./deploy.sh --status
```

## Step 6: Advanced Configuration

### 6.1 Notification Setup

For Slack notifications:
1. Create a Slack webhook URL
2. Add `WEBHOOK_URL` to your CircleCI context
3. The pipeline will send deployment status messages

For Discord notifications:
1. Create a Discord webhook URL
2. Add `WEBHOOK_URL` to your CircleCI context
3. Format is the same as Slack

### 6.2 Manual Deployment

To manually trigger a deployment:

```bash
# Using CircleCI API
curl -X POST \
  https://circleci.com/api/v2/project/github/LetrazApp/letraz-utils/pipeline \
  -H "Circle-Token: YOUR-CIRCLECI-TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "parameters": {
      "force-deploy": true
    }
  }'
```

### 6.3 Rollback Procedure

If a deployment fails, the script automatically rolls back. For manual rollback:

```bash
# On production server
docker stop letraz-utils
docker rm letraz-utils

# Start previous version (if backup exists)  
docker rename letraz-utils-backup letraz-utils
docker start letraz-utils

# Or deploy specific version
./deploy.sh abc1234  # specific commit SHA
```

## Step 7: Troubleshooting

### 7.1 Common Issues

**Issue**: SSH Permission Denied
```bash
# Solution: Check SSH key was added correctly to server
ssh -i ~/.ssh/circleci_deployment user@server
```

**Issue**: Docker Build Fails
```bash
# Solution: Check GitHub token has package permissions
echo $GITHUB_TOKEN | docker login ghcr.io -u $GITHUB_USERNAME --password-stdin
```

**Issue**: Health Check Fails
```bash
# Solution: Check application logs
docker logs letraz-utils
```

### 7.2 Pipeline Status

Monitor pipeline status:
- **Green**: All steps completed successfully
- **Yellow**: Pipeline running
- **Red**: Failed step (check logs)

### 7.3 Debug Mode

For detailed debugging, add to your commit message:
```
feat: your change [debug-ci]
```

This will enable verbose logging in the pipeline.

## Step 8: Security Best Practices

### 8.1 Secret Rotation

Rotate secrets regularly:
1. **GitHub Token**: Every 6-12 months
2. **SSH Keys**: Every 12 months  
3. **API Keys**: As recommended by providers

### 8.2 Access Control

- Use principle of least privilege for all credentials
- Regularly audit who has access to CircleCI contexts
- Monitor deployment logs for suspicious activity

### 8.3 Network Security

- Consider using VPN for SSH access
- Implement firewall rules on production server
- Use non-standard SSH ports if possible

## Conclusion

Your CI/CD pipeline is now configured for:
- **Automated deployments** on every main branch push
- **Multi-platform support** for both ARM64 and AMD64
- **Secure secret management** with CircleCI contexts
- **Reliable rollback capabilities** with health checks
- **Comprehensive monitoring** with notifications

The pipeline follows industry best practices and is production-ready for the letraz-utils GenAI application.

## Support

If you encounter issues:
1. Check the [CircleCI documentation](https://circleci.com/docs/)
2. Review pipeline logs in the CircleCI dashboard
3. Verify all secrets are properly configured
4. Test SSH connectivity manually

For application-specific issues, refer to the main project documentation. 