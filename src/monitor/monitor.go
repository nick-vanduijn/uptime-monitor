package monitor

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"uptime-monitor/database"
	"uptime-monitor/handler"
	"uptime-monitor/internal/config"
	"uptime-monitor/models"
)

type UptimeMonitor struct {
	config    *config.Config
	services  map[string]*models.ServiceStatus
	client    *http.Client
	dbManager *database.DBManager
	logger    *slog.Logger
	alertChan chan models.Alert
	semaphore chan struct{}
	mutex     sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	routines  sync.WaitGroup
}

func NewUptimeMonitor(cfg *config.Config, logger *slog.Logger) (*UptimeMonitor, error) {
	ctx, cancel := context.WithCancel(context.Background())

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.TLSSkipVerify,
		},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	client := &http.Client{
		Timeout:   cfg.RequestTimeout,
		Transport: transport,
	}

	dbManager, err := database.NewDBManager(cfg.DatabasePath, logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize database manager: %w", err)
	}

	um := &UptimeMonitor{
		config:    cfg,
		services:  make(map[string]*models.ServiceStatus),
		client:    client,
		dbManager: dbManager,
		logger:    logger,
		alertChan: make(chan models.Alert, 100),
		semaphore: make(chan struct{}, cfg.MaxConcurrency),
		ctx:       ctx,
		cancel:    cancel,
	}

	if err := um.loadServices(); err != nil {
		logger.Error("Failed to load services from database", "error", err)
		return nil, fmt.Errorf("failed to load services: %w", err)
	}

	for _, serviceURL := range cfg.Services {
		if err := um.AddService(serviceURL); err != nil {
			logger.Error("Failed to add service from config", "url", serviceURL, "error", err)
		}
	}

	return um, nil
}

func (um *UptimeMonitor) loadServices() error {
	dbServices, err := um.dbManager.LoadServicesFromDB()
	if err != nil {
		return fmt.Errorf("failed to load services from DB: %w", err)
	}

	um.mutex.Lock()
	defer um.mutex.Unlock()

	for url, service := range dbServices {
		um.services[url] = service
		um.logger.Debug("Loaded service from DB", "url", url, "id", service.ID)
	}
	return nil
}

func (um *UptimeMonitor) AddService(url string) error {
	um.mutex.Lock()
	defer um.mutex.Unlock()

	if _, exists := um.services[url]; exists {
		return fmt.Errorf("service %s already exists", url)
	}

	service := &models.ServiceStatus{
		URL:       url,
		IsUp:      false,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := um.dbManager.AddServiceToDB(service); err != nil {
		return fmt.Errorf("failed to add service to database: %w", err)
	}

	um.services[url] = service
	um.logger.Info("Added new service", "url", url, "id", service.ID)

	return nil
}

func (um *UptimeMonitor) CheckService(url string) error {

	select {
	case <-um.ctx.Done():
		return um.ctx.Err()
	case um.semaphore <- struct{}{}:

	}
	defer func() { <-um.semaphore }()

	um.mutex.RLock()
	service, exists := um.services[url]
	um.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("service %s not found in monitor", url)
	}

	service.Mutex.Lock()
	defer service.Mutex.Unlock()

	var lastErr error
	var success bool
	var statusCode int
	var duration time.Duration

	for attempt := 0; attempt <= um.config.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-um.ctx.Done():
				return um.ctx.Err()
			case <-time.After(um.config.RetryDelay):
				um.logger.Debug("Retrying service check", "url", url, "attempt", attempt+1)
			}
		}

		start := time.Now()
		req, err := http.NewRequestWithContext(um.ctx, "GET", url, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to create request: %w", err)
			um.logger.Debug("Check attempt failed (request creation)", "url", url, "attempt", attempt+1, "error", lastErr)
			continue
		}

		resp, err := um.client.Do(req)
		duration = time.Since(start)

		if err != nil {
			lastErr = err
			um.logger.Debug("Check attempt failed (HTTP request)", "url", url, "attempt", attempt+1, "error", err)
			continue
		}
		defer resp.Body.Close()
		statusCode = resp.StatusCode

		if statusCode >= 200 && statusCode < 400 {
			success = true
			lastErr = nil
			break
		} else {
			lastErr = fmt.Errorf("HTTP status %d", statusCode)
			um.logger.Debug("Check attempt failed (bad status code)", "url", url, "attempt", attempt+1, "status", statusCode)
		}
	}

	previousStatus := service.IsUp
	service.LastCheck = time.Now()
	service.ResponseTime = duration.Milliseconds()
	service.StatusCode = statusCode
	service.TotalChecks++
	service.UpdatedAt = time.Now()

	if success {
		service.IsUp = true
		service.SuccessChecks++
		service.ConsecutiveFails = 0
		service.LastError = ""

		if !previousStatus {
			um.alertChan <- models.Alert{
				ServiceURL: url,
				Type:       "up",
				Message:    fmt.Sprintf("Service %s is now UP (Status: %d, Response: %dms)", url, statusCode, duration.Milliseconds()),
				Timestamp:  time.Now(),
			}
		}
	} else {
		service.IsUp = false
		service.ConsecutiveFails++
		if lastErr != nil {
			service.LastError = lastErr.Error()
		} else {
			service.LastError = "unknown error"
		}

		if previousStatus {
			um.alertChan <- models.Alert{
				ServiceURL: url,
				Type:       "down",
				Message:    fmt.Sprintf("Service %s is DOWN (%s)", url, service.LastError),
				Timestamp:  time.Now(),
			}
		}
	}

	if success && duration > 5*time.Second {
		um.alertChan <- models.Alert{
			ServiceURL: url,
			Type:       "slow",
			Message:    fmt.Sprintf("Service %s is responding slowly (%dms)", url, duration.Milliseconds()),
			Timestamp:  time.Now(),
		}
	}

	um.dbManager.UpdateServiceInDB(service)
	if err := um.dbManager.RecordCheckHistory(service.ID, service.IsUp, service.ResponseTime, service.StatusCode, service.LastError); err != nil {
		um.logger.Error("Failed to record check history", "url", url, "error", err)
	}

	statusStr := "UP"
	if !success {
		statusStr = "DOWN"
	}

	um.logger.Info("Health check completed",
		"url", url,
		"status", statusStr,
		"code", statusCode,
		"response_time_ms", duration.Milliseconds(),
		"consecutive_fails", service.ConsecutiveFails,
		"error_message", service.LastError,
	)

	return lastErr
}

func (um *UptimeMonitor) StartMonitoring() {
	um.logger.Info("Starting uptime monitoring",
		"check_interval", um.config.CheckInterval,
		"services_count", len(um.services),
		"max_concurrency", um.config.MaxConcurrency,
	)

	um.routines.Add(1)
	go func() {
		defer um.routines.Done()
		um.processAlerts()
	}()
	um.routines.Add(1)
	go func() {
		defer um.routines.Done()
		um.startHTTPServer()
	}()
	um.routines.Add(1)
	go func() {
		defer um.routines.Done()
		um.startStatusReporting()
	}()

	ticker := time.NewTicker(um.config.CheckInterval)
	defer ticker.Stop()

	um.checkAllServices()

	for {
		select {
		case <-um.ctx.Done():
			um.logger.Info("Monitoring routine stopped by context cancellation")
			return
		case <-ticker.C:
			um.checkAllServices()
		}
	}
}

func (um *UptimeMonitor) checkAllServices() {
	um.mutex.RLock()
	urls := make([]string, 0, len(um.services))
	for url := range um.services {
		urls = append(urls, url)
	}
	um.mutex.RUnlock()

	var wg sync.WaitGroup
	for _, url := range urls {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			err := um.CheckService(u)
			if err != nil {
				um.logger.Debug("Service check finished with error", "url", u, "error", err)
			}
		}(url)
	}
	wg.Wait()
	um.logger.Debug("All services checked in this interval")
}

func (um *UptimeMonitor) processAlerts() {
	for {
		select {
		case <-um.ctx.Done():
			um.logger.Info("Alert processing routine stopped by context cancellation")
			return
		case alert, ok := <-um.alertChan:
			if !ok {
				um.logger.Info("Alert channel closed, stopping alert processing")
				return
			}
			um.logger.Info("Processing alert", "type", alert.Type, "service", alert.ServiceURL)

			if um.config.EmailSMTPHost != "" && len(um.config.EmailTo) > 0 {
				go um.sendEmailAlert(alert)
			}

			if um.config.WebhookURL != "" {
				go um.sendWebhookAlert(alert)
			}
		}
	}
}

func (um *UptimeMonitor) sendEmailAlert(alert models.Alert) {
	subject := fmt.Sprintf("Uptime Monitor Alert: %s - %s", strings.ToUpper(alert.Type), alert.ServiceURL)
	body := fmt.Sprintf("Service: %s\nType: %s\nMessage: %s\nTime: %s",
		alert.ServiceURL, alert.Type, alert.Message, alert.Timestamp.Format(time.RFC3339))

	auth := smtp.PlainAuth("", um.config.EmailUsername, um.config.EmailPassword, um.config.EmailSMTPHost)

	msg := []byte(fmt.Sprintf("To: %s\r\nSubject: %s\r\n\r\n%s\r\n",
		strings.Join(um.config.EmailTo, ","), subject, body))

	addr := fmt.Sprintf("%s:%d", um.config.EmailSMTPHost, um.config.EmailSMTPPort)
	err := smtp.SendMail(addr, auth, um.config.EmailFrom, um.config.EmailTo, msg)

	if err != nil {
		um.logger.Error("Failed to send email alert", "error", err, "service", alert.ServiceURL, "type", alert.Type)
	} else {
		um.logger.Info("Email alert sent", "to", um.config.EmailTo, "type", alert.Type, "service", alert.ServiceURL)
	}
}

func (um *UptimeMonitor) sendWebhookAlert(alert models.Alert) {
	payload, err := json.Marshal(alert)
	if err != nil {
		um.logger.Error("Failed to marshal webhook payload", "error", err, "service", alert.ServiceURL)
		return
	}

	req, err := http.NewRequestWithContext(um.ctx, "POST", um.config.WebhookURL, strings.NewReader(string(payload)))
	if err != nil {
		um.logger.Error("Failed to create webhook request", "error", err, "service", alert.ServiceURL)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := um.client.Do(req)
	if err != nil {
		um.logger.Error("Failed to send webhook alert", "error", err, "service", alert.ServiceURL)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		um.logger.Info("Webhook alert sent", "status", resp.StatusCode, "type", alert.Type, "service", alert.ServiceURL)
	} else {
		um.logger.Error("Webhook alert failed", "status", resp.StatusCode, "type", alert.Type, "service", alert.ServiceURL)

	}
}

func (um *UptimeMonitor) startHTTPServer() {
	mux := http.NewServeMux()
	handlers := handler.NewHTTPHandlers(um, &um.mutex)
	handlers.RegisterRoutes(mux)

	server := &http.Server{
		Addr:    ":" + um.config.HTTPPort,
		Handler: mux,
	}

	um.logger.Info("Starting HTTP server", "port", um.config.HTTPPort)

	go func() {
		<-um.ctx.Done()
		um.logger.Info("Shutting down HTTP server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			um.logger.Error("HTTP server graceful shutdown failed", "error", err)
		} else {
			um.logger.Info("HTTP server shut down gracefully")
		}
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		um.logger.Error("HTTP server failed to listen or serve", "error", err)
	}
}

func (um *UptimeMonitor) GetAllServices() map[string]*models.ServiceStatus {
	um.mutex.RLock()
	defer um.mutex.RUnlock()

	services := make(map[string]*models.ServiceStatus, len(um.services))
	for k, v := range um.services {
		services[k] = v
	}
	return services
}

func (um *UptimeMonitor) GetServiceResponseHistory(serviceID int, limit int) ([]models.ResponseTimePoint, error) {
	return um.dbManager.GetResponseTimeHistory(serviceID, limit)
}

func (um *UptimeMonitor) startStatusReporting() {
	if um.config.ReportInterval <= 0 {
		um.logger.Info("Status reporting disabled (REPORT_INTERVAL <= 0)")
		return
	}

	ticker := time.NewTicker(um.config.ReportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-um.ctx.Done():
			um.logger.Info("Status reporting routine stopped by context cancellation")
			return
		case <-ticker.C:
			um.printStatusReport()
		}
	}
}

func (um *UptimeMonitor) printStatusReport() {
	um.mutex.RLock()
	defer um.mutex.RUnlock()

	totalServices := len(um.services)
	upServices := 0

	um.logger.Info("=== Uptime Monitor Status Report ===")

	for _, service := range um.services {
		service.Mutex.RLock()
		if service.IsUp {
			upServices++
		}

		uptime := service.GetUptime()
		um.logger.Info("Service status",
			"url", service.URL,
			"status", map[bool]string{true: "UP", false: "DOWN"}[service.IsUp],
			"uptime_percent", fmt.Sprintf("%.2f", uptime),
			"response_time_ms", service.ResponseTime,
			"total_checks", service.TotalChecks,
			"consecutive_fails", service.ConsecutiveFails,
			"last_error", service.LastError,
		)
		service.Mutex.RUnlock()
	}

	overallAvailability := 0.0
	if totalServices > 0 {
		overallAvailability = float64(upServices) / float64(totalServices) * 100
	}

	um.logger.Info("Summary",
		"services_up", upServices,
		"services_total", totalServices,
		"overall_availability", fmt.Sprintf("%.1f%%", overallAvailability),
	)
}

func (um *UptimeMonitor) Close() error {
	um.logger.Info("Shutting down uptime monitor...")

	um.cancel()
	um.routines.Wait()

	if um.dbManager != nil {
		if err := um.dbManager.Close(); err != nil {
			um.logger.Error("Failed to close database", "error", err)
			return fmt.Errorf("failed to close database: %w", err)
		}
	}

	close(um.alertChan)

	um.logger.Info("Uptime monitor shutdown complete")
	return nil
}
