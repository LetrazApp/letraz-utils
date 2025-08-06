package config

import (
	"os"
	"regexp"
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
		MaxTokens   int           `yaml:"max_tokens" default:"8192"`
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

	BrowserPool struct {
		MaxInstances       int           `yaml:"max_instances" default:"5"`
		MaxIdleTime        time.Duration `yaml:"max_idle_time" default:"5m"`
		AcquisitionTimeout time.Duration `yaml:"acquisition_timeout" default:"30s"`
		CleanupInterval    time.Duration `yaml:"cleanup_interval" default:"5m"`
		MaxBrowsers        int           `yaml:"max_browsers" default:"5"`
		MinBrowsers        int           `yaml:"min_browsers" default:"2"`
	} `yaml:"browser_pool"`

	Firecrawl struct {
		APIKey     string        `yaml:"api_key"`
		APIURL     string        `yaml:"api_url" default:"https://api.firecrawl.dev"`
		Version    string        `yaml:"version" default:"v1"`
		Timeout    time.Duration `yaml:"timeout" default:"60s"`
		MaxRetries int           `yaml:"max_retries" default:"3"`
		Formats    []string      `yaml:"formats" default:"markdown"`
	} `yaml:"firecrawl"`

	BrightData struct {
		APIKey     string        `yaml:"api_key"`
		BaseURL    string        `yaml:"base_url" default:"https://api.brightdata.com"`
		DatasetID  string        `yaml:"dataset_id" default:"gd_lpfll7v5hcqtkxl6l"`
		Timeout    time.Duration `yaml:"timeout" default:"60s"`
		MaxRetries int           `yaml:"max_retries" default:"3"`
	} `yaml:"brightdata"`

	Logging struct {
		Level  string `yaml:"level" default:"info"`
		Format string `yaml:"format" default:"json"`
		Output string `yaml:"output" default:"stdout"`

		// New logging configuration
		Adapters []struct {
			Name    string                 `yaml:"name"`
			Type    string                 `yaml:"type"`
			Enabled bool                   `yaml:"enabled"`
			Options map[string]interface{} `yaml:"options"`
		} `yaml:"adapters"`
	} `yaml:"logging"`

	Redis struct {
		URL      string        `yaml:"url" default:"redis://localhost:6379"`
		Password string        `yaml:"password"`
		DB       int           `yaml:"db" default:"0"`
		Timeout  time.Duration `yaml:"timeout" default:"5s"`
	} `yaml:"redis"`

	DigitalOcean struct {
		Spaces struct {
			BucketURL       string `yaml:"bucket_url"`
			CDNEndpoint     string `yaml:"cdn_endpoint"`
			AccessKeyID     string `yaml:"access_key_id"`
			AccessKeySecret string `yaml:"access_key_secret"`
			Region          string `yaml:"region" default:"blr1"`
			BucketName      string `yaml:"bucket_name" default:"letraz-all-purpose"`
		} `yaml:"spaces"`
	} `yaml:"digitalocean"`

	Resume struct {
		Client struct {
			BaseURL      string `yaml:"base_url" default:"http://localhost:3000"`
			PreviewURL   string `yaml:"preview_url" default:"http://localhost:3000/admin/resumes"`
			PreviewToken string `yaml:"preview_token"`
		} `yaml:"client"`
	} `yaml:"resume"`

	Callback struct {
		ServerAddress string        `yaml:"server_address"`
		Timeout       time.Duration `yaml:"timeout" default:"30s"`
		MaxRetries    int           `yaml:"max_retries" default:"3"`
		Enabled       bool          `yaml:"enabled" default:"true"`
	} `yaml:"callback"`
}

// expandEnvVars expands environment variables in a string using ${VAR} or $VAR syntax
func expandEnvVars(s string) string {
	// Expand ${VAR} syntax
	re := regexp.MustCompile(`\$\{([^}]+)\}`)
	s = re.ReplaceAllStringFunc(s, func(match string) string {
		varName := match[2 : len(match)-1] // Remove ${ and }
		if val := os.Getenv(varName); val != "" {
			return val
		}
		return match // Return original if env var not found
	})

	// Expand $VAR syntax (but avoid replacing ${VAR} that was already processed)
	re2 := regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)
	s = re2.ReplaceAllStringFunc(s, func(match string) string {
		varName := match[1:] // Remove $
		if val := os.Getenv(varName); val != "" {
			return val
		}
		return match // Return original if env var not found
	})

	return s
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
	config.LLM.MaxTokens = 8192
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

	config.BrowserPool.MaxInstances = 5
	config.BrowserPool.MaxIdleTime = 5 * time.Minute
	config.BrowserPool.AcquisitionTimeout = 30 * time.Second
	config.BrowserPool.CleanupInterval = 5 * time.Minute
	config.BrowserPool.MaxBrowsers = 5
	config.BrowserPool.MinBrowsers = 2

	config.Firecrawl.MaxRetries = 3
	config.Firecrawl.Timeout = 60 * time.Second
	config.Firecrawl.Formats = []string{"markdown"}

	config.Logging.Level = "warn"
	config.Logging.Format = "json"
	config.Logging.Output = "stdout"

	config.Redis.URL = "redis://localhost:6379"
	config.Redis.DB = 0
	config.Redis.Timeout = 5 * time.Second

	config.Callback.Timeout = 30 * time.Second
	config.Callback.MaxRetries = 3
	config.Callback.Enabled = true

	// Load from YAML file if it exists
	if configPath != "" {
		if data, err := os.ReadFile(configPath); err == nil {
			// Expand environment variables in the YAML content
			yamlContent := expandEnvVars(string(data))

			if err := yaml.Unmarshal([]byte(yamlContent), config); err != nil {
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

	// Handle Betterstack adapter enabled/disabled via environment variable
	if betterstackEnabled := os.Getenv("BETTERSTACK_ENABLED"); betterstackEnabled != "" {
		enabled := betterstackEnabled == "true" || betterstackEnabled == "1"

		// Find and update the Betterstack adapter
		for i := range c.Logging.Adapters {
			if c.Logging.Adapters[i].Name == "betterstack" || c.Logging.Adapters[i].Type == "betterstack" {
				c.Logging.Adapters[i].Enabled = enabled
				break
			}
		}
	}

	// DigitalOcean Spaces configuration
	if bucketURL := os.Getenv("BUCKET_URL"); bucketURL != "" {
		c.DigitalOcean.Spaces.BucketURL = bucketURL
	}

	if cdnEndpoint := os.Getenv("BUCKET_CDN_ENDPOINT"); cdnEndpoint != "" {
		c.DigitalOcean.Spaces.CDNEndpoint = cdnEndpoint
	}

	if accessKeyID := os.Getenv("BUCKET_ACCESS_KEY_ID"); accessKeyID != "" {
		c.DigitalOcean.Spaces.AccessKeyID = accessKeyID
	}

	if accessKeySecret := os.Getenv("BUCKET_ACCESS_KEY_SECRET"); accessKeySecret != "" {
		c.DigitalOcean.Spaces.AccessKeySecret = accessKeySecret
	}

	if region := os.Getenv("BUCKET_REGION"); region != "" {
		c.DigitalOcean.Spaces.Region = region
	}

	if bucketName := os.Getenv("BUCKET_NAME"); bucketName != "" {
		c.DigitalOcean.Spaces.BucketName = bucketName
	}

	// Resume client configuration
	if clientBaseURL := os.Getenv("RESUME_CLIENT_BASE_URL"); clientBaseURL != "" {
		c.Resume.Client.BaseURL = clientBaseURL
	}

	if previewURL := os.Getenv("RESUME_PREVIEW_URL"); previewURL != "" {
		c.Resume.Client.PreviewURL = previewURL
	}

	if previewToken := os.Getenv("RESUME_PREVIEW_TOKEN"); previewToken != "" {
		c.Resume.Client.PreviewToken = previewToken
	}

	// Callback configuration
	if callbackServerAddr := os.Getenv("CALLBACK_SERVER_ADDRESS"); callbackServerAddr != "" {
		c.Callback.ServerAddress = callbackServerAddr
	}

	if callbackTimeout := os.Getenv("CALLBACK_TIMEOUT"); callbackTimeout != "" {
		if timeout, err := time.ParseDuration(callbackTimeout); err == nil {
			c.Callback.Timeout = timeout
		}
	}

	if callbackMaxRetries := os.Getenv("CALLBACK_MAX_RETRIES"); callbackMaxRetries != "" {
		if retries, err := strconv.Atoi(callbackMaxRetries); err == nil {
			c.Callback.MaxRetries = retries
		}
	}

	if callbackEnabled := os.Getenv("CALLBACK_ENABLED"); callbackEnabled != "" {
		c.Callback.Enabled = callbackEnabled == "true" || callbackEnabled == "1"
	}

	// Browser pool configuration
	if maxInstances := os.Getenv("BROWSER_POOL_MAX_INSTANCES"); maxInstances != "" {
		if instances, err := strconv.Atoi(maxInstances); err == nil {
			c.BrowserPool.MaxInstances = instances
		}
	}

	if maxIdleTime := os.Getenv("BROWSER_POOL_MAX_IDLE_TIME"); maxIdleTime != "" {
		if duration, err := time.ParseDuration(maxIdleTime); err == nil {
			c.BrowserPool.MaxIdleTime = duration
		}
	}

	if acquisitionTimeout := os.Getenv("BROWSER_POOL_ACQUISITION_TIMEOUT"); acquisitionTimeout != "" {
		if duration, err := time.ParseDuration(acquisitionTimeout); err == nil {
			c.BrowserPool.AcquisitionTimeout = duration
		}
	}

	if cleanupInterval := os.Getenv("BROWSER_POOL_CLEANUP_INTERVAL"); cleanupInterval != "" {
		if duration, err := time.ParseDuration(cleanupInterval); err == nil {
			c.BrowserPool.CleanupInterval = duration
		}
	}

	if maxBrowsers := os.Getenv("BROWSER_POOL_MAX_BROWSERS"); maxBrowsers != "" {
		if browsers, err := strconv.Atoi(maxBrowsers); err == nil {
			c.BrowserPool.MaxBrowsers = browsers
		}
	}

	if minBrowsers := os.Getenv("BROWSER_POOL_MIN_BROWSERS"); minBrowsers != "" {
		if browsers, err := strconv.Atoi(minBrowsers); err == nil {
			c.BrowserPool.MinBrowsers = browsers
		}
	}

	// Handle additional logging adapter options via environment variables
	c.loadLoggingAdapterEnvVars()
}

// loadLoggingAdapterEnvVars loads environment variables for logging adapters
func (c *Config) loadLoggingAdapterEnvVars() {
	for i := range c.Logging.Adapters {
		adapter := &c.Logging.Adapters[i]

		switch adapter.Type {
		case "betterstack":
			if token := os.Getenv("BETTERSTACK_SOURCE_TOKEN"); token != "" {
				if adapter.Options == nil {
					adapter.Options = make(map[string]interface{})
				}
				adapter.Options["source_token"] = token
			}

			if endpoint := os.Getenv("BETTERSTACK_ENDPOINT"); endpoint != "" {
				if adapter.Options == nil {
					adapter.Options = make(map[string]interface{})
				}
				adapter.Options["endpoint"] = endpoint
			}

			if batchSize := os.Getenv("BETTERSTACK_BATCH_SIZE"); batchSize != "" {
				if size, err := strconv.Atoi(batchSize); err == nil {
					if adapter.Options == nil {
						adapter.Options = make(map[string]interface{})
					}
					adapter.Options["batch_size"] = size
				}
			}

			if flushInterval := os.Getenv("BETTERSTACK_FLUSH_INTERVAL"); flushInterval != "" {
				if adapter.Options == nil {
					adapter.Options = make(map[string]interface{})
				}
				adapter.Options["flush_interval"] = flushInterval
			}

			if maxRetries := os.Getenv("BETTERSTACK_MAX_RETRIES"); maxRetries != "" {
				if retries, err := strconv.Atoi(maxRetries); err == nil {
					if adapter.Options == nil {
						adapter.Options = make(map[string]interface{})
					}
					adapter.Options["max_retries"] = retries
				}
			}

			if timeout := os.Getenv("BETTERSTACK_TIMEOUT"); timeout != "" {
				if adapter.Options == nil {
					adapter.Options = make(map[string]interface{})
				}
				adapter.Options["timeout"] = timeout
			}

			if userAgent := os.Getenv("BETTERSTACK_USER_AGENT"); userAgent != "" {
				if adapter.Options == nil {
					adapter.Options = make(map[string]interface{})
				}
				adapter.Options["user_agent"] = userAgent
			}
		}
	}
}
