package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"letraz-utils/internal/config"
	"letraz-utils/internal/logging"
)

// RedisClient wraps the Redis client with conversation history management
type RedisClient struct {
	client *redis.Client
	config *config.Config
	logger logging.Logger
}

// ConversationEntry represents a single conversation entry
type ConversationEntry struct {
	ID        string                 `json:"id"`
	Role      string                 `json:"role"` // "user" or "assistant"
	Content   string                 `json:"content"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ConversationHistory represents the complete conversation history for a resume
type ConversationHistory struct {
	ThreadID  string              `json:"thread_id"`
	ResumeID  string              `json:"resume_id"`
	Entries   []ConversationEntry `json:"entries"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

// NewRedisClient creates a new Redis client instance
func NewRedisClient(cfg *config.Config) *RedisClient {
	// Parse Redis URL
	opts, err := redis.ParseURL(cfg.Redis.URL)
	if err != nil {
		// Fallback to default configuration
		opts = &redis.Options{
			Addr:     "localhost:6379",
			Password: "",
			DB:       0,
		}
	}

	// Configure timeouts
	opts.DialTimeout = 5 * time.Second
	opts.ReadTimeout = 3 * time.Second
	opts.WriteTimeout = 3 * time.Second

	client := redis.NewClient(opts)

	return &RedisClient{
		client: client,
		config: cfg,
		logger: logging.GetGlobalLogger(),
	}
}

// Ping tests the Redis connection
func (r *RedisClient) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// Close closes the Redis connection
func (r *RedisClient) Close() error {
	return r.client.Close()
}

// CreateConversationThread creates a new conversation thread for a resume
func (r *RedisClient) CreateConversationThread(ctx context.Context, resumeID string) error {
	threadKey := r.getThreadKey(resumeID)

	// Check if thread already exists
	exists, err := r.client.Exists(ctx, threadKey).Result()
	if err != nil {
		return fmt.Errorf("failed to check if thread exists: %w", err)
	}

	if exists > 0 {
		// Thread already exists, just update the timestamp
		history, err := r.GetConversationHistory(ctx, resumeID)
		if err != nil {
			return fmt.Errorf("failed to get existing conversation history: %w", err)
		}
		history.UpdatedAt = time.Now()

		historyJSON, err := json.Marshal(history)
		if err != nil {
			return fmt.Errorf("failed to marshal conversation history: %w", err)
		}

		err = r.client.Set(ctx, threadKey, historyJSON, 24*time.Hour).Err()
		if err != nil {
			return fmt.Errorf("failed to update conversation thread: %w", err)
		}
		return nil
	}

	// Create new thread
	history := &ConversationHistory{
		ThreadID:  GenerateRequestID(),
		ResumeID:  resumeID,
		Entries:   []ConversationEntry{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	historyJSON, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("failed to marshal conversation history: %w", err)
	}

	// Store with 24-hour expiration
	err = r.client.Set(ctx, threadKey, historyJSON, 24*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to create conversation thread: %w", err)
	}

	return nil
}

// AddConversationEntry adds a new entry to the conversation history
func (r *RedisClient) AddConversationEntry(ctx context.Context, resumeID string, entry ConversationEntry) error {
	// Get current history
	history, err := r.GetConversationHistory(ctx, resumeID)
	if err != nil {
		// If thread doesn't exist, create it first
		if err := r.CreateConversationThread(ctx, resumeID); err != nil {
			return fmt.Errorf("failed to create conversation thread: %w", err)
		}
		history, err = r.GetConversationHistory(ctx, resumeID)
		if err != nil {
			return fmt.Errorf("failed to get conversation history after creation: %w", err)
		}
	}

	// Add new entry
	entry.ID = GenerateRequestID()
	entry.Timestamp = time.Now()
	history.Entries = append(history.Entries, entry)
	history.UpdatedAt = time.Now()

	// Keep only last 50 entries to manage memory
	if len(history.Entries) > 50 {
		history.Entries = history.Entries[len(history.Entries)-50:]
	}

	// Save updated history
	threadKey := r.getThreadKey(resumeID)
	historyJSON, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("failed to marshal conversation history: %w", err)
	}

	err = r.client.Set(ctx, threadKey, historyJSON, 24*time.Hour).Err()
	if err != nil {
		r.logger.Error("Failed to save conversation entry", map[string]interface{}{
			"resume_id": resumeID,
			"entry_id":  entry.ID,
			"error":     err.Error(),
		})
		return fmt.Errorf("failed to save conversation entry: %w", err)
	}

	return nil
}

// GetConversationHistory retrieves the conversation history for a resume
func (r *RedisClient) GetConversationHistory(ctx context.Context, resumeID string) (*ConversationHistory, error) {
	threadKey := r.getThreadKey(resumeID)

	historyJSON, err := r.client.Get(ctx, threadKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("conversation thread not found for resume %s", resumeID)
		}
		return nil, fmt.Errorf("failed to get conversation history: %w", err)
	}

	var history ConversationHistory
	err = json.Unmarshal([]byte(historyJSON), &history)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal conversation history: %w", err)
	}

	return &history, nil
}

// DeleteConversationThread deletes the conversation thread for a resume
func (r *RedisClient) DeleteConversationThread(ctx context.Context, resumeID string) error {
	threadKey := r.getThreadKey(resumeID)
	return r.client.Del(ctx, threadKey).Err()
}

// getThreadKey generates the Redis key for a conversation thread
func (r *RedisClient) getThreadKey(resumeID string) string {
	return fmt.Sprintf("conversation:resume:%s", resumeID)
}

// IsHealthy checks if Redis is connected and healthy
func (r *RedisClient) IsHealthy(ctx context.Context) error {
	return r.Ping(ctx)
}
