package handler

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"sync"
	"uptime-monitor/models"
)

type MonitorInfoProvider interface {
	GetAllServices() map[string]*models.ServiceStatus
	GetServiceResponseHistory(serviceID int, limit int) ([]models.ResponseTimePoint, error)
}

type HTTPHandlers struct {
	monitor MonitorInfoProvider
	mutex   *sync.RWMutex
}

func NewHTTPHandlers(monitor MonitorInfoProvider, mu *sync.RWMutex) *HTTPHandlers {
	if monitor == nil {
		panic("monitor cannot be nil")
	}
	if mu == nil {
		panic("mutex cannot be nil")
	}
	return &HTTPHandlers{
		monitor: monitor,
		mutex:   mu,
	}
}

func (h *HTTPHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, "index.html")
	})
	mux.HandleFunc("/health", h.handleHealth)
	mux.HandleFunc("/status", h.handleStatus)
	mux.HandleFunc("/status/html", h.handleStatusHTML)
}


func (h *HTTPHandlers) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (h *HTTPHandlers) handleStatus(w http.ResponseWriter, r *http.Request) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	status := make(map[string]*models.ServiceStatus)
	for url, service := range h.monitor.GetAllServices() {
		service.Mutex.RLock()
		status[url] = &models.ServiceStatus{
			ID:               service.ID,
			URL:              service.URL,
			IsUp:             service.IsUp,
			LastCheck:        service.LastCheck,
			ResponseTime:     service.ResponseTime,
			StatusCode:       service.StatusCode,
			TotalChecks:      service.TotalChecks,
			SuccessChecks:    service.SuccessChecks,
			ConsecutiveFails: service.ConsecutiveFails,
			LastError:        service.LastError,
			CreatedAt:        service.CreatedAt,
			UpdatedAt:        service.UpdatedAt,
		}
		service.Mutex.RUnlock()
	}

	json.NewEncoder(w).Encode(status)
}

func (h *HTTPHandlers) handleStatusHTML(w http.ResponseWriter, r *http.Request) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<div class="space-y-6">`))

	for _, service := range h.monitor.GetAllServices() {
		service.Mutex.RLock()

		statusClass := "bg-green-100 text-green-700"
		if !service.IsUp {
			statusClass = "bg-red-100 text-red-700"
		}

		safeURL := html.EscapeString(service.URL)
		safeStatus := html.EscapeString(map[bool]string{true: "Operational", false: "Down"}[service.IsUp])
		uptime := service.GetUptime()
		safeLastCheck := html.EscapeString(service.LastCheck.Format("2006-01-02 15:04:05"))
		safeLastError := html.EscapeString(service.LastError)

		w.Write([]byte(fmt.Sprintf(`
            <div class="bg-white rounded-lg shadow p-6 border border-gray-200">
                <div class="flex items-center justify-between mb-2">
                    <div class="flex items-center gap-2">
                        <span class="font-semibold text-lg text-gray-800">%s</span>
                    </div>
                    <span class="ml-2 px-3 py-1 rounded-full %s text-xs font-bold uppercase tracking-wide">%s</span>
                </div>
                <div class="flex items-center gap-2 mb-2">
                    <div class="flex gap-[2px]">`, safeURL, statusClass, safeStatus)))

		for i := 0; i < 45; i++ {
			w.Write([]byte(`<div class="w-1.5 h-5 bg-green-400 rounded"></div>`))
		}

		w.Write([]byte(fmt.Sprintf(`
                    </div>
                    <span class="ml-4 text-xs text-gray-500">90 days ago</span>
                    <span class="ml-auto text-xs text-gray-500">Today</span>
                </div>
                <div class="flex items-center justify-between text-xs text-gray-600 mt-2">
                    <span>%.2f%% uptime</span>
                    <span>Last check: %s</span>
                    <span>Response: %dms</span>
                    <span class="text-red-500">%s</span>
                </div>
            </div>`, uptime, safeLastCheck, service.ResponseTime, safeLastError)))
		service.Mutex.RUnlock()
	}
	w.Write([]byte(`</div>`))
}
