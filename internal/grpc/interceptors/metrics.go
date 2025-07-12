package interceptors

import (
	"context"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"letraz-utils/pkg/utils"
)

// MetricsData holds metrics information for gRPC calls
type MetricsData struct {
	RequestCount    int64         `json:"request_count"`
	SuccessCount    int64         `json:"success_count"`
	ErrorCount      int64         `json:"error_count"`
	TotalDuration   time.Duration `json:"total_duration"`
	AverageDuration time.Duration `json:"average_duration"`
	LastUpdated     time.Time     `json:"last_updated"`
}

// MethodMetrics holds metrics for a specific gRPC method
type MethodMetrics struct {
	Method  string      `json:"method"`
	Metrics MetricsData `json:"metrics"`
	mu      sync.RWMutex
}

// MetricsCollector collects and stores gRPC metrics
type MetricsCollector struct {
	methods map[string]*MethodMetrics
	mu      sync.RWMutex
}

// Global metrics collector instance
var (
	globalMetricsCollector *MetricsCollector
	metricsOnce            sync.Once
)

// GetMetricsCollector returns the global metrics collector instance
func GetMetricsCollector() *MetricsCollector {
	metricsOnce.Do(func() {
		globalMetricsCollector = &MetricsCollector{
			methods: make(map[string]*MethodMetrics),
		}
	})
	return globalMetricsCollector
}

// RecordMetrics records metrics for a gRPC method call
func (c *MetricsCollector) RecordMetrics(method string, duration time.Duration, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	methodMetrics, exists := c.methods[method]
	if !exists {
		methodMetrics = &MethodMetrics{
			Method: method,
			Metrics: MetricsData{
				LastUpdated: time.Now(),
			},
		}
		c.methods[method] = methodMetrics
	}

	methodMetrics.mu.Lock()
	defer methodMetrics.mu.Unlock()

	// Update metrics
	methodMetrics.Metrics.RequestCount++
	methodMetrics.Metrics.TotalDuration += duration
	methodMetrics.Metrics.AverageDuration = methodMetrics.Metrics.TotalDuration / time.Duration(methodMetrics.Metrics.RequestCount)
	methodMetrics.Metrics.LastUpdated = time.Now()

	if err != nil {
		methodMetrics.Metrics.ErrorCount++
	} else {
		methodMetrics.Metrics.SuccessCount++
	}
}

// GetMethodMetrics returns metrics for a specific method
func (c *MetricsCollector) GetMethodMetrics(method string) *MethodMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if methodMetrics, exists := c.methods[method]; exists {
		// Return a copy to avoid race conditions
		methodMetrics.mu.RLock()
		defer methodMetrics.mu.RUnlock()

		return &MethodMetrics{
			Method:  methodMetrics.Method,
			Metrics: methodMetrics.Metrics,
		}
	}

	return nil
}

// GetAllMetrics returns all collected metrics
func (c *MetricsCollector) GetAllMetrics() map[string]*MethodMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*MethodMetrics)
	for method, methodMetrics := range c.methods {
		methodMetrics.mu.RLock()
		result[method] = &MethodMetrics{
			Method:  methodMetrics.Method,
			Metrics: methodMetrics.Metrics,
		}
		methodMetrics.mu.RUnlock()
	}

	return result
}

// Reset clears all metrics
func (c *MetricsCollector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.methods = make(map[string]*MethodMetrics)
}

// MetricsInterceptor returns a gRPC unary interceptor that collects metrics
func MetricsInterceptor() grpc.UnaryServerInterceptor {
	collector := GetMetricsCollector()

	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		startTime := time.Now()

		// Call the actual handler
		resp, err := handler(ctx, req)

		// Record metrics
		duration := time.Since(startTime)
		collector.RecordMetrics(info.FullMethod, duration, err)

		// Log metrics for monitoring
		logger := utils.GetLogger()
		statusCode := codes.OK
		if err != nil {
			if s, ok := status.FromError(err); ok {
				statusCode = s.Code()
			} else {
				statusCode = codes.Internal
			}
		}

		logger.WithFields(map[string]interface{}{
			"method":      info.FullMethod,
			"duration_ms": duration.Milliseconds(),
			"status_code": statusCode.String(),
			"type":        "grpc_metrics",
		}).Debug("gRPC method metrics recorded")

		return resp, err
	}
}

// StreamMetricsInterceptor returns a gRPC streaming interceptor that collects metrics
func StreamMetricsInterceptor() grpc.StreamServerInterceptor {
	collector := GetMetricsCollector()

	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		startTime := time.Now()

		// Call the actual handler
		err := handler(srv, ss)

		// Record metrics
		duration := time.Since(startTime)
		collector.RecordMetrics(info.FullMethod, duration, err)

		// Log metrics for monitoring
		logger := utils.GetLogger()
		statusCode := codes.OK
		if err != nil {
			if s, ok := status.FromError(err); ok {
				statusCode = s.Code()
			} else {
				statusCode = codes.Internal
			}
		}

		logger.WithFields(map[string]interface{}{
			"method":      info.FullMethod,
			"duration_ms": duration.Milliseconds(),
			"status_code": statusCode.String(),
			"type":        "grpc_stream_metrics",
		}).Debug("gRPC stream metrics recorded")

		return err
	}
}

// LogMetricsSummary logs a summary of all collected metrics
func LogMetricsSummary() {
	collector := GetMetricsCollector()
	allMetrics := collector.GetAllMetrics()

	logger := utils.GetLogger()

	for method, methodMetrics := range allMetrics {
		successRate := float64(0)
		if methodMetrics.Metrics.RequestCount > 0 {
			successRate = float64(methodMetrics.Metrics.SuccessCount) / float64(methodMetrics.Metrics.RequestCount) * 100
		}

		logger.WithFields(map[string]interface{}{
			"method":           method,
			"request_count":    methodMetrics.Metrics.RequestCount,
			"success_count":    methodMetrics.Metrics.SuccessCount,
			"error_count":      methodMetrics.Metrics.ErrorCount,
			"success_rate":     successRate,
			"average_duration": methodMetrics.Metrics.AverageDuration,
			"type":             "grpc_metrics_summary",
		}).Info("gRPC method metrics summary")
	}
}

// StartMetricsReporting starts a goroutine that periodically logs metrics
func StartMetricsReporting(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				LogMetricsSummary()
			case <-ctx.Done():
				return
			}
		}
	}()
}
