package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppName  string
	Env      string
	API      HTTPServerConfig
	Cleanup  CleanupConfig
	Worker   WorkerConfig
	Postgres PostgresConfig
	Redis    RedisConfig
	Storage  StorageConfig
}

type HTTPServerConfig struct {
	Addr            string
	RateLimit       HTTPRateLimitConfig
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

type HTTPRateLimitConfig struct {
	Enabled                  bool
	ClientTTL                time.Duration
	PresignBurst             int
	PresignRequestsPerMinute int
	CreateBurst              int
	CreateRequestsPerMinute  int
	RetryBurst               int
	RetryRequestsPerMinute   int
}

type WorkerConfig struct {
	Concurrency int
	JobTimeout  time.Duration
	MetricsAddr string
	PollTimeout time.Duration
}

type CleanupConfig struct {
	BatchSize     int
	DLQRetention  time.Duration
	Enabled       bool
	Interval      time.Duration
	JobRetention  time.Duration
	StartupJitter time.Duration
}

type PostgresConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
	SSLMode  string
}

type RedisConfig struct {
	Addr       string
	Password   string
	DB         int
	QueueKey   string
	DLQKey     string
	MaxRetries int
}

type StorageConfig struct {
	Endpoint           string
	PublicBaseURL      string
	AccessKey          string
	SecretKey          string
	Bucket             string
	Region             string
	UseSSL             bool
	UploadPresignTTL   time.Duration
	DownloadPresignTTL time.Duration
}

func Load(service string) (Config, error) {
	var parseErr error
	readInt := func(key string, fallback int) int {
		if parseErr != nil {
			return fallback
		}

		value, err := intFromEnv(key, fallback)
		if err != nil {
			parseErr = err
			return fallback
		}

		return value
	}
	readBool := func(key string, fallback bool) bool {
		if parseErr != nil {
			return fallback
		}

		value, err := boolFromEnv(key, fallback)
		if err != nil {
			parseErr = err
			return fallback
		}

		return value
	}
	readDuration := func(key string, fallback time.Duration) time.Duration {
		if parseErr != nil {
			return fallback
		}

		value, err := durationFromEnv(key, fallback)
		if err != nil {
			parseErr = err
			return fallback
		}

		return value
	}

	cfg := Config{
		AppName: fmt.Sprintf("photon-%s", service),
		Env:     getEnv("PHOTON_ENV", "development"),
		API: HTTPServerConfig{
			Addr: getEnv("PHOTON_API_ADDR", ":8080"),
			RateLimit: HTTPRateLimitConfig{
				Enabled:                  readBool("PHOTON_API_RATE_LIMIT_ENABLED", true),
				ClientTTL:                readDuration("PHOTON_API_RATE_LIMIT_CLIENT_TTL", 30*time.Minute),
				PresignBurst:             readInt("PHOTON_API_PRESIGN_RATE_LIMIT_BURST", 5),
				PresignRequestsPerMinute: readInt("PHOTON_API_PRESIGN_RATE_LIMIT_PER_MINUTE", 20),
				CreateBurst:              readInt("PHOTON_API_JOBS_CREATE_RATE_LIMIT_BURST", 10),
				CreateRequestsPerMinute:  readInt("PHOTON_API_JOBS_CREATE_RATE_LIMIT_PER_MINUTE", 30),
				RetryBurst:               readInt("PHOTON_API_JOBS_RETRY_RATE_LIMIT_BURST", 2),
				RetryRequestsPerMinute:   readInt("PHOTON_API_JOBS_RETRY_RATE_LIMIT_PER_MINUTE", 6),
			},
			ReadTimeout:     readDuration("PHOTON_API_READ_TIMEOUT", 10*time.Second),
			WriteTimeout:    readDuration("PHOTON_API_WRITE_TIMEOUT", 15*time.Second),
			ShutdownTimeout: readDuration("PHOTON_API_SHUTDOWN_TIMEOUT", 10*time.Second),
		},
		Worker: WorkerConfig{
			Concurrency: readInt("PHOTON_WORKER_CONCURRENCY", 4),
			JobTimeout:  readDuration("PHOTON_WORKER_JOB_TIMEOUT", 2*time.Minute),
			MetricsAddr: getEnv("PHOTON_WORKER_METRICS_ADDR", ":8081"),
			PollTimeout: readDuration("PHOTON_WORKER_POLL_TIMEOUT", 5*time.Second),
		},
		Cleanup: CleanupConfig{
			BatchSize:     readInt("PHOTON_CLEANUP_BATCH_SIZE", 50),
			DLQRetention:  readDuration("PHOTON_CLEANUP_DLQ_RETENTION", 168*time.Hour),
			Enabled:       readBool("PHOTON_CLEANUP_ENABLED", true),
			Interval:      readDuration("PHOTON_CLEANUP_INTERVAL", 6*time.Hour),
			JobRetention:  readDuration("PHOTON_CLEANUP_JOB_RETENTION", 168*time.Hour),
			StartupJitter: readDuration("PHOTON_CLEANUP_STARTUP_JITTER", 0),
		},
		Postgres: PostgresConfig{
			Host:     getEnv("PHOTON_POSTGRES_HOST", "localhost"),
			Port:     readInt("PHOTON_POSTGRES_PORT", 5432),
			User:     getEnv("PHOTON_POSTGRES_USER", "photon"),
			Password: getEnv("PHOTON_POSTGRES_PASSWORD", "photon"),
			Database: getEnv("PHOTON_POSTGRES_DB", "photon"),
			SSLMode:  getEnv("PHOTON_POSTGRES_SSLMODE", "disable"),
		},
		Redis: RedisConfig{
			Addr:       getEnv("PHOTON_REDIS_ADDR", "localhost:6379"),
			Password:   getEnv("PHOTON_REDIS_PASSWORD", ""),
			DB:         readInt("PHOTON_REDIS_DB", 0),
			QueueKey:   getEnv("PHOTON_REDIS_QUEUE_KEY", "photon:jobs"),
			DLQKey:     getEnv("PHOTON_REDIS_DLQ_KEY", "photon:jobs:dlq"),
			MaxRetries: readInt("PHOTON_REDIS_MAX_RETRIES", 3),
		},
		Storage: StorageConfig{
			Endpoint:           getEnv("PHOTON_STORAGE_ENDPOINT", "localhost:9000"),
			PublicBaseURL:      getEnv("PHOTON_STORAGE_PUBLIC_BASE_URL", ""),
			AccessKey:          getEnv("PHOTON_STORAGE_ACCESS_KEY", "minioadmin"),
			SecretKey:          getEnv("PHOTON_STORAGE_SECRET_KEY", "minioadmin"),
			Bucket:             getEnv("PHOTON_STORAGE_BUCKET", "photon"),
			Region:             getEnv("PHOTON_STORAGE_REGION", "us-east-1"),
			UseSSL:             readBool("PHOTON_STORAGE_USE_SSL", false),
			UploadPresignTTL:   readDuration("PHOTON_STORAGE_UPLOAD_URL_TTL", 15*time.Minute),
			DownloadPresignTTL: readDuration("PHOTON_STORAGE_DOWNLOAD_URL_TTL", 30*time.Minute),
		},
	}

	if parseErr != nil {
		return Config{}, parseErr
	}

	if cfg.Worker.Concurrency <= 0 {
		return Config{}, fmt.Errorf("PHOTON_WORKER_CONCURRENCY must be greater than zero")
	}

	if cfg.Worker.JobTimeout <= 0 {
		return Config{}, fmt.Errorf("PHOTON_WORKER_JOB_TIMEOUT must be greater than zero")
	}

	if cfg.Cleanup.BatchSize <= 0 {
		return Config{}, fmt.Errorf("PHOTON_CLEANUP_BATCH_SIZE must be greater than zero")
	}

	if cfg.Cleanup.Interval <= 0 {
		return Config{}, fmt.Errorf("PHOTON_CLEANUP_INTERVAL must be greater than zero")
	}

	if cfg.Cleanup.JobRetention <= 0 {
		return Config{}, fmt.Errorf("PHOTON_CLEANUP_JOB_RETENTION must be greater than zero")
	}

	if cfg.Cleanup.DLQRetention <= 0 {
		return Config{}, fmt.Errorf("PHOTON_CLEANUP_DLQ_RETENTION must be greater than zero")
	}

	if cfg.API.RateLimit.ClientTTL <= 0 {
		return Config{}, fmt.Errorf("PHOTON_API_RATE_LIMIT_CLIENT_TTL must be greater than zero")
	}

	if strings.TrimSpace(cfg.Redis.QueueKey) == "" {
		return Config{}, fmt.Errorf("PHOTON_REDIS_QUEUE_KEY must not be empty")
	}

	if strings.TrimSpace(cfg.Redis.DLQKey) == "" {
		return Config{}, fmt.Errorf("PHOTON_REDIS_DLQ_KEY must not be empty")
	}

	if cfg.Redis.MaxRetries <= 0 {
		return Config{}, fmt.Errorf("PHOTON_REDIS_MAX_RETRIES must be greater than zero")
	}

	if cfg.Storage.UploadPresignTTL <= 0 {
		return Config{}, fmt.Errorf("PHOTON_STORAGE_UPLOAD_URL_TTL must be greater than zero")
	}

	if cfg.Storage.DownloadPresignTTL <= 0 {
		return Config{}, fmt.Errorf("PHOTON_STORAGE_DOWNLOAD_URL_TTL must be greater than zero")
	}

	return cfg, nil
}

func (c PostgresConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host,
		c.Port,
		c.User,
		c.Password,
		c.Database,
		c.SSLMode,
	)
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}

	return fallback
}

func intFromEnv(key string, fallback int) (int, error) {
	value := strings.TrimSpace(getEnv(key, ""))
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid integer: %w", key, err)
	}

	return parsed, nil
}

func boolFromEnv(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(getEnv(key, ""))
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a valid boolean: %w", key, err)
	}

	return parsed, nil
}

func durationFromEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(getEnv(key, ""))
	if value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", key, err)
	}

	return parsed, nil
}
