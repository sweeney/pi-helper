package network

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInternetChecker_Check(t *testing.T) {
	tests := []struct {
		name           string
		handler        http.HandlerFunc
		clientOverride *http.Client
		wantStatus     string
		wantHTTPStatus int
		wantErrSubstr  string // empty means expect nil error
	}{
		{
			name:           "204 No Content is OK",
			handler:        func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) },
			wantStatus:     InternetStatusOK,
			wantHTTPStatus: 204,
		},
		{
			name:           "200 OK is OK",
			handler:        func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) },
			wantStatus:     InternetStatusOK,
			wantHTTPStatus: 200,
		},
		{
			name:           "500 is offline",
			handler:        func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
			wantStatus:     InternetStatusOffline,
			wantHTTPStatus: 500,
			wantErrSubstr:  "unexpected status 500",
		},
		{
			name:           "403 is offline",
			handler:        func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) },
			wantStatus:     InternetStatusOffline,
			wantHTTPStatus: 403,
			wantErrSubstr:  "unexpected status 403",
		},
		{
			name:    "301 is offline (no redirect follow)",
			handler: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusMovedPermanently) },
			clientOverride: &http.Client{
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				},
			},
			wantStatus:     InternetStatusOffline,
			wantHTTPStatus: 301,
			wantErrSubstr:  "unexpected status 301",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			opts := []InternetCheckerOption{WithInternetCheckURL(srv.URL)}
			if tt.clientOverride != nil {
				opts = append(opts, WithHTTPClient(tt.clientOverride))
			}
			c := NewInternetChecker(opts...)

			result := c.Check(context.Background())

			if result.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", result.Status, tt.wantStatus)
			}
			if result.HTTPStatus != tt.wantHTTPStatus {
				t.Errorf("HTTPStatus = %d, want %d", result.HTTPStatus, tt.wantHTTPStatus)
			}
			if tt.wantErrSubstr == "" && result.Err != nil {
				t.Errorf("Err = %v, want nil", result.Err)
			}
			if tt.wantErrSubstr != "" {
				if result.Err == nil {
					t.Fatalf("Err = nil, want error containing %q", tt.wantErrSubstr)
				}
				if got := result.Err.Error(); !contains(got, tt.wantErrSubstr) {
					t.Errorf("Err = %q, want substring %q", got, tt.wantErrSubstr)
				}
			}
			if result.Latency <= 0 {
				t.Errorf("Latency = %v, want > 0", result.Latency)
			}
		})
	}
}

func TestInternetChecker_Check_ConnectionFailure(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		wantErrSubstr string
	}{
		{
			name:          "unreachable host",
			url:           "http://127.0.0.1:1",
			wantErrSubstr: "http request",
		},
		{
			name:          "invalid URL",
			url:           "://bad-url",
			wantErrSubstr: "build request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewInternetChecker(
				WithInternetCheckURL(tt.url),
				WithInternetCheckTimeout(100*time.Millisecond),
			)

			result := c.Check(context.Background())

			if result.Status != InternetStatusOffline {
				t.Errorf("Status = %q, want %q", result.Status, InternetStatusOffline)
			}
			if result.HTTPStatus != 0 {
				t.Errorf("HTTPStatus = %d, want 0 for connection failure", result.HTTPStatus)
			}
			if result.Err == nil {
				t.Fatalf("Err = nil, want error containing %q", tt.wantErrSubstr)
			}
			if got := result.Err.Error(); !contains(got, tt.wantErrSubstr) {
				t.Errorf("Err = %q, want substring %q", got, tt.wantErrSubstr)
			}
		})
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
		t.Errorf("Status = %q, want %q", result.Status, InternetStatusOffline)
	}
	if result.Err == nil {
		t.Error("Err should be non-nil for cancelled context")
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
		t.Errorf("Status = %q, want %q", result.Status, InternetStatusOffline)
	}
	if result.Err == nil {
		t.Error("Err should be non-nil for timeout")
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
		t.Errorf("method = %q, want %q", method, http.MethodGet)
	}
}

func TestInternetChecker_Run_ImmediateCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewInternetChecker(
		WithInternetCheckURL(srv.URL),
		WithInternetCheckInterval(time.Hour),
	)

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan CheckResult, 1)

	go c.Run(ctx, ch)

	select {
	case result := <-ch:
		if result.Status != InternetStatusOK {
			t.Errorf("immediate check Status = %q, want %q", result.Status, InternetStatusOK)
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

	<-ch
	cancel()

	select {
	case <-done:
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
	ch := make(chan CheckResult, 1)

	done := make(chan struct{})
	go func() {
		c.Run(ctx, ch)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run deadlocked with full channel")
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

func TestInternetChecker_AllOptions(t *testing.T) {
	custom := &http.Client{Timeout: 42 * time.Second}
	c := NewInternetChecker(
		WithInternetCheckURL("http://example.com"),
		WithInternetCheckInterval(10*time.Minute),
		WithInternetCheckTimeout(30*time.Second),
		WithInternetCheckVerbose(true),
		WithHTTPClient(custom),
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
	if c.client != custom {
		t.Error("expected custom HTTP client to be used")
	}
}

// contains is a helper to avoid importing strings in the test file.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
