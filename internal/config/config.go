package config

import (
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	Server struct {
		Port         int           `yaml:"port" default:"8080"`
		Host         string        `yaml:"host" default:"0.0.0.0"`
		ReadTimeout  time.Duration `yaml:"read_timeout" default:"30s"`
		WriteTimeout time.Duration `yaml:"write_timeout" default:"30s"`
		IdleTimeout  time.Duration `yaml:"idle_timeout" default:"60s"`
	} `yaml:"server"`

	Workers struct {
		PoolSize   int           `yaml:"pool_size" default:"10"`
		QueueSize  int           `yaml:"queue_size" default:"100"`
		RateLimit  int           `yaml:"rate_limit" default:"60"` // requests per minute
		Timeout    time.Duration `yaml:"timeout" default:"30s"`
		MaxRetries int           `yaml:"max_retries" default:"3"`
	} `yaml:"workers"`

	BackgroundTasks struct {
		MaxConcurrentTasks int           `yaml:"max_concurrent_tasks" default:"50"`
		TaskTimeout        time.Duration `yaml:"task_timeout" default:"300s"`
		CleanupInterval    time.Duration `yaml:"cleanup_interval" default:"1h"`
		MaxTaskAge         time.Duration `yaml:"max_task_age" default:"24h"`
	} `yaml:"background_tasks"`

	LLM struct {
		Provider    string        `yaml:"provider" default:"claude"`
		APIKey      string        `yaml:"api_key"`
		Model       string        `yaml:"model" default:"claude-3-haiku-20240307"`
		MaxTokens   int           `yaml:"max_tokens" default:"4096"`
		Temperature float32       `yaml:"temperature" default:"0.1"`
		Timeout     time.Duration `yaml:"timeout" default:"30s"`
	} `yaml:"llm"`

	Scraper struct {
		UserAgent      string        `yaml:"user_agent"`
		Proxies        []string      `yaml:"proxies"`
		MaxRetries     int           `yaml:"max_retries" default:"3"`
		RequestTimeout time.Duration `yaml:"request_timeout" default:"30s"`
		HeadlessMode   bool          `yaml:"headless_mode" default:"true"`
		StealthMode    bool          `yaml:"stealth_mode" default:"true"`
		Captcha        struct {
			Provider        string        `yaml:"provider" default:"2captcha"`
			APIKey          string        `yaml:"api_key"`
			Timeout         time.Duration `yaml:"timeout" default:"120s"`
			EnableAutoSolve bool          `yaml:"enable_auto_solve" default:"true"`
		} `yaml:"captcha"`
	} `yaml:"scraper"`

	Firecrawl struct {
		APIKey     string        `yaml:"api_key"`
		APIURL     string        `yaml:"api_url" default:"https://api.firecrawl.dev"`
		Version    string        `yaml:"version" default:"v1"`
		Timeout    time.Duration `yaml:"timeout" default:"60s"`
		MaxRetries int           `yaml:"max_retries" default:"3"`
		Formats    []string      `yaml:"formats" default:"markdown"`
	} `yaml:"firecrawl"`

	Logging struct {
		Level  string `yaml:"level" default:"info"`
		Format string `yaml:"format" default:"json"`
		Output string `yaml:"output" default:"stdout"`
	} `yaml:"logging"`

	Redis struct {
		URL      string        `yaml:"url" default:"redis://localhost:6379"`
		Password string        `yaml:"password"`
		DB       int           `yaml:"db" default:"0"`
		Timeout  time.Duration `yaml:"timeout" default:"5s"`
	} `yaml:"redis"`
}

// LoadConfig loads configuration from file and environment variables
func LoadConfig(configPath string) (*Config, error) {
	// Load .env file if it exists (ignore errors if file doesn't exist)
	_ = godotenv.Load()

	config := &Config{}

	// Set defaults
	config.Server.Port = 8080
	config.Server.Host = "0.0.0.0"
	config.Server.ReadTimeout = 30 * time.Second
	config.Server.WriteTimeout = 30 * time.Second
	config.Server.IdleTimeout = 60 * time.Second

	config.Workers.PoolSize = 10
	config.Workers.QueueSize = 100
	config.Workers.RateLimit = 60
	config.Workers.Timeout = 30 * time.Second
	config.Workers.MaxRetries = 3

	config.BackgroundTasks.MaxConcurrentTasks = 50
	config.BackgroundTasks.TaskTimeout = 300 * time.Second
	config.BackgroundTasks.CleanupInterval = 1 * time.Hour
	config.BackgroundTasks.MaxTaskAge = 24 * time.Hour

	config.LLM.Provider = "claude"
	config.LLM.MaxTokens = 4096
	config.LLM.Temperature = 0.1
	config.LLM.Timeout = 120 * time.Second

	config.Scraper.MaxRetries = 3
	config.Scraper.RequestTimeout = 30 * time.Second
	config.Scraper.HeadlessMode = true
	config.Scraper.StealthMode = true
	config.Scraper.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	config.Scraper.Captcha.Provider = "2captcha"
	config.Scraper.Captcha.Timeout = 120 * time.Second
	config.Scraper.Captcha.EnableAutoSolve = true

	config.Firecrawl.MaxRetries = 3
	config.Firecrawl.Timeout = 60 * time.Second
	config.Firecrawl.Formats = []string{"markdown"}

	config.Logging.Level = "warn"
	config.Logging.Format = "json"
	config.Logging.Output = "stdout"

	config.Redis.URL = "redis://localhost:6379"
	config.Redis.DB = 0
	config.Redis.Timeout = 5 * time.Second

	// Load from YAML file if it exists
	if configPath != "" {
		if data, err := os.ReadFile(configPath); err == nil {
			if err := yaml.Unmarshal(data, config); err != nil {
				return nil, err
			}
		}
	}

	// Override with environment variables
	config.loadFromEnv()

	return config, nil
}

// loadFromEnv loads configuration from environment variables
func (c *Config) loadFromEnv() {
	if port := os.Getenv("PORT"); port != "" {
		if p, err := time.ParseDuration(port + "s"); err == nil {
			c.Server.Port = int(p.Seconds())
		}
	}

	if host := os.Getenv("HOST"); host != "" {
		c.Server.Host = host
	}

	if apiKey := os.Getenv("LLM_API_KEY"); apiKey != "" {
		c.LLM.APIKey = apiKey
	}

	if provider := os.Getenv("LLM_PROVIDER"); provider != "" {
		c.LLM.Provider = provider
	}

	if model := os.Getenv("LLM_MODEL"); model != "" {
		c.LLM.Model = model
	}

	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		c.Logging.Level = logLevel
	}

	if logFormat := os.Getenv("LOG_FORMAT"); logFormat != "" {
		c.Logging.Format = logFormat
	}

	if captchaAPIKey := os.Getenv("CAPTCHA_API_KEY"); captchaAPIKey != "" {
		c.Scraper.Captcha.APIKey = captchaAPIKey
	}

	// Also support 2CAPTCHA_API_KEY for compatibility
	if captchaAPIKey := os.Getenv("2CAPTCHA_API_KEY"); captchaAPIKey != "" {
		c.Scraper.Captcha.APIKey = captchaAPIKey
	}

	if firecrawlAPIKey := os.Getenv("FIRECRAWL_API_KEY"); firecrawlAPIKey != "" {
		c.Firecrawl.APIKey = firecrawlAPIKey
	}

	if firecrawlAPIURL := os.Getenv("FIRECRAWL_API_URL"); firecrawlAPIURL != "" {
		c.Firecrawl.APIURL = firecrawlAPIURL
	}

	if firecrawlVersion := os.Getenv("FIRECRAWL_VERSION"); firecrawlVersion != "" {
		c.Firecrawl.Version = firecrawlVersion
	}

	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		c.Redis.URL = redisURL
	}

	if redisPassword := os.Getenv("REDIS_PASSWORD"); redisPassword != "" {
		c.Redis.Password = redisPassword
	}

	if redisDB := os.Getenv("REDIS_DB"); redisDB != "" {
		if db, err := strconv.Atoi(redisDB); err == nil {
			c.Redis.DB = db
		}
	}

	if redisTimeout := os.Getenv("REDIS_TIMEOUT"); redisTimeout != "" {
		if timeout, err := time.ParseDuration(redisTimeout); err == nil {
			c.Redis.Timeout = timeout
		}
	}
}
