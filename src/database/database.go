package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"
	"uptime-monitor/models"

	_ "github.com/mattn/go-sqlite3"
)


type DBManager struct {
	db     *sql.DB
	logger *slog.Logger
}


func NewDBManager(dbPath string, logger *slog.Logger) (*DBManager, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := initSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize database schema: %w", err)
	}

	return &DBManager{db: db, logger: logger}, nil
}


func initSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS services (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT UNIQUE NOT NULL,
		is_up BOOLEAN DEFAULT FALSE,
		last_check DATETIME,
		response_time_ms INTEGER DEFAULT 0,
		status_code INTEGER DEFAULT 0,
		total_checks INTEGER DEFAULT 0,
		success_checks INTEGER DEFAULT 0,
		consecutive_fails INTEGER DEFAULT 0,
		last_error TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	
	CREATE TABLE IF NOT EXISTS check_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		service_id INTEGER,
		is_up BOOLEAN,
		response_time_ms INTEGER,
		status_code INTEGER,
		error_message TEXT,
		checked_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (service_id) REFERENCES services (id) ON DELETE CASCADE
	);
	
	CREATE INDEX IF NOT EXISTS idx_check_history_service_id ON check_history(service_id);
	CREATE INDEX IF NOT EXISTS idx_check_history_checked_at ON check_history(checked_at);
	`

	_, err := db.Exec(schema)
	return err
}


func (dm *DBManager) LoadServicesFromDB() (map[string]*models.ServiceStatus, error) {
	services := make(map[string]*models.ServiceStatus)
	rows, err := dm.db.Query(`
		SELECT id, url, is_up, last_check, response_time_ms, status_code, 
			   total_checks, success_checks, consecutive_fails, last_error, 
			   created_at, updated_at 
		FROM services
	`)
	if err != nil {
		return nil, fmt.Errorf("querying services failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		service := &models.ServiceStatus{}
		var lastCheck, createdAt, updatedAt sql.NullTime
		var lastError sql.NullString

		err := rows.Scan(
			&service.ID, &service.URL, &service.IsUp, &lastCheck,
			&service.ResponseTime, &service.StatusCode, &service.TotalChecks,
			&service.SuccessChecks, &service.ConsecutiveFails, &lastError,
			&createdAt, &updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning service row failed: %w", err)
		}

		if lastCheck.Valid {
			service.LastCheck = lastCheck.Time
		}
		if createdAt.Valid {
			service.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			service.UpdatedAt = updatedAt.Time
		}
		if lastError.Valid {
			service.LastError = lastError.String
		}

		services[service.URL] = service
	}

	return services, rows.Err()
}



func (dm *DBManager) AddServiceToDB(service *models.ServiceStatus) error {
	result, err := dm.db.Exec(`
		INSERT OR IGNORE INTO services (url, created_at, updated_at) 
		VALUES (?, ?, ?)
	`, service.URL, service.CreatedAt, service.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to insert service: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	
	if rowsAffected == 0 {
		var id int
		err := dm.db.QueryRow("SELECT id FROM services WHERE url = ?", service.URL).Scan(&id)
		if err != nil {
			return fmt.Errorf("failed to retrieve existing service ID: %w", err)
		}
		service.ID = id
	} else {
		
		id, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("failed to get last insert ID: %w", err)
		}
		service.ID = int(id)
	}

	return nil
}


func (dm *DBManager) UpdateServiceInDB(service *models.ServiceStatus) {
	
	
	_, err := dm.db.Exec(`
		UPDATE services 
		SET is_up = ?, last_check = ?, response_time_ms = ?, status_code = ?,
			total_checks = ?, success_checks = ?, consecutive_fails = ?,
			last_error = ?, updated_at = ?
		WHERE id = ?
	`, service.IsUp, service.LastCheck, service.ResponseTime, service.StatusCode,
		service.TotalChecks, service.SuccessChecks, service.ConsecutiveFails,
		service.LastError, service.UpdatedAt, service.ID)

	if err != nil {
		dm.logger.Error("Failed to update service in database", "error", err, "service_id", service.ID, "url", service.URL)
	}
}


func (dm *DBManager) RecordCheckHistory(serviceID int, isUp bool, responseTimeMs int64, statusCode int, errorMsg string) {
	_, err := dm.db.Exec(`
		INSERT INTO check_history (service_id, is_up, response_time_ms, status_code, error_message)
		VALUES (?, ?, ?, ?, ?)
	`, serviceID, isUp, responseTimeMs, statusCode, errorMsg)

	if err != nil {
		dm.logger.Error("Failed to record check history", "error", err, "service_id", serviceID)
	}
}


func (dm *DBManager) GetResponseTimeHistory(serviceID int, limit int) ([]models.ResponseTimePoint, error) {
	query := `
		SELECT checked_at, response_time_ms
		FROM check_history
		WHERE service_id = ?
		ORDER BY checked_at DESC
		LIMIT ?
	`
	rows, err := dm.db.Query(query, serviceID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query response time history: %w", err)
	}
	defer rows.Close()

	var history []models.ResponseTimePoint
	for rows.Next() {
		var timestamp time.Time
		var responseMs int64
		if err := rows.Scan(&timestamp, &responseMs); err != nil {
			return nil, fmt.Errorf("failed to scan response time history row: %w", err)
		}
		history = append(history, models.ResponseTimePoint{Timestamp: timestamp.Unix(), ResponseMs: responseMs})
	}
	
	for i, j := 0, len(history)-1; i < j; i, j = i+1, j-1 {
		history[i], history[j] = history[j], history[i]
	}
	return history, rows.Err()
}


func (dm *DBManager) Close() error {
	return dm.db.Close()
}