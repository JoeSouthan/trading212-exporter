package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestCLI_MissingAPIKey(t *testing.T) {
	// Clear environment variable
	os.Unsetenv("TRADING212_API_KEY")

	cmd := newRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("Expected error due to missing API key, got nil")
	}

	expectedErr := "TRADING212_API_KEY environment variable or --api-key flag must be set"
	if err.Error() != expectedErr {
		t.Errorf("Expected error message %q, got %q", expectedErr, err.Error())
	}
}

func TestCLI_MissingAPISecret(t *testing.T) {
	// Clear environment variable
	os.Unsetenv("TRADING212_API_SECRET")

	cmd := newRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--api-key", "raw-test-key"})

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("Expected error due to missing API secret, got nil")
	}

	expectedErr := "TRADING212_API_SECRET environment variable or --api-secret flag must be set"
	if err.Error() != expectedErr {
		t.Errorf("Expected error message %q, got %q", expectedErr, err.Error())
	}
}

func TestCLI_WithFlagAPIKeyAndDemo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v0/equity/account/summary":
			_, _ = w.Write([]byte(`{"id": 123, "currency": "USD", "totalValue": 100.0, "cash": {}}`))
		case "/api/v0/equity/positions":
			_, _ = w.Write([]byte(`[]`))
		case "/api/v0/equity/pies":
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TRADING212_API_URL", server.URL)
	defer os.Unsetenv("TRADING212_API_URL")

	cmd := newRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--api-key", "test-key-flag", "--api-secret", "test-secret-flag"})

	err := cmd.ExecuteContext(context.Background())
	if err != nil {
		t.Fatalf("Unexpected error executing command: %v. Output/Stderr: %s", err, buf.String())
	}

	output := buf.String()
	if output == "" {
		t.Fatal("Expected JSON output, got empty string")
	}

	if !bytes.Contains(buf.Bytes(), []byte(`"account_id": 123`)) {
		t.Errorf("Expected output to contain account_id 123, got:\n%s", output)
	}
}

func TestCLI_WithApiSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			t.Error("Expected basic auth header")
		}
		if username != "my-key" {
			t.Errorf("Expected username 'my-key', got '%s'", username)
		}
		if password != "my-secret" {
			t.Errorf("Expected password 'my-secret', got '%s'", password)
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v0/equity/account/summary":
			_, _ = w.Write([]byte(`{"id": 456, "currency": "USD", "totalValue": 100.0, "cash": {}}`))
		case "/api/v0/equity/positions":
			_, _ = w.Write([]byte(`[]`))
		case "/api/v0/equity/pies":
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TRADING212_API_URL", server.URL)
	defer os.Unsetenv("TRADING212_API_URL")

	cmd := newRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--api-key", "my-key", "--api-secret", "my-secret"})

	err := cmd.ExecuteContext(context.Background())
	if err != nil {
		t.Fatalf("Unexpected error executing command: %v. Output/Stderr: %s", err, buf.String())
	}

	output := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte(`"account_id": 456`)) {
		t.Errorf("Expected output to contain account_id 456, got:\n%s", output)
	}
}

