# Letraz Job Scraper Implementation Plan

## Overview

The Letraz Job Scraper is a Go-based microservice designed to scrape job postings from various job boards, process them through LLM APIs, and return structured job data. This service operates without database connections and focuses on real-time scraping and processing.

## Architecture Overview

```
                ┌────────────────────────────────────────┐
                │           Load Balancer / API Gateway  │ (Outside of this project's scope)
                └────────────┬───────────────────────────┘
                             │
                ┌────────────▼────────────┐
                │   Go Microservice API   │
                │   (Gin, Echo, Fiber)    │
                └────────────┬────────────┘
                             │
  ┌──────────────────────────▼─────────────────────────┐
  │            Worker Pool / Job Dispatcher            │
  │   (Goroutines + Rate Limit + Timeout Middleware)   │
  └────────────┬──────────────────────────────┬────────┘
               │                              │
 ┌─────────────▼──────────────┐   ┌────────────▼─────────────┐
 │   Scraping Engine (Headed) │   │ Scraping Engine (Raw)    │
 │   (Rod / chromedp + stealth)│   │ (Colly / HTTP Clients)  │
 └─────────────┬──────────────┘   └────────────┬─────────────┘
               │                               │
       ┌───────▼────────┐              ┌───────▼───────┐
       │ Extracted HTML │              │ Extracted HTML│
       └───────┬────────┘              └───────┬───────┘
               │                               │
          ┌────▼────────────────────────────────▼───┐
          │      Job Description Post-Processor     │
          │   (Text cleanup, summarization, etc.)   │
          └────┬───────────────────────────┬────────┘
               │                           │
    ┌──────────▼──────────┐     ┌──────────▼────────────┐
    │  External LLM API   │     │ Local LLM Runtime (opt)│
    │ (OpenAI / Claude)   │     │ (llama.cpp / Ollama)   │
    └──────────┬──────────┘     └──────────┬────────────┘
               │                           │
          ┌────▼─────────────┐       ┌─────▼────────────┐
          │  Structured JSON │       │ Structured JSON  │
          └────┬─────────────┘       └─────┬────────────┘
               │                           │
       ┌───────▼───────────────────────────▼───────────┐
       │           Final Response to Client            │
       └───────────────────────────────────────────────┘
```

## Project Structure

```
letraz-scrapper/
├── cmd/
│   └── server/
│       └── main.go                 # Application entry point
├── internal/
│   ├── api/
│   │   ├── handlers/
│   │   │   ├── health.go          # Health check handlers
│   │   │   ├── scrape.go          # Main scraping endpoints
│   │   │   └── metrics.go         # Metrics and monitoring
│   │   ├── middleware/
│   │   │   ├── cors.go            # CORS configuration
│   │   │   ├── ratelimit.go       # Rate limiting middleware
│   │   │   ├── timeout.go         # Request timeout handling
│   │   │   └── validation.go      # Request validation
│   │   └── routes/
│   │       └── routes.go          # Route definitions
│   ├── scraper/
│   │   ├── engines/
│   │   │   ├── headed/
│   │   │   │   ├── rod.go         # Rod + stealth implementation
│   │   │   │   └── browser.go     # Browser management
│   │   │   └── raw/
│   │   │       ├── colly.go       # Colly scraper implementation
│   │   │       └── http.go        # Raw HTTP client
│   │   ├── processors/
│   │   │   ├── cleaner.go         # HTML/text cleanup
│   │   │   ├── extractor.go       # Content extraction
│   │   │   └── validator.go       # Data validation
│   │   └── workers/
│   │       ├── pool.go            # Worker pool implementation
│   │       ├── dispatcher.go      # Job dispatcher
│   │       └── limiter.go         # Rate limiting logic
│   ├── llm/
│   │   ├── providers/
│   │   │   ├── openai.go          # OpenAI integration
│   │   │   ├── claude.go          # Claude integration
│   │   │   └── local.go           # Local LLM support
│   │   └── schemas/
│   │       ├── prompts.go         # LLM prompts
│   │       └── parser.go          # Response parsing
│   └── config/
│       ├── config.go              # Configuration management
│       └── env.go                 # Environment variables
├── pkg/
│   ├── models/
│   │   ├── job.go                 # Job posting models
│   │   ├── request.go             # API request models
│   │   └── response.go            # API response models
│   └── utils/
│       ├── logger.go              # Logging utilities
│       ├── errors.go              # Error handling
│       └── helpers.go             # General utilities
├── configs/
│   ├── config.yaml                # Default configuration
│   └── config.example.yaml        # Example configuration
├── scripts/
│   ├── build.sh                   # Build scripts
│   └── docker-build.sh            # Docker build scripts
├── docs/
│   ├── api.md                     # API documentation
│   └── deployment.md              # Deployment guide
├── go.mod
├── go.sum
├── Dockerfile
├── DEPLOYMENT.md
├── .gitignore
├── env.example
└── README.md
```

## Core Dependencies

```go
module letraz-scrapper

go 1.21

require (
    github.com/labstack/echo/v4 v4.11.3
    github.com/go-rod/rod v0.114.5
    github.com/go-rod/stealth v0.4.9
    github.com/gocolly/colly/v2 v2.1.0
    github.com/sashabaranov/go-openai v1.17.9
    golang.org/x/time v0.5.0
    github.com/google/uuid v1.4.0
    gopkg.in/yaml.v3 v3.0.1
    github.com/go-playground/validator/v10 v10.16.0
    github.com/sirupsen/logrus v1.9.3
    github.com/PuerkitoBio/goquery v1.8.1
)
```

## Data Models

### Job Posting Schema

```go
type JobPosting struct {
    ID               string            `json:"id" validate:"required"`
    Title            string            `json:"title" validate:"required"`
    Company          string            `json:"company" validate:"required"`
    Location         string            `json:"location"`
    Remote           bool              `json:"remote"`
    Salary           *SalaryRange      `json:"salary,omitempty"`
    Description      string            `json:"description"`
    Requirements     []string          `json:"requirements"`
    Skills           []string          `json:"skills"`
    Benefits         []string          `json:"benefits"`
    ExperienceLevel  string            `json:"experience_level"`
    JobType          string            `json:"job_type"`
    PostedDate       time.Time         `json:"posted_date"`
    ApplicationURL   string            `json:"application_url"`
    Metadata         map[string]string `json:"metadata"`
    ProcessedAt      time.Time         `json:"processed_at"`
}

type SalaryRange struct {
    Min      int    `json:"min"`
    Max      int    `json:"max"`
    Currency string `json:"currency"`
    Period   string `json:"period"` // hourly, monthly, yearly
}
```

### API Request/Response Models

```go
type ScrapeRequest struct {
    URL     string         `json:"url" validate:"required,url"`
    Options *ScrapeOptions `json:"options,omitempty"`
}

type ScrapeOptions struct {
    Engine      string        `json:"engine,omitempty"`      // "headed", "raw", "auto"
    Timeout     time.Duration `json:"timeout,omitempty"`     // Request timeout
    LLMProvider string        `json:"llm_provider,omitempty"` // "openai", "claude", "local"
    UserAgent   string        `json:"user_agent,omitempty"`  // Custom user agent
    Proxy       string        `json:"proxy,omitempty"`       // Proxy configuration
}

type ScrapeResponse struct {
    Success    bool        `json:"success"`
    Job        *JobPosting `json:"job,omitempty"`
    Error      string      `json:"error,omitempty"`
    ProcessingTime time.Duration `json:"processing_time"`
    Engine     string      `json:"engine_used"`
    RequestID  string      `json:"request_id"`
}
```

## Configuration Structure

```go
type Config struct {
    Server struct {
        Port         int           `yaml:"port" default:"8080"`
        Host         string        `yaml:"host" default:"0.0.0.0"`
        ReadTimeout  time.Duration `yaml:"read_timeout" default:"30s"`
        WriteTimeout time.Duration `yaml:"write_timeout" default:"30s"`
        IdleTimeout  time.Duration `yaml:"idle_timeout" default:"60s"`
    } `yaml:"server"`
    
    Workers struct {
        PoolSize     int           `yaml:"pool_size" default:"10"`
        QueueSize    int           `yaml:"queue_size" default:"100"`
        RateLimit    int           `yaml:"rate_limit" default:"60"` // requests per minute
        Timeout      time.Duration `yaml:"timeout" default:"30s"`
        MaxRetries   int           `yaml:"max_retries" default:"3"`
    } `yaml:"workers"`
    
    LLM struct {
        Provider     string `yaml:"provider" default:"openai"`
        APIKey       string `yaml:"api_key"`
        Model        string `yaml:"model" default:"gpt-3.5-turbo"`
        MaxTokens    int    `yaml:"max_tokens" default:"4096"`
        Temperature  float32 `yaml:"temperature" default:"0.1"`
        Timeout      time.Duration `yaml:"timeout" default:"30s"`
    } `yaml:"llm"`
    
    Scraper struct {
        UserAgent       string   `yaml:"user_agent"`
        Proxies         []string `yaml:"proxies"`
        MaxRetries      int      `yaml:"max_retries" default:"3"`
        RequestTimeout  time.Duration `yaml:"request_timeout" default:"30s"`
        HeadlessMode    bool     `yaml:"headless_mode" default:"true"`
        StealthMode     bool     `yaml:"stealth_mode" default:"true"`
    } `yaml:"scraper"`
    
    Logging struct {
        Level  string `yaml:"level" default:"info"`
        Format string `yaml:"format" default:"json"`
        Output string `yaml:"output" default:"stdout"`
    } `yaml:"logging"`
}
```

## API Endpoints

### Main Endpoints

```http
POST /api/v1/scrape
Content-Type: application/json

{
    "url": "https://jobs.example.com/posting/123",
    "options": {
        "engine": "headed",
        "timeout": "30s",
        "llm_provider": "openai"
    }
}
```

### Health & Monitoring

```http
GET /health              # Health check
GET /health/ready        # Readiness probe
GET /health/live         # Liveness probe
GET /metrics            # Prometheus metrics
GET /status             # Service status
```

## Implementation Phases

### Phase 1: Foundation (Week 1) ✅ COMPLETE
- [x] Project structure setup
- [x] Echo API framework configuration
- [x] Basic health check endpoints
- [x] Configuration management
- [x] Logging setup
- [x] Basic request/response models

**Deliverables:**
- Running Echo server with health endpoints
- Configuration loading from YAML/ENV
- Structured logging implementation
- Basic API documentation

### Phase 2: Core Scraping Engine (Week 2) ✅ COMPLETE
- [x] Rod + stealth plugin integration
- [x] Browser management and pool
- [x] Basic HTML extraction
- [x] Error handling and retries
- [x] Timeout management

**Deliverables:**
- Working Rod-based scraper
- Browser instance management
- Basic job data extraction
- Comprehensive error handling

### Phase 3: Worker Pool & Rate Limiting (Week 3) ✅ COMPLETE
- [x] Goroutine-based worker pool
- [x] Job queue implementation
- [x] Rate limiting per domain
- [x] Circuit breaker pattern
- [x] Request timeout handling

**Deliverables:**
- Concurrent request processing
- Rate limiting implementation
- Queue management system
- Performance monitoring

### Phase 4: LLM Integration (Week 4) ✅ COMPLETE
- [x] Claude API integration
- [x] Prompt engineering for job extraction
- [x] Response parsing and validation
- [x] Cost optimization strategies

**Deliverables:**
- LLM provider abstraction
- Job data extraction prompts
- Structured response parsing
- Error handling for LLM failures

### Phase 5: Raw HTTP Engine (Week 5)
- [ ] Colly-based scraper implementation
- [ ] HTTP client with custom headers
- [ ] Fallback mechanism
- [ ] Auto-detection logic
- [ ] Performance comparison

**Deliverables:**
- Alternative scraping engine
- Automatic engine selection
- Performance benchmarks
- Fallback strategies

### Phase 6: Post-Processing Pipeline (Week 6)
- [ ] HTML cleanup and sanitization
- [ ] Text extraction and normalization
- [ ] Data validation and schema compliance
- [ ] Duplicate detection
- [ ] Content summarization

**Deliverables:**
- Data processing pipeline
- Validation rules
- Clean, structured output
- Quality assurance metrics

### Phase 7: Testing & Optimization (Week 7)
- [ ] Unit tests for all components
- [ ] Integration tests
- [ ] Load testing
- [ ] Performance optimization
- [ ] Documentation completion

**Deliverables:**
- Comprehensive test suite
- Performance benchmarks
- Complete documentation
- Deployment guides

## Key Features

### Auto-Detection Engine Selection
- Analyze target website characteristics
- Choose optimal scraping strategy
- Fallback mechanisms for failures
- Performance-based engine switching

### Resilience & Error Handling
- Comprehensive retry logic
- Circuit breaker implementation
- Graceful degradation
- Error classification and reporting

### Performance Optimization
- Concurrent processing with resource limits
- Memory-efficient browser management
- Request caching strategies
- Connection pooling

### Security & Compliance
- Input validation and sanitization
- Rate limiting and abuse prevention
- Secure credential management
- GDPR compliance considerations

### Monitoring & Observability
- Structured logging with correlation IDs
- Prometheus metrics integration
- Request tracing
- Performance profiling

## Development Guidelines

### Code Standards
- Follow Go best practices and conventions
- Use dependency injection for testability
- Implement comprehensive error handling
- Write clear, self-documenting code

### Testing Strategy
- Unit tests for all business logic
- Integration tests for external dependencies
- Load tests for performance validation
- Contract tests for API endpoints

### Security Considerations
- Validate all input parameters
- Implement request size limits
- Use secure HTTP clients
- Protect against common vulnerabilities

### Performance Targets
- Response time: < 30 seconds for complex pages
- Throughput: 100+ requests per minute
- Memory usage: < 1GB under normal load
- CPU usage: < 80% under peak load

## Deployment Strategy

### Containerization
- Multi-stage Docker builds
- Minimal base images for security
- Health check integration
- Resource limit configuration

### Orchestration
- Kubernetes deployment manifests
- Horizontal pod autoscaling
- Load balancer configuration
- Monitoring and alerting setup

### Environment Management
- Development, staging, production configs
- Secret management integration
- Feature flag support
- Blue-green deployment strategy

## Future Enhancements

### Scalability Improvements
- Distributed worker pools
- Message queue integration
- Microservice decomposition
- Database integration for caching

### Feature Extensions
- Batch processing support
- Webhook notifications
- Custom extraction rules
- Multi-language support

### AI/ML Enhancements
- Custom model fine-tuning
- Advanced NLP processing
- Automated quality scoring
- Trend analysis capabilities

---

This implementation plan serves as a living document that will be updated as the project evolves. Regular reviews and adjustments will ensure the project stays on track and meets the requirements of the Letraz ecosystem. 