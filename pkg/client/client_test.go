package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPacedTransport_AuthorizationHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		expected := "Basic dGVzdC1rZXk6" // base64("test-key:")
		if auth != expected {
			t.Errorf("Expected Authorization header '%s', got '%s'", expected, auth)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := &PacedTransport{
		ApiKey:    "test-key",
		Transport: http.DefaultTransport,
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer resp.Body.Close()
}

func TestPacedTransport_RetryOn429(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := &PacedTransport{
		ApiKey:    "test-key",
		Transport: http.DefaultTransport,
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if attempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", attempts)
	}
}

func TestRateLimiter(t *testing.T) {
	t.Run("WaitIfLimitReached no wait when remaining > 0", func(t *testing.T) {
		rl := &RateLimiter{
			remaining: 5,
			reset:     time.Now().Add(10 * time.Second),
		}
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		start := time.Now()
		err := rl.WaitIfLimitReached(ctx)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if time.Since(start) > 10*time.Millisecond {
			t.Errorf("Expected no wait, but took %v", time.Since(start))
		}
	})

	t.Run("WaitIfLimitReached waits when limit reached", func(t *testing.T) {
		resetTime := time.Now().Add(50 * time.Millisecond)
		rl := &RateLimiter{
			remaining: 0,
			reset:     resetTime,
		}
		ctx := context.Background()

		start := time.Now()
		err := rl.WaitIfLimitReached(ctx)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		elapsed := time.Since(start)
		if elapsed < 40*time.Millisecond {
			t.Errorf("Expected wait of at least 50ms, but took %v", elapsed)
		}
	})

	t.Run("UpdateFromHeaders parses Unix timestamp", func(t *testing.T) {
		rl := &RateLimiter{}
		headers := http.Header{}
		headers.Set("X-RateLimit-Remaining", "3")

		nowSec := time.Now().Unix()
		resetSec := nowSec + 10
		headers.Set("X-RateLimit-Reset", strconv.FormatInt(resetSec, 10))

		rl.UpdateFromHeaders(headers)

		if rl.remaining != 3 {
			t.Errorf("Expected remaining 3, got %d", rl.remaining)
		}

		expectedReset := time.Unix(resetSec, 0)
		if !rl.reset.Equal(expectedReset) {
			t.Errorf("Expected reset time %v, got %v", expectedReset, rl.reset)
		}
	})
}

func TestNewClient(t *testing.T) {
	t.Run("Demo client", func(t *testing.T) {
		c, err := NewClient("demo-key", "demo-secret", false, 30*time.Second)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if c == nil {
			t.Fatal("Expected client, got nil")
		}
		if c.GenClient.Server != "https://demo.trading212.com/" {
			t.Errorf("Expected demo server URL, got %s", c.GenClient.Server)
		}
	})

	t.Run("Live client", func(t *testing.T) {
		c, err := NewClient("live-key", "live-secret", true, 30*time.Second)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if c == nil {
			t.Fatal("Expected client, got nil")
		}
		if c.GenClient.Server != "https://live.trading212.com/" {
			t.Errorf("Expected live server URL, got %s", c.GenClient.Server)
		}
	})

	t.Run("Empty key error", func(t *testing.T) {
		_, err := NewClient("", "secret", false, 30*time.Second)
		if err == nil {
			t.Fatal("Expected error for empty API key, got nil")
		}
	})

	t.Run("Empty secret error", func(t *testing.T) {
		_, err := NewClient("key", "", false, 30*time.Second)
		if err == nil {
			t.Fatal("Expected error for empty API secret, got nil")
		}
	})
}

func TestPacedTransport_WithSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		expected := "Basic dGVzdC1rZXk6dGVzdC1zZWNyZXQ=" // base64("test-key:test-secret")
		if auth != expected {
			t.Errorf("Expected Authorization header '%s', got '%s'", expected, auth)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := &PacedTransport{
		ApiKey:    "test-key",
		ApiSecret: "test-secret",
		Transport: http.DefaultTransport,
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer resp.Body.Close()
}

func TestPacedTransport_LogsAndPreservesErrorResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "request-123")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid credentials"}`))
	}))
	defer server.Close()

	var logs bytes.Buffer
	transport := &PacedTransport{
		ApiKey:    "test-key",
		ApiSecret: "test-secret",
		Transport: http.DefaultTransport,
		Logger:    slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	if got, want := string(body), `{"error":"invalid credentials"}`; got != want {
		t.Errorf("response body = %q, want %q", got, want)
	}

	logOutput := logs.String()
	if !strings.Contains(logOutput, "HTTP request failed") {
		t.Errorf("expected error diagnostic, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "invalid credentials") {
		t.Errorf("expected response body in diagnostic, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "request-123") {
		t.Errorf("expected response headers in diagnostic, got: %s", logOutput)
	}
}

func TestPacedTransport_HidesErrorResponseBodyAtInfoLevel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid credentials"}`))
	}))
	defer server.Close()

	var logs bytes.Buffer
	transport := &PacedTransport{
		ApiKey:    "test-key",
		ApiSecret: "test-secret",
		Transport: http.DefaultTransport,
		Logger:    slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if strings.Contains(logs.String(), "invalid credentials") {
		t.Errorf("INFO-level logger exposed response body: %s", logs.String())
	}
}

func TestPacedTransport_RetryOn5xx(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := &PacedTransport{
		ApiKey:    "test-key",
		Transport: http.DefaultTransport,
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
}

// mockTimeoutError is a net.Error that reports Timeout() == true.
type mockTimeoutError struct{}

func (e *mockTimeoutError) Error() string   { return "mock timeout" }
func (e *mockTimeoutError) Timeout() bool   { return true }
func (e *mockTimeoutError) Temporary() bool { return true } //nolint:staticcheck

// errTransport returns a net.Error for the first failN calls, then delegates.
type errTransport struct {
	failN    int
	attempts int
	delegate http.RoundTripper
}

func (et *errTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	et.attempts++
	if et.attempts <= et.failN {
		return nil, &net.OpError{Op: "dial", Err: &mockTimeoutError{}}
	}
	return et.delegate.RoundTrip(req)
}

func TestPacedTransport_RetryOnTransientError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	et := &errTransport{failN: 2, delegate: http.DefaultTransport}
	transport := &PacedTransport{
		ApiKey:    "test-key",
		Transport: et,
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if et.attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", et.attempts)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
}

func TestPacedTransport_TransientErrorExhaustsRetries(t *testing.T) {
	et := &errTransport{failN: 10, delegate: http.DefaultTransport}
	transport := &PacedTransport{
		ApiKey:    "test-key",
		Transport: et,
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", "http://127.0.0.1:0", nil)
	_, err := transport.RoundTrip(req)
	if err == nil {
		t.Fatal("Expected error after exhausting retries, got nil")
	}
	if !errors.As(err, new(*net.OpError)) {
		t.Errorf("Expected net.OpError, got %T: %v", err, err)
	}
}
