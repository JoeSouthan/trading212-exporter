package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joesouthan/trading212-exporter/pkg/gen"
)

type RateLimiter struct {
	mu        sync.Mutex
	remaining int
	reset     time.Time
}

func (rl *RateLimiter) WaitIfLimitReached(ctx context.Context) error {
	rl.mu.Lock()
	if rl.remaining > 0 || !time.Now().Before(rl.reset) {
		rl.mu.Unlock()
		return nil
	}
	waitDuration := time.Until(rl.reset)
	rl.mu.Unlock()

	slog.InfoContext(ctx, "rate limit reached, throttling", "wait", waitDuration.Round(time.Millisecond))

	timer := time.NewTimer(waitDuration)
	defer timer.Stop()
	select {
	case <-timer.C:
		slog.InfoContext(ctx, "throttle wait complete, resuming")
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (rl *RateLimiter) UpdateFromHeaders(headers http.Header) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	remStr := headers.Get("X-RateLimit-Remaining")
	resetStr := headers.Get("X-RateLimit-Reset")

	if remStr != "" {
		if val, err := strconv.Atoi(remStr); err == nil {
			rl.remaining = val
		}
	}

	if resetStr != "" {
		if sec, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
			rl.reset = time.Unix(sec, 0)
		}
	}

	if remStr != "" || resetStr != "" {
		slog.Default().Debug("rate limit state updated", "remaining", rl.remaining, "reset_in", time.Until(rl.reset).Round(time.Second))
	}
}

type PacedTransport struct {
	ApiKey    string
	ApiSecret string
	Transport http.RoundTripper
	Limiter   *RateLimiter
	Logger    *slog.Logger
}

func (t *PacedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.Limiter != nil {
		if err := t.Limiter.WaitIfLimitReached(req.Context()); err != nil {
			return nil, err
		}
	}

	reqCopy := req.Clone(req.Context())
	apiKey := strings.TrimSpace(t.ApiKey)
	apiSecret := strings.TrimSpace(t.ApiSecret)
	if apiKey != "" {
		reqCopy.SetBasicAuth(apiKey, apiSecret)
	}

	transport := t.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}

	logger.InfoContext(req.Context(), "→ request", "method", req.Method, "url", req.URL.String())

	var resp *http.Response
	var err error
	maxRetries := 5
	backoff := 1 * time.Second

	for i := 0; i < maxRetries; i++ {
		resp, err = transport.RoundTrip(reqCopy)
		if err != nil {
			// Don't retry if the context was cancelled.
			if reqCopy.Context().Err() != nil {
				return nil, err
			}
			// Retry transient network errors (timeouts, connection resets).
			var netErr net.Error
			if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) { //nolint:staticcheck
				if i == maxRetries-1 {
					return nil, err
				}
				logger.WarnContext(reqCopy.Context(), "transient error, retrying", "attempt", i+1, "sleep", backoff, "error", err)
				if err := sleepWithContext(reqCopy.Context(), backoff); err != nil {
					return nil, err
				}
				backoff *= 2
				continue
			}
			return nil, err
		}

		if t.Limiter != nil {
			t.Limiter.UpdateFromHeaders(resp.Header)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			if i == maxRetries-1 {
				break
			}
			resp.Body.Close()
			retryAfterStr := resp.Header.Get("Retry-After")
			sleepDur := backoff
			if retryAfterStr != "" {
				if sec, err := strconv.Atoi(retryAfterStr); err == nil {
					sleepDur = time.Duration(sec) * time.Second
				}
			}

			logger.WarnContext(reqCopy.Context(), "rate limited, retrying", "attempt", i+1, "sleep", sleepDur)

			if err := sleepWithContext(reqCopy.Context(), sleepDur); err != nil {
				return nil, err
			}
			backoff *= 2
			continue
		}

		if resp.StatusCode >= 500 {
			if i == maxRetries-1 {
				break
			}
			resp.Body.Close()
			logger.WarnContext(reqCopy.Context(), "server error, retrying", "attempt", i+1, "status", resp.StatusCode, "sleep", backoff)
			if err := sleepWithContext(reqCopy.Context(), backoff); err != nil {
				return nil, err
			}
			backoff *= 2
			continue
		}

		break
	}

	logger.InfoContext(req.Context(), "← response", "method", req.Method, "status", resp.StatusCode, "url", req.URL.String())

	return resp, nil
}

type Client struct {
	GenClient *gen.Client
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func NewClient(apiKey, apiSecret string, isLive bool, timeout time.Duration) (*Client, error) {
	baseURL := "https://demo.trading212.com"
	if isLive {
		baseURL = "https://live.trading212.com"
	}
	if override := os.Getenv("TRADING212_API_URL"); override != "" {
		baseURL = override
	}

	apiKey = strings.TrimSpace(apiKey)
	apiSecret = strings.TrimSpace(apiSecret)
	if apiKey == "" {
		return nil, fmt.Errorf("API Key cannot be empty")
	}
	if apiSecret == "" {
		return nil, fmt.Errorf("API Secret cannot be empty")
	}

	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	httpClient := &http.Client{
		Timeout: timeout,
		Transport: &PacedTransport{
			ApiKey:    apiKey,
			ApiSecret: apiSecret,
			Limiter:   &RateLimiter{remaining: 10, reset: time.Now()},
		},
	}

	genClient, err := gen.NewClient(baseURL, gen.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}
	return &Client{GenClient: genClient}, nil
}
