package exporter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/joesouthan/trading212-exporter/pkg/client"
	"github.com/joesouthan/trading212-exporter/pkg/gen"
)

func TestExporter_Export(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v0/equity/account/summary":
			_, _ = w.Write([]byte(`{
				"id": 123456,
				"currency": "GBP",
				"totalValue": 10000.0,
				"cash": {
					"availableToTrade": 500.0,
					"inPies": 200.0,
					"reservedForOrders": 0.0
				}
			}`))
		case "/api/v0/equity/positions":
			_, _ = w.Write([]byte(`[
				{
					"instrument": {
						"ticker": "AAPL_US_EQ",
						"name": "Apple Inc.",
						"isin": "US0378331005",
						"currency": "USD"
					},
					"quantity": 10.0,
					"quantityInPies": 6.0,
					"averagePricePaid": 150.0,
					"currentPrice": 160.0,
					"walletImpact": {
						"totalCost": 1500.0,
						"currentValue": 1600.0,
						"unrealizedProfitLoss": 100.0
					}
				}
			]`))
		case "/api/v0/equity/pies":
			_, _ = w.Write([]byte(`[
				{
					"id": 9876,
					"cash": 200.0,
					"status": "ON_TRACK",
					"result": {
						"priceAvgValue": 1000.0,
						"priceAvgResult": 50.0
					}
				}
			]`))
		case "/api/v0/equity/pies/9876":
			_, _ = w.Write([]byte(`{
				"settings": {
					"id": 9876,
					"name": "My Tech Pie"
				},
				"instruments": [
					{
						"ticker": "AAPL_US_EQ",
						"ownedQuantity": 6.0,
						"currentShare": 0.5,
						"expectedShare": 0.5
					}
				]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Initialize wrapper client targeting mock server
	httpClient := &http.Client{
		Transport: &client.PacedTransport{
			ApiKey: "test",
		},
	}
	genClient, _ := gen.NewClient(server.URL, gen.WithHTTPClient(httpClient))
	c := &client.Client{GenClient: genClient}

	exp := NewExporter(c)
	report, err := exp.Export(context.Background())
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if report.AccountID != 123456 {
		t.Errorf("Expected AccountID 123456, got %d", report.AccountID)
	}
	if report.Currency != "GBP" {
		t.Errorf("Expected Currency GBP, got %s", report.Currency)
	}

	if len(report.Holdings) != 1 {
		t.Fatalf("Expected 1 holding, got %d", len(report.Holdings))
	}

	h := report.Holdings[0]
	if h.Ticker != "AAPL_US_EQ" {
		t.Errorf("Expected Ticker AAPL_US_EQ, got %s", h.Ticker)
	}
	if h.QuantityNotInPies != 4.0 {
		t.Errorf("Expected QuantityNotInPies to be 4.0 (10.0 - 6.0), got %f", h.QuantityNotInPies)
	}

	if len(h.Pies) != 1 {
		t.Fatalf("Expected AAPL to be in 1 pie, got %d", len(h.Pies))
	}

	p := h.Pies[0]
	if p.PieID != 9876 {
		t.Errorf("Expected PieID 9876, got %d", p.PieID)
	}
	if p.PieName != "My Tech Pie" {
		t.Errorf("Expected PieName 'My Tech Pie', got '%s'", p.PieName)
	}
	if p.Quantity != 6.0 {
		t.Errorf("Expected quantity in pie to be 6.0, got %f", p.Quantity)
	}

	// Ensure standard JSON serialization succeeds
	_, err = json.Marshal(report)
	if err != nil {
		t.Fatalf("Failed to marshal report: %v", err)
	}
}

func TestExporter_Export_StatusErrorDoesNotIncludeResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid credentials"}`))
	}))
	defer server.Close()

	httpClient := &http.Client{
		Transport: &client.PacedTransport{ApiKey: "test"},
	}
	genClient, _ := gen.NewClient(server.URL, gen.WithHTTPClient(httpClient))
	exp := NewExporter(&client.Client{GenClient: genClient})

	_, err := exp.Export(context.Background())
	if err == nil {
		t.Fatal("expected status error, got nil")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "unexpected status fetching account summary: 401") {
		t.Errorf("expected status code in error, got: %s", errStr)
	}
	if strings.Contains(errStr, "invalid credentials") {
		t.Errorf("status error exposed response body: %s", errStr)
	}
}

func TestExporter_Export_PieDetailsParseErrorIncludesBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v0/equity/account/summary":
			_, _ = w.Write([]byte(`{"id": 123456, "currency": "GBP", "totalValue": 10000.0, "cash": {}}`))
		case "/api/v0/equity/positions":
			_, _ = w.Write([]byte(`[]`))
		case "/api/v0/equity/pies":
			_, _ = w.Write([]byte(`[{"id": 9876}]`))
		case "/api/v0/equity/pies/9876":
			_, _ = w.Write([]byte(`{"settings": {"id": 9876, "name": "My Tech Pie", "creationDate":`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	httpClient := &http.Client{
		Transport: &client.PacedTransport{
			ApiKey: "test",
		},
	}
	genClient, _ := gen.NewClient(server.URL, gen.WithHTTPClient(httpClient))
	c := &client.Client{GenClient: genClient}

	exp := NewExporter(c)
	_, err := exp.Export(context.Background())
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "failed to parse pie details for 9876") {
		t.Fatalf("expected pie-details parse error, got: %s", errStr)
	}
	if !strings.Contains(errStr, "body:") {
		t.Fatalf("expected error to include body, got: %s", errStr)
	}
	if !strings.Contains(errStr, "\"creationDate\":") {
		t.Fatalf("expected response payload in error body, got: %s", errStr)
	}
}
