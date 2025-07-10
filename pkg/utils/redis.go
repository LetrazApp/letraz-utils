package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"

	"letraz-utils/internal/config"
)

// RedisClient wraps the Redis client with conversation history management
type RedisClient struct {
	client *redis.Client
	config *config.Config
	logger *logrus.Logger
}

// ConversationEntry represents a single conversation entry
type ConversationEntry struct {
	ID        string                 `json:"id"`
	Role      string                 `json:"role"` // "user" or "assistant"
	Content   string                 `json:"content"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ConversationHistory represents the complete conversation history
type ConversationHistory struct {
	ThreadID  string              `json:"thread_id"`
	ResumeID  string              `json:"resume_id"`
	Entries   []ConversationEntry `json:"entries"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

// NewRedisClient creates a new Redis client instance
func NewRedisClient(cfg *config.Config) *RedisClient {
	logger := GetLogger()

	// Parse Redis URL and create client options
	opt, err := redis.ParseURL(cfg.Redis.URL)
	if err != nil {
		logger.WithError(err).Fatal("Failed to parse Redis URL")
	}

	// Set password if provided
	if cfg.Redis.Password != "" {
		opt.Password = cfg.Redis.Password
	}

	// Set database
	opt.DB = cfg.Redis.DB

	// Set timeouts
	opt.DialTimeout = cfg.Redis.Timeout
	opt.ReadTimeout = cfg.Redis.Timeout
	opt.WriteTimeout = cfg.Redis.Timeout

	client := redis.NewClient(opt)

	return &RedisClient{
		client: client,
		config: cfg,
		logger: logger,
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

// CreateConversationThread creates a new conversation thread with the specified resumeID as threadID
func (r *RedisClient) CreateConversationThread(ctx context.Context, resumeID string) error {
	threadKey := r.getThreadKey(resumeID)

	// Check if thread already exists
	exists, err := r.client.Exists(ctx, threadKey).Result()
	if err != nil {
		return fmt.Errorf("failed to check if thread exists: %w", err)
	}

	if exists > 0 {
		r.logger.WithField("resume_id", resumeID).Debug("Conversation thread already exists")
		return nil
	}

	// Create new conversation history
	history := ConversationHistory{
		ThreadID:  resumeID,
		ResumeID:  resumeID,
		Entries:   []ConversationEntry{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Store in Redis with expiration (30 days)
	historyJSON, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("failed to marshal conversation history: %w", err)
	}

	err = r.client.Set(ctx, threadKey, historyJSON, 30*24*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to store conversation thread: %w", err)
	}

	r.logger.WithField("resume_id", resumeID).Info("Created new conversation thread")
	return nil
}

// AddConversationEntry adds a new entry to the conversation history
func (r *RedisClient) AddConversationEntry(ctx context.Context, resumeID string, entry ConversationEntry) error {
	threadKey := r.getThreadKey(resumeID)

	// Get existing conversation history
	history, err := r.GetConversationHistory(ctx, resumeID)
	if err != nil {
		// If thread doesn't exist, create it first
		if err := r.CreateConversationThread(ctx, resumeID); err != nil {
			return fmt.Errorf("failed to create conversation thread: %w", err)
		}
		history = &ConversationHistory{
			ThreadID:  resumeID,
			ResumeID:  resumeID,
			Entries:   []ConversationEntry{},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}

	// Set entry metadata
	entry.ID = fmt.Sprintf("%s_%d", resumeID, len(history.Entries)+1)
	entry.Timestamp = time.Now()

	// Add new entry
	history.Entries = append(history.Entries, entry)
	history.UpdatedAt = time.Now()

	// Store updated history
	historyJSON, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("failed to marshal updated conversation history: %w", err)
	}

	err = r.client.Set(ctx, threadKey, historyJSON, 30*24*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to update conversation history: %w", err)
	}

	r.logger.WithFields(logrus.Fields{
		"resume_id":     resumeID,
		"entry_id":      entry.ID,
		"role":          entry.Role,
		"total_entries": len(history.Entries),
	}).Debug("Added conversation entry")

	return nil
}

// GetConversationHistory retrieves the conversation history for a given resumeID
func (r *RedisClient) GetConversationHistory(ctx context.Context, resumeID string) (*ConversationHistory, error) {
	threadKey := r.getThreadKey(resumeID)

	historyJSON, err := r.client.Get(ctx, threadKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("conversation thread not found for resume ID: %s", resumeID)
		}
		return nil, fmt.Errorf("failed to get conversation history: %w", err)
	}

	var history ConversationHistory
	if err := json.Unmarshal([]byte(historyJSON), &history); err != nil {
		return nil, fmt.Errorf("failed to unmarshal conversation history: %w", err)
	}

	return &history, nil
}

// DeleteConversationThread deletes a conversation thread
func (r *RedisClient) DeleteConversationThread(ctx context.Context, resumeID string) error {
	threadKey := r.getThreadKey(resumeID)

	err := r.client.Del(ctx, threadKey).Err()
	if err != nil {
		return fmt.Errorf("failed to delete conversation thread: %w", err)
	}

	r.logger.WithField("resume_id", resumeID).Info("Deleted conversation thread")
	return nil
}

// getThreadKey generates the Redis key for a conversation thread
func (r *RedisClient) getThreadKey(resumeID string) string {
	return fmt.Sprintf("conversation:thread:%s", resumeID)
}

// IsHealthy checks if Redis is healthy and accessible
func (r *RedisClient) IsHealthy(ctx context.Context) error {
	return r.Ping(ctx)
}
