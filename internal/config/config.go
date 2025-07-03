package config

import (
	"os"
	"time"

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

	LLM struct {
		Provider    string        `yaml:"provider" default:"openai"`
		APIKey      string        `yaml:"api_key"`
		Model       string        `yaml:"model" default:"gpt-3.5-turbo"`
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
	} `yaml:"scraper"`

	Logging struct {
		Level  string `yaml:"level" default:"info"`
		Format string `yaml:"format" default:"json"`
		Output string `yaml:"output" default:"stdout"`
	} `yaml:"logging"`
}

// LoadConfig loads configuration from file and environment variables
func LoadConfig(configPath string) (*Config, error) {
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

	config.LLM.Provider = "openai"
	config.LLM.Model = "gpt-3.5-turbo"
	config.LLM.MaxTokens = 4096
	config.LLM.Temperature = 0.1
	config.LLM.Timeout = 30 * time.Second

	config.Scraper.MaxRetries = 3
	config.Scraper.RequestTimeout = 30 * time.Second
	config.Scraper.HeadlessMode = true
	config.Scraper.StealthMode = true
	config.Scraper.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	config.Logging.Level = "warn"
	config.Logging.Format = "json"
	config.Logging.Output = "stdout"

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
}
