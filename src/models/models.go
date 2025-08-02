package models

import (
	"sync"
	"time"
)


type ResponseTimePoint struct {
	Timestamp int64 `json:"timestamp"`  
	ResponseMs int64 `json:"response_ms"` 
}


type ServiceStatus struct {
	ID               int       `json:"id"`
	URL              string    `json:"url"`
	IsUp             bool      `json:"is_up"`
	LastCheck        time.Time `json:"last_check"`
	ResponseTime     int64     `json:"response_time_ms"`
	StatusCode       int       `json:"status_code"`
	TotalChecks      int       `json:"total_checks"`
	SuccessChecks    int       `json:"success_checks"`
	ConsecutiveFails int       `json:"consecutive_fails"`
	LastError        string    `json:"last_error,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Mutex            sync.RWMutex `json:"-"` 
	
}


func (s *ServiceStatus) GetUptime() float64 {
	s.Mutex.RLock()
	defer s.Mutex.RUnlock()

	if s.TotalChecks == 0 {
		return 0
	}
	return float64(s.SuccessChecks) / float64(s.TotalChecks) * 100
}


type Alert struct {
	ServiceURL string    `json:"service_url"`
	Type       string    `json:"type"`      
	Message    string    `json:"message"`
	Timestamp  time.Time `json:"timestamp"`
}