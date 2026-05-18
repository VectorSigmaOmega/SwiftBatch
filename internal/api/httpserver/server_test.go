package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRedirectLegacyHostRedirectsToCanonicalHost(t *testing.T) {
	t.Parallel()

	nextCalled := false
	handler := redirectLegacyHost(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodPost, "https://swiftbatch.abhinash.dev/v1/jobs?view=full", nil)
	request.Host = "swiftbatch.abhinash.dev"

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if nextCalled {
		t.Fatalf("next handler should not be called for legacy host")
	}

	if got, want := recorder.Code, http.StatusPermanentRedirect; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}

	if got, want := recorder.Header().Get("Location"), "https://photon.abhinash.dev/v1/jobs?view=full"; got != want {
		t.Fatalf("location = %q, want %q", got, want)
	}
}

func TestRedirectLegacyHostPassesThroughCanonicalHost(t *testing.T) {
	t.Parallel()

	nextCalled := false
	handler := redirectLegacyHost(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "https://photon.abhinash.dev/", nil)
	request.Host = "photon.abhinash.dev"

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if !nextCalled {
		t.Fatalf("next handler should be called for canonical host")
	}

	if got, want := recorder.Code, http.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}
