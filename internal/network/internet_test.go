package network

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInternetChecker_Check_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewInternetChecker(
		WithInternetCheckURL(srv.URL),
	)

	status := c.Check(context.Background())
	if status != InternetStatusOK {
		t.Errorf("Check() = %q, want %q", status, InternetStatusOK)
	}
}

func TestInternetChecker_Check_200OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewInternetChecker(WithInternetCheckURL(srv.URL))

	status := c.Check(context.Background())
	if status != InternetStatusOK {
		t.Errorf("Check() = %q, want %q", status, InternetStatusOK)
	}
}

func TestInternetChecker_Check_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewInternetChecker(WithInternetCheckURL(srv.URL))

	status := c.Check(context.Background())
	if status != InternetStatusOffline {
		t.Errorf("Check() = %q, want %q", status, InternetStatusOffline)
	}
}

func TestInternetChecker_Check_Unreachable(t *testing.T) {
	c := NewInternetChecker(
		WithInternetCheckURL("http://127.0.0.1:1"), // nothing listening
		WithInternetCheckTimeout(100*time.Millisecond),
	)

	status := c.Check(context.Background())
	if status != InternetStatusOffline {
		t.Errorf("Check() = %q, want %q", status, InternetStatusOffline)
	}
}

func TestInternetChecker_Check_CancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewInternetChecker(WithInternetCheckURL(srv.URL))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	status := c.Check(ctx)
	if status != InternetStatusOffline {
		t.Errorf("Check() with cancelled ctx = %q, want %q", status, InternetStatusOffline)
	}
}

func TestInternetChecker_Run_ImmediateCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewInternetChecker(
		WithInternetCheckURL(srv.URL),
		WithInternetCheckInterval(time.Hour), // long interval so only the immediate check fires
	)

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan string, 1)

	go c.Run(ctx, ch)

	select {
	case status := <-ch:
		if status != InternetStatusOK {
			t.Errorf("immediate check = %q, want %q", status, InternetStatusOK)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for immediate check result")
	}

	cancel()
}

func TestInternetChecker_Run_PeriodicCheck(t *testing.T) {
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewInternetChecker(
		WithInternetCheckURL(srv.URL),
		WithInternetCheckInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan string, 10)

	go c.Run(ctx, ch)

	// Wait for at least 3 results (immediate + 2 periodic)
	for i := 0; i < 3; i++ {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for check result %d", i+1)
		}
	}

	cancel()

	if count < 3 {
		t.Errorf("expected at least 3 HTTP requests, got %d", count)
	}
}

func TestInternetChecker_Defaults(t *testing.T) {
	c := NewInternetChecker()

	if c.url != DefaultInternetCheckURL {
		t.Errorf("default URL = %q, want %q", c.url, DefaultInternetCheckURL)
	}
	if c.interval != 5*time.Minute {
		t.Errorf("default interval = %v, want 5m", c.interval)
	}
	if c.timeout != 10*time.Second {
		t.Errorf("default timeout = %v, want 10s", c.timeout)
	}
	if c.client == nil {
		t.Error("client should not be nil")
	}
}

func TestInternetChecker_WithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 42 * time.Second}
	c := NewInternetChecker(WithHTTPClient(custom))

	if c.client != custom {
		t.Error("expected custom HTTP client to be used")
	}
}
