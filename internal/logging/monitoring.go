package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"letraz-utils/internal/logging/types"
)

// MonitoringService manages health monitoring and metrics collection for logging adapters
type MonitoringService struct {
	logger           Logger
	adapters         map[string]types.LogAdapter
	healthCheckers   map[string]*AdapterHealthChecker
	metricsCollector *MetricsCollector
	alertManager     *AlertManager
	mu               sync.RWMutex
	config           MonitoringConfig
	stopCh           chan struct{}
	httpServer       *http.Server
}

// MonitoringConfig configures the monitoring service
type MonitoringConfig struct {
	Enabled             bool          `yaml:"enabled"`
	Port                int           `yaml:"port"`
	HealthCheckInterval time.Duration `yaml:"health_check_interval"`
	MetricsInterval     time.Duration `yaml:"metrics_interval"`
	RetentionPeriod     time.Duration `yaml:"retention_period"`
	AlertThresholds     struct {
		ErrorRate      float64       `yaml:"error_rate"`      // Error rate threshold (%)
		ResponseTime   time.Duration `yaml:"response_time"`   // Response time threshold
		CircuitBreaker int           `yaml:"circuit_breaker"` // Circuit breaker trips threshold
	} `yaml:"alert_thresholds"`
}

// AdapterHealthChecker monitors the health of a specific adapter
type AdapterHealthChecker struct {
	name                string
	adapter             types.LogAdapter
	lastHealthCheck     time.Time
	isHealthy           bool
	consecutiveFailures int
	healthHistory       []HealthCheckResult
	mu                  sync.RWMutex
}

// HealthCheckResult represents the result of a health check
type HealthCheckResult struct {
	Timestamp time.Time     `json:"timestamp"`
	Healthy   bool          `json:"healthy"`
	Error     string        `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
}

// MetricsCollector collects and aggregates metrics from adapters
type MetricsCollector struct {
	metrics map[string]*AdapterMetrics
	mu      sync.RWMutex
}

// AdapterMetrics stores metrics for a specific adapter
type AdapterMetrics struct {
	Name                string                 `json:"name"`
	TotalRequests       int64                  `json:"total_requests"`
	SuccessfulRequests  int64                  `json:"successful_requests"`
	FailedRequests      int64                  `json:"failed_requests"`
	ErrorRate           float64                `json:"error_rate"`
	AverageResponseTime time.Duration          `json:"average_response_time"`
	CircuitBreakerTrips int64                  `json:"circuit_breaker_trips"`
	LastActivity        time.Time              `json:"last_activity"`
	CustomMetrics       map[string]interface{} `json:"custom_metrics,omitempty"`
}

// AlertManager manages alerts for adapter health issues
type AlertManager struct {
	config        MonitoringConfig
	activeAlerts  map[string]*Alert
	alertHistory  []Alert
	mu            sync.RWMutex
	alertHandlers []AlertHandler
}

// Alert represents a health alert
type Alert struct {
	ID          string        `json:"id"`
	AdapterName string        `json:"adapter_name"`
	Type        AlertType     `json:"type"`
	Severity    AlertSeverity `json:"severity"`
	Message     string        `json:"message"`
	Timestamp   time.Time     `json:"timestamp"`
	Resolved    bool          `json:"resolved"`
	ResolvedAt  *time.Time    `json:"resolved_at,omitempty"`
}

// AlertType represents the type of alert
type AlertType string

const (
	AlertTypeHealthCheck    AlertType = "health_check"
	AlertTypeErrorRate      AlertType = "error_rate"
	AlertTypeResponseTime   AlertType = "response_time"
	AlertTypeCircuitBreaker AlertType = "circuit_breaker"
)

// AlertSeverity represents the severity of an alert
type AlertSeverity string

const (
	AlertSeverityCritical AlertSeverity = "critical"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityInfo     AlertSeverity = "info"
)

// AlertHandler handles alert notifications
type AlertHandler interface {
	HandleAlert(alert Alert) error
}

// NewMonitoringService creates a new monitoring service
func NewMonitoringService(logger Logger, config MonitoringConfig) *MonitoringService {
	// Set defaults
	if config.Port == 0 {
		config.Port = 8081
	}
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 30 * time.Second
	}
	if config.MetricsInterval == 0 {
		config.MetricsInterval = 60 * time.Second
	}
	if config.RetentionPeriod == 0 {
		config.RetentionPeriod = 24 * time.Hour
	}
	if config.AlertThresholds.ErrorRate == 0 {
		config.AlertThresholds.ErrorRate = 5.0 // 5% error rate
	}
	if config.AlertThresholds.ResponseTime == 0 {
		config.AlertThresholds.ResponseTime = 5 * time.Second
	}
	if config.AlertThresholds.CircuitBreaker == 0 {
		config.AlertThresholds.CircuitBreaker = 5
	}

	return &MonitoringService{
		logger:         logger,
		adapters:       make(map[string]types.LogAdapter),
		healthCheckers: make(map[string]*AdapterHealthChecker),
		metricsCollector: &MetricsCollector{
			metrics: make(map[string]*AdapterMetrics),
		},
		alertManager: &AlertManager{
			config:       config,
			activeAlerts: make(map[string]*Alert),
			alertHistory: make([]Alert, 0),
		},
		config: config,
		stopCh: make(chan struct{}),
	}
}

// Start starts the monitoring service
func (ms *MonitoringService) Start() error {
	if !ms.config.Enabled {
		return nil
	}

	// Start HTTP server for health and metrics endpoints
	if err := ms.startHTTPServer(); err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	// Start background monitoring tasks
	go ms.healthCheckLoop()
	go ms.metricsCollectionLoop()
	go ms.alertProcessingLoop()

	ms.logger.Info("Monitoring service started", map[string]interface{}{
		"port":                  ms.config.Port,
		"health_check_interval": ms.config.HealthCheckInterval.String(),
		"metrics_interval":      ms.config.MetricsInterval.String(),
	})

	return nil
}

// Stop stops the monitoring service
func (ms *MonitoringService) Stop() error {
	close(ms.stopCh)

	// Shutdown HTTP server
	if ms.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return ms.httpServer.Shutdown(ctx)
	}

	return nil
}

// AddAdapter adds an adapter to monitoring
func (ms *MonitoringService) AddAdapter(adapter types.LogAdapter) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	name := adapter.Name()
	ms.adapters[name] = adapter
	ms.healthCheckers[name] = &AdapterHealthChecker{
		name:          name,
		adapter:       adapter,
		isHealthy:     true,
		healthHistory: make([]HealthCheckResult, 0),
	}
	ms.metricsCollector.metrics[name] = &AdapterMetrics{
		Name:          name,
		CustomMetrics: make(map[string]interface{}),
	}

	ms.logger.Info("Added adapter to monitoring", map[string]interface{}{
		"adapter": name,
	})
}

// RemoveAdapter removes an adapter from monitoring
func (ms *MonitoringService) RemoveAdapter(name string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	delete(ms.adapters, name)
	delete(ms.healthCheckers, name)
	delete(ms.metricsCollector.metrics, name)

	ms.logger.Info("Removed adapter from monitoring", map[string]interface{}{
		"adapter": name,
	})
}

// GetAdapterHealth returns the health status of an adapter
func (ms *MonitoringService) GetAdapterHealth(name string) (*AdapterHealthChecker, bool) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	checker, exists := ms.healthCheckers[name]
	return checker, exists
}

// GetAdapterMetrics returns the metrics for an adapter
func (ms *MonitoringService) GetAdapterMetrics(name string) (*AdapterMetrics, bool) {
	ms.metricsCollector.mu.RLock()
	defer ms.metricsCollector.mu.RUnlock()

	metrics, exists := ms.metricsCollector.metrics[name]
	return metrics, exists
}

// GetOverallHealth returns the overall health status of all adapters
func (ms *MonitoringService) GetOverallHealth() map[string]interface{} {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	totalAdapters := len(ms.adapters)
	healthyAdapters := 0
	adapterStatus := make(map[string]bool)

	for name, checker := range ms.healthCheckers {
		checker.mu.RLock()
		healthy := checker.isHealthy
		checker.mu.RUnlock()

		adapterStatus[name] = healthy
		if healthy {
			healthyAdapters++
		}
	}

	return map[string]interface{}{
		"total_adapters":     totalAdapters,
		"healthy_adapters":   healthyAdapters,
		"unhealthy_adapters": totalAdapters - healthyAdapters,
		"overall_healthy":    healthyAdapters == totalAdapters,
		"adapter_status":     adapterStatus,
	}
}

// healthCheckLoop runs periodic health checks
func (ms *MonitoringService) healthCheckLoop() {
	ticker := time.NewTicker(ms.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ms.performHealthChecks()
		case <-ms.stopCh:
			return
		}
	}
}

// performHealthChecks performs health checks on all adapters
func (ms *MonitoringService) performHealthChecks() {
	ms.mu.RLock()
	adapters := make(map[string]types.LogAdapter)
	for name, adapter := range ms.adapters {
		adapters[name] = adapter
	}
	ms.mu.RUnlock()

	for name, adapter := range adapters {
		go ms.performHealthCheck(name, adapter)
	}
}

// performHealthCheck performs a health check on a specific adapter
func (ms *MonitoringService) performHealthCheck(name string, adapter types.LogAdapter) {
	ms.mu.RLock()
	checker, exists := ms.healthCheckers[name]
	ms.mu.RUnlock()

	if !exists {
		return
	}

	start := time.Now()
	err := adapter.Health()
	duration := time.Since(start)

	result := HealthCheckResult{
		Timestamp: start,
		Healthy:   err == nil,
		Duration:  duration,
	}

	if err != nil {
		result.Error = err.Error()
	}

	checker.mu.Lock()
	checker.lastHealthCheck = start
	checker.healthHistory = append(checker.healthHistory, result)

	// Keep only recent history
	if len(checker.healthHistory) > 100 {
		checker.healthHistory = checker.healthHistory[1:]
	}

	// Update health status
	previousHealth := checker.isHealthy
	checker.isHealthy = err == nil

	if !checker.isHealthy {
		checker.consecutiveFailures++
	} else {
		checker.consecutiveFailures = 0
	}
	checker.mu.Unlock()

	// Generate alerts if health status changed
	if previousHealth != checker.isHealthy {
		if !checker.isHealthy {
			ms.alertManager.createAlert(name, AlertTypeHealthCheck, AlertSeverityCritical,
				fmt.Sprintf("Adapter %s is unhealthy: %v", name, err))
		} else {
			ms.alertManager.resolveAlert(name, AlertTypeHealthCheck)
		}
	}
}

// metricsCollectionLoop runs periodic metrics collection
func (ms *MonitoringService) metricsCollectionLoop() {
	ticker := time.NewTicker(ms.config.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ms.collectMetrics()
		case <-ms.stopCh:
			return
		}
	}
}

// collectMetrics collects metrics from all adapters
func (ms *MonitoringService) collectMetrics() {
	ms.mu.RLock()
	adapters := make(map[string]types.LogAdapter)
	for name, adapter := range ms.adapters {
		adapters[name] = adapter
	}
	ms.mu.RUnlock()

	for name, adapter := range adapters {
		go ms.collectAdapterMetrics(name, adapter)
	}
}

// collectAdapterMetrics collects metrics from a specific adapter
func (ms *MonitoringService) collectAdapterMetrics(name string, adapter types.LogAdapter) {
	// Get adapter statistics if available
	var stats map[string]interface{}
	if statsProvider, ok := adapter.(interface{ GetStats() map[string]interface{} }); ok {
		stats = statsProvider.GetStats()
	}

	ms.metricsCollector.mu.Lock()
	defer ms.metricsCollector.mu.Unlock()

	metrics, exists := ms.metricsCollector.metrics[name]
	if !exists {
		return
	}

	// Update metrics from stats
	if stats != nil {
		if totalReqs, ok := stats["total_requests"].(int64); ok {
			metrics.TotalRequests = totalReqs
		}
		if successReqs, ok := stats["successful_requests"].(int64); ok {
			metrics.SuccessfulRequests = successReqs
		}
		if failedReqs, ok := stats["failed_requests"].(int64); ok {
			metrics.FailedRequests = failedReqs
		}
		if avgResponseTime, ok := stats["average_response_time"].(time.Duration); ok {
			metrics.AverageResponseTime = avgResponseTime
		}
		if cbTrips, ok := stats["circuit_breaker_trips"].(int64); ok {
			metrics.CircuitBreakerTrips = cbTrips
		}

		// Calculate error rate
		if metrics.TotalRequests > 0 {
			metrics.ErrorRate = float64(metrics.FailedRequests) / float64(metrics.TotalRequests) * 100
		}

		// Store custom metrics
		for key, value := range stats {
			if key != "total_requests" && key != "successful_requests" &&
				key != "failed_requests" && key != "average_response_time" &&
				key != "circuit_breaker_trips" {
				metrics.CustomMetrics[key] = value
			}
		}
	}

	metrics.LastActivity = time.Now()

	// Check alert thresholds
	ms.checkAlertThresholds(name, metrics)
}

// checkAlertThresholds checks if any alert thresholds are exceeded
func (ms *MonitoringService) checkAlertThresholds(name string, metrics *AdapterMetrics) {
	// Check error rate threshold
	if metrics.ErrorRate > ms.config.AlertThresholds.ErrorRate {
		ms.alertManager.createAlert(name, AlertTypeErrorRate, AlertSeverityWarning,
			fmt.Sprintf("High error rate: %.2f%% (threshold: %.2f%%)",
				metrics.ErrorRate, ms.config.AlertThresholds.ErrorRate))
	}

	// Check response time threshold
	if metrics.AverageResponseTime > ms.config.AlertThresholds.ResponseTime {
		ms.alertManager.createAlert(name, AlertTypeResponseTime, AlertSeverityWarning,
			fmt.Sprintf("High response time: %v (threshold: %v)",
				metrics.AverageResponseTime, ms.config.AlertThresholds.ResponseTime))
	}

	// Check circuit breaker trips threshold
	if metrics.CircuitBreakerTrips > int64(ms.config.AlertThresholds.CircuitBreaker) {
		ms.alertManager.createAlert(name, AlertTypeCircuitBreaker, AlertSeverityCritical,
			fmt.Sprintf("High circuit breaker trips: %d (threshold: %d)",
				metrics.CircuitBreakerTrips, ms.config.AlertThresholds.CircuitBreaker))
	}
}

// alertProcessingLoop processes alerts
func (ms *MonitoringService) alertProcessingLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ms.processAlerts()
		case <-ms.stopCh:
			return
		}
	}
}

// processAlerts processes and cleans up old alerts
func (ms *MonitoringService) processAlerts() {
	ms.alertManager.mu.Lock()
	defer ms.alertManager.mu.Unlock()

	// Clean up resolved alerts older than retention period
	cutoff := time.Now().Add(-ms.config.RetentionPeriod)
	filtered := make([]Alert, 0)

	for _, alert := range ms.alertManager.alertHistory {
		if alert.Resolved && alert.ResolvedAt != nil && alert.ResolvedAt.Before(cutoff) {
			continue // Skip old resolved alerts
		}
		filtered = append(filtered, alert)
	}

	ms.alertManager.alertHistory = filtered
}

// startHTTPServer starts the HTTP server for health and metrics endpoints
func (ms *MonitoringService) startHTTPServer() error {
	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("/health", ms.handleHealth)
	mux.HandleFunc("/health/", ms.handleAdapterHealth)

	// Metrics endpoint
	mux.HandleFunc("/metrics", ms.handleMetrics)
	mux.HandleFunc("/metrics/", ms.handleAdapterMetrics)

	// Alerts endpoint
	mux.HandleFunc("/alerts", ms.handleAlerts)

	ms.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", ms.config.Port),
		Handler: mux,
	}

	go func() {
		if err := ms.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			ms.logger.Error("HTTP server error", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()

	return nil
}

// HTTP handlers

// handleHealth handles the overall health endpoint
func (ms *MonitoringService) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := ms.GetOverallHealth()

	w.Header().Set("Content-Type", "application/json")
	if !health["overall_healthy"].(bool) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	json.NewEncoder(w).Encode(health)
}

// handleAdapterHealth handles adapter-specific health endpoints
func (ms *MonitoringService) handleAdapterHealth(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[len("/health/"):]

	checker, exists := ms.GetAdapterHealth(name)
	if !exists {
		http.NotFound(w, r)
		return
	}

	checker.mu.RLock()
	response := map[string]interface{}{
		"adapter":              name,
		"healthy":              checker.isHealthy,
		"last_health_check":    checker.lastHealthCheck,
		"consecutive_failures": checker.consecutiveFailures,
		"recent_history":       checker.healthHistory,
	}
	checker.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if !checker.isHealthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	json.NewEncoder(w).Encode(response)
}

// handleMetrics handles the metrics endpoint
func (ms *MonitoringService) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ms.metricsCollector.mu.RLock()
	defer ms.metricsCollector.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ms.metricsCollector.metrics)
}

// handleAdapterMetrics handles adapter-specific metrics endpoints
func (ms *MonitoringService) handleAdapterMetrics(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[len("/metrics/"):]

	metrics, exists := ms.GetAdapterMetrics(name)
	if !exists {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// handleAlerts handles the alerts endpoint
func (ms *MonitoringService) handleAlerts(w http.ResponseWriter, r *http.Request) {
	ms.alertManager.mu.RLock()
	defer ms.alertManager.mu.RUnlock()

	// Sort alerts by timestamp (newest first)
	alerts := make([]Alert, 0, len(ms.alertManager.alertHistory))
	alerts = append(alerts, ms.alertManager.alertHistory...)

	sort.Slice(alerts, func(i, j int) bool {
		return alerts[i].Timestamp.After(alerts[j].Timestamp)
	})

	response := map[string]interface{}{
		"active_alerts": ms.alertManager.activeAlerts,
		"alert_history": alerts,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Alert manager methods

// createAlert creates a new alert
func (am *AlertManager) createAlert(adapterName string, alertType AlertType, severity AlertSeverity, message string) {
	am.mu.Lock()
	defer am.mu.Unlock()

	alertID := fmt.Sprintf("%s_%s", adapterName, alertType)

	// Don't create duplicate active alerts
	if _, exists := am.activeAlerts[alertID]; exists {
		return
	}

	alert := &Alert{
		ID:          alertID,
		AdapterName: adapterName,
		Type:        alertType,
		Severity:    severity,
		Message:     message,
		Timestamp:   time.Now(),
		Resolved:    false,
	}

	am.activeAlerts[alertID] = alert
	am.alertHistory = append(am.alertHistory, *alert)

	// Trigger alert handlers
	for _, handler := range am.alertHandlers {
		go handler.HandleAlert(*alert)
	}
}

// resolveAlert resolves an active alert
func (am *AlertManager) resolveAlert(adapterName string, alertType AlertType) {
	am.mu.Lock()
	defer am.mu.Unlock()

	alertID := fmt.Sprintf("%s_%s", adapterName, alertType)

	if alert, exists := am.activeAlerts[alertID]; exists {
		now := time.Now()
		alert.Resolved = true
		alert.ResolvedAt = &now

		// Update in history
		for i := range am.alertHistory {
			if am.alertHistory[i].ID == alertID && !am.alertHistory[i].Resolved {
				am.alertHistory[i].Resolved = true
				am.alertHistory[i].ResolvedAt = &now
				break
			}
		}

		delete(am.activeAlerts, alertID)
	}
}

// AddAlertHandler adds an alert handler
func (am *AlertManager) AddAlertHandler(handler AlertHandler) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.alertHandlers = append(am.alertHandlers, handler)
}
