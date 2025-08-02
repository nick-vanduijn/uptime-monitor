package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	CheckInterval  time.Duration
	ReportInterval time.Duration
	RequestTimeout time.Duration
	DatabasePath   string
	LogLevel       string
	HTTPPort       string
	EmailSMTPHost  string
	EmailSMTPPort  int
	EmailUsername  string
	EmailPassword  string
	EmailFrom      string
	EmailTo        []string
	WebhookURL     string
	Services       []string
	MaxRetries     int
	RetryDelay     time.Duration
	TLSSkipVerify  bool
	MaxConcurrency int
}

func LoadConfig() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		fmt.Println("No .env file found, relying on environment variables.")
	}

	cfg := &Config{
		CheckInterval:  getEnvDuration("CHECK_INTERVAL", 10*time.Second),
		ReportInterval: getEnvDuration("REPORT_INTERVAL", 5*time.Minute),
		RequestTimeout: getEnvDuration("REQUEST_TIMEOUT", 10*time.Second),
		DatabasePath:   getEnvString("DATABASE_PATH", "uptime.db"),
		LogLevel:       getEnvString("LOG_LEVEL", "INFO"),
		HTTPPort:       getEnvString("HTTP_PORT", "8080"),
		EmailSMTPHost:  getEnvString("EMAIL_SMTP_HOST", ""),
		EmailSMTPPort:  getEnvInt("EMAIL_SMTP_PORT", 587),
		EmailUsername:  getEnvString("EMAIL_USERNAME", ""),
		EmailPassword:  getEnvString("EMAIL_PASSWORD", ""),
		EmailFrom:      getEnvString("EMAIL_FROM", ""),
		EmailTo:        getEnvStringSlice("EMAIL_TO", []string{}),
		WebhookURL:     getEnvString("WEBHOOK_URL", ""),
		Services: getEnvStringSlice("SERVICES", []string{
			"https://google.com",
		}), 
		MaxRetries:     getEnvInt("MAX_RETRIES", 2),
		RetryDelay:     getEnvDuration("RETRY_DELAY", 1*time.Second),
		TLSSkipVerify:  getEnvBool("TLS_SKIP_VERIFY", false),
		MaxConcurrency: getEnvInt("MAX_CONCURRENCY", 10),
	}

	return cfg, nil
}

func getEnvString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func getEnvStringSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		parts := strings.Split(value, ",")
		var cleanedParts []string
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				cleanedParts = append(cleanedParts, trimmed)
			}
		}
		if len(cleanedParts) > 0 {
			return cleanedParts
		}
	}
	return defaultValue
}