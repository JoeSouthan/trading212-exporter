# Trading212 Holdings Exporter

A command-line tool written in Go to export your Trading212 portfolio holdings reconciled with your custom Pies. The tool parses your holdings, associates them with the Pies they belong to, calculates the quantities held outside of Pies, and outputs a clean JSON structure to `stdout`.

Note: vibe coded for my own use, feel free to fork and modify as needed. Contributions are welcome!

## Features

- **OpenAPI Client**: Auto-generated type-safe client using `oapi-codegen`.
- **Holding Reconciliation**: Computes total quantity, quantity held inside Pies (identifying which Pies and current/expected target allocations), and quantity held outside Pies (free/individual positions).
- **Dynamic Pacing & Rate Limit Resilience**:
  - Dynamically throttles requests based on `X-RateLimit-*` headers returned by Trading212.
  - Automatically retries on `HTTP 429 Too Many Requests` responses using `Retry-After` headers and exponential backoff.
- **Graceful Shutdown**: Native OS signal handling (`SIGINT`/`SIGTERM`) cancels active requests cleanly.
- **Clean JSON Serialization**: Handles slice structures to guarantee empty arrays serialize to `[]` instead of `null`.

## Getting Started

### Prerequisites

- Go 1.21 or later
- A Trading212 API Key and API Secret pair (with `account`, `portfolio`, and `pies` read permissions)

### Installation

Clone this repository:

```bash
git clone github.com/joesouthan/trading212-exporter
cd trading212-exporter
```

Build the binary:

```bash
make build
```

### Usage

Set your API Key and API Secret as environment variables and run the exporter:

```bash
export TRADING212_API_KEY="your-api-key-here"
export TRADING212_API_SECRET="your-api-secret-here"

# Query the Demo environment (default)
./trading212-exporter

# Query the Live environment
./trading212-exporter --live
```

You can instead create a `.env` file in the directory where you run the command (copy `.env.example` as a starting point). It is loaded automatically, without replacing environment variables already set in your shell:

```dotenv
TRADING212_API_KEY="your-api-key-here"
TRADING212_API_SECRET="your-api-secret-here"
```

CLI flags take precedence over both shell and `.env` values.

Alternatively, you can pass them via CLI flags:

```bash
./trading212-exporter --api-key "your-api-key" --api-secret "your-api-secret"
```

You can cleanly redirect the output to a JSON file:

```bash
./trading212-exporter > portfolio.json
```

## Makefile Commands

- `make build`: Compile the binary.
- `make test`: Run all package unit tests.
- `make generate`: Regenerate the OpenAPI client code from `api.yaml`.
- `make update-schema`: Download the latest Trading 212 OpenAPI schema and regenerate the client.
- `make clean`: Remove the compiled binary.

## JSON Output Structure

The output is written to `stdout` in the following format:

```json
{
  "account_id": 123456,
  "currency": "GBP",
  "total_value": 10000.0,
  "cash": {
    "available_to_trade": 500.0,
    "in_pies": 200.0,
    "reserved_for_orders": 0.0
  },
  "holdings": [
    {
      "ticker": "AAPL_US_EQ",
      "name": "Apple Inc.",
      "isin": "US0378331005",
      "currency": "USD",
      "total_quantity": 10.0,
      "quantity_in_pies": 6.0,
      "quantity_not_in_pies": 4.0,
      "average_price": 150.0,
      "current_price": 160.0,
      "total_cost": 1500.0,
      "current_value": 1600.0,
      "unrealized_pnl": 100.0,
      "pies": [
        {
          "pie_id": 9876,
          "pie_name": "My Tech Pie",
          "quantity": 6.0,
          "current_share": 0.5,
          "expected_share": 0.5
        }
      ]
    }
  ],
  "pies": [
    {
      "pie_id": 9876,
      "name": "My Tech Pie",
      "cash": 200.0,
      "total_value": 1000.0,
      "unrealized_pnl": 50.0,
      "status": "ON_TRACK"
    }
  ]
}
```
