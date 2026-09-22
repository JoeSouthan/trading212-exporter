package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joesouthan/trading212-exporter/pkg/client"
	"github.com/joesouthan/trading212-exporter/pkg/exporter"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := newRootCommand().ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	var live bool
	var apiKey string
	var apiSecret string
	var outputFile string
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:           "trading212-exporter",
		Short:         "Export Trading212 holdings reconciled with Pies",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadDotenv(); err != nil {
				return err
			}

			if apiKey == "" {
				apiKey = os.Getenv("TRADING212_API_KEY")
			}
			if apiKey == "" {
				return fmt.Errorf("TRADING212_API_KEY environment variable or --api-key flag must be set")
			}

			if apiSecret == "" {
				apiSecret = os.Getenv("TRADING212_API_SECRET")
			}
			if apiSecret == "" {
				return fmt.Errorf("TRADING212_API_SECRET environment variable or --api-secret flag must be set")
			}

			c, err := client.NewClient(apiKey, apiSecret, live, timeout)
			if err != nil {
				return fmt.Errorf("creating Trading212 client: %w", err)
			}
			exp := exporter.NewExporter(c)

			report, err := exp.Export(cmd.Context())
			if err != nil {
				return fmt.Errorf("exporting holdings: %w", err)
			}

			out := cmd.OutOrStdout()
			if outputFile != "" {
				f, err := os.Create(outputFile)
				if err != nil {
					return fmt.Errorf("opening output file: %w", err)
				}
				defer f.Close()
				out = f
			}

			encoder := json.NewEncoder(out)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(report); err != nil {
				return fmt.Errorf("formatting JSON output: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&live, "live", "l", false, "Use the Trading212 Live environment (default is Demo)")
	cmd.Flags().StringVarP(&apiKey, "api-key", "k", "", "Trading212 API Key (fallback: TRADING212_API_KEY env var)")
	cmd.Flags().StringVarP(&apiSecret, "api-secret", "s", "", "Trading212 API Secret (fallback: TRADING212_API_SECRET env var)")
	cmd.Flags().StringVarP(&outputFile, "output", "o", "", "Write JSON output to this file instead of stdout")
	cmd.Flags().DurationVar(&timeout, "timeout", 60*time.Second, "Per-request HTTP timeout")

	return cmd
}

func loadDotenv() error {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("loading .env file: %w", err)
	}

	return nil
}
