package network

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInternetChecker_Check_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewInternetChecker(WithInternetCheckURL(srv.URL))

	result := c.Check(context.Background())
	if result.Status != InternetStatusOK {
		t.Errorf("Check().Status = %q, want %q", result.Status, InternetStatusOK)
	}
	if result.HTTPStatus != 204 {
		t.Errorf("Check().HTTPStatus = %d, want 204", result.HTTPStatus)
	}
	if result.Err != nil {
		t.Errorf("Check().Err = %v, want nil", result.Err)
	}
	if result.Latency <= 0 {
		t.Errorf("Check().Latency = %v, want > 0", result.Latency)
	}
}

func TestInternetChecker_Check_200OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewInternetChecker(WithInternetCheckURL(srv.URL))

	result := c.Check(context.Background())
	if result.Status != InternetStatusOK {
		t.Errorf("Check().Status = %q, want %q", result.Status, InternetStatusOK)
	}
	if result.HTTPStatus != 200 {
		t.Errorf("Check().HTTPStatus = %d, want 200", result.HTTPStatus)
	}
}

func TestInternetChecker_Check_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewInternetChecker(WithInternetCheckURL(srv.URL))

	result := c.Check(context.Background())
	if result.Status != InternetStatusOffline {
		t.Errorf("Check().Status = %q, want %q", result.Status, InternetStatusOffline)
	}
	if result.HTTPStatus != 500 {
		t.Errorf("Check().HTTPStatus = %d, want 500", result.HTTPStatus)
	}
	if result.Err == nil {
		t.Error("Check().Err should be non-nil for server error")
	}
	if !strings.Contains(result.Err.Error(), "unexpected status 500") {
		t.Errorf("Check().Err = %v, want to contain 'unexpected status 500'", result.Err)
	}
}

func TestInternetChecker_Check_Unreachable(t *testing.T) {
	c := NewInternetChecker(
		WithInternetCheckURL("http://127.0.0.1:1"), // nothing listening
		WithInternetCheckTimeout(100*time.Millisecond),
	)

	result := c.Check(context.Background())
	if result.Status != InternetStatusOffline {
		t.Errorf("Check().Status = %q, want %q", result.Status, InternetStatusOffline)
	}
	if result.Err == nil {
		t.Error("Check().Err should be non-nil for unreachable host")
	}
	if result.HTTPStatus != 0 {
		t.Errorf("Check().HTTPStatus = %d, want 0 for connection failure", result.HTTPStatus)
	}
	if result.Latency <= 0 {
		t.Errorf("Check().Latency = %v, want > 0 even on failure", result.Latency)
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

	result := c.Check(ctx)
	if result.Status != InternetStatusOffline {
		t.Errorf("Check().Status with cancelled ctx = %q, want %q", result.Status, InternetStatusOffline)
	}
	if result.Err == nil {
		t.Error("Check().Err should be non-nil for cancelled context")
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
	ch := make(chan CheckResult, 1)

	go c.Run(ctx, ch)

	select {
	case result := <-ch:
		if result.Status != InternetStatusOK {
			t.Errorf("immediate check = %q, want %q", result.Status, InternetStatusOK)
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
	ch := make(chan CheckResult, 10)

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

func TestInternetChecker_Check_InvalidURL(t *testing.T) {
	c := NewInternetChecker(WithInternetCheckURL("://bad-url"))

	result := c.Check(context.Background())
	if result.Status != InternetStatusOffline {
		t.Errorf("Check().Status with invalid URL = %q, want %q", result.Status, InternetStatusOffline)
	}
	if result.Err == nil {
		t.Error("Check().Err should be non-nil for invalid URL")
	}
	if !strings.Contains(result.Err.Error(), "build request") {
		t.Errorf("Check().Err = %v, want to contain 'build request'", result.Err)
	}
}

func TestInternetChecker_Check_UsesGET(t *testing.T) {
	var method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewInternetChecker(WithInternetCheckURL(srv.URL))
	c.Check(context.Background())

	if method != http.MethodGet {
		t.Errorf("Check() used method %q, want %q", method, http.MethodGet)
	}
}

func TestInternetChecker_Check_3xxIsOffline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer srv.Close()

	// Disable redirect following so we see the 3xx directly
	c := NewInternetChecker(
		WithInternetCheckURL(srv.URL),
		WithHTTPClient(&http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}),
	)

	result := c.Check(context.Background())
	if result.Status != InternetStatusOffline {
		t.Errorf("Check().Status with 301 = %q, want %q", result.Status, InternetStatusOffline)
	}
	if result.HTTPStatus != 301 {
		t.Errorf("Check().HTTPStatus = %d, want 301", result.HTTPStatus)
	}
}

func TestInternetChecker_Check_403IsOffline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewInternetChecker(WithInternetCheckURL(srv.URL))

	result := c.Check(context.Background())
	if result.Status != InternetStatusOffline {
		t.Errorf("Check().Status with 403 = %q, want %q", result.Status, InternetStatusOffline)
	}
	if result.HTTPStatus != 403 {
		t.Errorf("Check().HTTPStatus = %d, want 403", result.HTTPStatus)
	}
}

func TestInternetChecker_Run_StopsOnCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewInternetChecker(
		WithInternetCheckURL(srv.URL),
		WithInternetCheckInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan CheckResult, 10)

	done := make(chan struct{})
	go func() {
		c.Run(ctx, ch)
		close(done)
	}()

	// Drain the immediate check
	<-ch

	cancel()

	select {
	case <-done:
		// Run returned — good
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after context cancel")
	}
}

func TestInternetChecker_Run_FullChannelDoesNotBlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewInternetChecker(
		WithInternetCheckURL(srv.URL),
		WithInternetCheckInterval(10*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	// Buffered channel of size 1 — will fill up fast
	ch := make(chan CheckResult, 1)

	done := make(chan struct{})
	go func() {
		c.Run(ctx, ch)
		close(done)
	}()

	// Let it run for a bit with a full channel — should not deadlock
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run deadlocked with full channel")
	}
}

func TestInternetChecker_Check_SlowServerTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewInternetChecker(
		WithInternetCheckURL(srv.URL),
		WithInternetCheckTimeout(50*time.Millisecond),
	)

	result := c.Check(context.Background())
	if result.Status != InternetStatusOffline {
		t.Errorf("Check().Status with slow server = %q, want %q", result.Status, InternetStatusOffline)
	}
	if result.Err == nil {
		t.Error("Check().Err should be non-nil for timeout")
	}
}

func TestInternetChecker_AllOptions(t *testing.T) {
	c := NewInternetChecker(
		WithInternetCheckURL("http://example.com"),
		WithInternetCheckInterval(10*time.Minute),
		WithInternetCheckTimeout(30*time.Second),
		WithInternetCheckVerbose(true),
	)

	if c.url != "http://example.com" {
		t.Errorf("url = %q, want %q", c.url, "http://example.com")
	}
	if c.interval != 10*time.Minute {
		t.Errorf("interval = %v, want 10m", c.interval)
	}
	if c.timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", c.timeout)
	}
	if !c.verbose {
		t.Error("verbose = false, want true")
	}
}

func TestInternetChecker_Check_LatencyPopulatedOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewInternetChecker(WithInternetCheckURL(srv.URL))

	result := c.Check(context.Background())
	if result.Latency < 5*time.Millisecond {
		t.Errorf("Check().Latency = %v, want >= 5ms", result.Latency)
	}
}

func TestInternetChecker_Check_ErrorWrapping(t *testing.T) {
	// Connection refused should include "http request" wrapper
	c := NewInternetChecker(
		WithInternetCheckURL("http://127.0.0.1:1"),
		WithInternetCheckTimeout(100*time.Millisecond),
	)

	result := c.Check(context.Background())
	if result.Err == nil {
		t.Fatal("expected error for unreachable host")
	}
	if !strings.Contains(result.Err.Error(), "http request") {
		t.Errorf("error should be wrapped with 'http request', got: %v", result.Err)
	}
}
