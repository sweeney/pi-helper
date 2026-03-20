package network

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Default internet check endpoint. Google's connectivity check returns 204
// with no body, making it lightweight and extremely reliable.
const DefaultInternetCheckURL = "https://connectivitycheck.goog/generate_204"

// CheckResult holds the outcome of a single internet connectivity check.
type CheckResult struct {
	Status     string        // InternetStatusOK, InternetStatusOffline
	Latency    time.Duration // round-trip time of the HTTP request
	HTTPStatus int           // HTTP status code (0 if request failed)
	Err        error         // nil on success, the underlying error on failure
}

// InternetChecker periodically performs an HTTP request to verify end-to-end
// internet connectivity. Results are delivered via a channel.
type InternetChecker struct {
	url      string
	interval time.Duration
	timeout  time.Duration
	client   *http.Client
	verbose  bool
}

// InternetCheckerOption configures an InternetChecker.
type InternetCheckerOption func(*InternetChecker)

// WithInternetCheckURL sets the URL to check.
func WithInternetCheckURL(url string) InternetCheckerOption {
	return func(c *InternetChecker) {
		c.url = url
	}
}

// WithInternetCheckInterval sets the check interval.
func WithInternetCheckInterval(d time.Duration) InternetCheckerOption {
	return func(c *InternetChecker) {
		c.interval = d
	}
}

// WithInternetCheckTimeout sets the HTTP request timeout.
func WithInternetCheckTimeout(d time.Duration) InternetCheckerOption {
	return func(c *InternetChecker) {
		c.timeout = d
	}
}

// WithInternetCheckVerbose enables verbose logging (unused here but passed through).
func WithInternetCheckVerbose(v bool) InternetCheckerOption {
	return func(c *InternetChecker) {
		c.verbose = v
	}
}

// WithHTTPClient sets a custom HTTP client (for testing).
func WithHTTPClient(client *http.Client) InternetCheckerOption {
	return func(c *InternetChecker) {
		c.client = client
	}
}

// NewInternetChecker creates a new InternetChecker.
func NewInternetChecker(opts ...InternetCheckerOption) *InternetChecker {
	c := &InternetChecker{
		url:      DefaultInternetCheckURL,
		interval: 5 * time.Minute,
		timeout:  10 * time.Second,
	}

	for _, opt := range opts {
		opt(c)
	}

	if c.client == nil {
		c.client = &http.Client{Timeout: c.timeout}
	}

	return c
}

// Check performs a single internet connectivity check and returns a detailed result.
func (c *InternetChecker) Check(ctx context.Context) CheckResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return CheckResult{Status: InternetStatusOffline, Err: fmt.Errorf("build request: %w", err)}
	}

	start := time.Now()
	resp, err := c.client.Do(req)
	latency := time.Since(start)

	if err != nil {
		return CheckResult{Status: InternetStatusOffline, Latency: latency, Err: fmt.Errorf("http request: %w", err)}
	}
	resp.Body.Close()

	// Accept any 2xx status as success
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return CheckResult{Status: InternetStatusOK, Latency: latency, HTTPStatus: resp.StatusCode}
	}
	return CheckResult{
		Status:     InternetStatusOffline,
		Latency:    latency,
		HTTPStatus: resp.StatusCode,
		Err:        fmt.Errorf("unexpected status %d", resp.StatusCode),
	}
}

// Run starts the periodic check loop. It sends a CheckResult on resultCh
// each time a check completes. It performs an immediate check on startup.
// It blocks until ctx is cancelled.
func (c *InternetChecker) Run(ctx context.Context, resultCh chan<- CheckResult) {
	// Immediate check on start
	resultCh <- c.Check(ctx)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result := c.Check(ctx)
			select {
			case resultCh <- result:
			default:
			}
		}
	}
}
