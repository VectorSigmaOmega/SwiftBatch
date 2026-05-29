package config

import (
	"strings"
	"testing"
)

func TestLoadRejectsInvalidIntegerEnv(t *testing.T) {
	t.Setenv("PHOTON_WORKER_CONCURRENCY", "many")

	_, err := Load("worker")
	if err == nil {
		t.Fatal("Load() error = nil, want invalid integer error")
	}

	if !strings.Contains(err.Error(), "PHOTON_WORKER_CONCURRENCY") {
		t.Fatalf("Load() error = %q, want PHOTON_WORKER_CONCURRENCY context", err)
	}
}

func TestLoadRejectsInvalidBooleanEnv(t *testing.T) {
	t.Setenv("PHOTON_STORAGE_USE_SSL", "sometimes")

	_, err := Load("api")
	if err == nil {
		t.Fatal("Load() error = nil, want invalid boolean error")
	}

	if !strings.Contains(err.Error(), "PHOTON_STORAGE_USE_SSL") {
		t.Fatalf("Load() error = %q, want PHOTON_STORAGE_USE_SSL context", err)
	}
}

func TestLoadRejectsInvalidDurationEnv(t *testing.T) {
	t.Setenv("PHOTON_CLEANUP_INTERVAL", "later")

	_, err := Load("cleanup")
	if err == nil {
		t.Fatal("Load() error = nil, want invalid duration error")
	}

	if !strings.Contains(err.Error(), "PHOTON_CLEANUP_INTERVAL") {
		t.Fatalf("Load() error = %q, want PHOTON_CLEANUP_INTERVAL context", err)
	}
}

func TestLoadRejectsNonPositiveRetryCount(t *testing.T) {
	t.Setenv("PHOTON_REDIS_MAX_RETRIES", "0")

	_, err := Load("worker")
	if err == nil {
		t.Fatal("Load() error = nil, want non-positive retry count error")
	}

	if !strings.Contains(err.Error(), "PHOTON_REDIS_MAX_RETRIES") {
		t.Fatalf("Load() error = %q, want PHOTON_REDIS_MAX_RETRIES context", err)
	}
}
