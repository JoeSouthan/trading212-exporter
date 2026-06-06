package exporter

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/joesouthan/trading212-exporter/pkg/client"
	"github.com/joesouthan/trading212-exporter/pkg/gen"
)

type PieComposition struct {
	PieID         int64   `json:"pie_id"`
	PieName       string  `json:"pie_name"`
	Quantity      float64 `json:"quantity"`
	CurrentShare  float64 `json:"current_share"`
	ExpectedShare float64 `json:"expected_share"`
}

type Holding struct {
	Ticker            string           `json:"ticker"`
	Name              string           `json:"name"`
	ISIN              string           `json:"isin"`
	Currency          string           `json:"currency"`
	TotalQuantity     float64          `json:"total_quantity"`
	QuantityInPies    float64          `json:"quantity_in_pies"`
	QuantityNotInPies float64          `json:"quantity_not_in_pies"`
	AveragePrice      float64          `json:"average_price"`
	CurrentPrice      float64          `json:"current_price"`
	TotalCost         float64          `json:"total_cost"`
	CurrentValue      float64          `json:"current_value"`
	UnrealizedPnL     float64          `json:"unrealized_pnl"`
	Pies              []PieComposition `json:"pies"`
}

type CashSummary struct {
	AvailableToTrade  float64 `json:"available_to_trade"`
	InPies            float64 `json:"in_pies"`
	ReservedForOrders float64 `json:"reserved_for_orders"`
}

type PieSummary struct {
	PieID         int64   `json:"pie_id"`
	Name          string  `json:"name"`
	Cash          float64 `json:"cash"`
	TotalValue    float64 `json:"total_value"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
	Status        string  `json:"status"`
}

type AccountReport struct {
	AccountID  int64        `json:"account_id"`
	Currency   string       `json:"currency"`
	TotalValue float64      `json:"total_value"`
	Cash       CashSummary  `json:"cash"`
	Holdings   []Holding    `json:"holdings"`
	Pies       []PieSummary `json:"pies"`
}

type Exporter struct {
	client *client.Client
}

func NewExporter(c *client.Client) *Exporter {
	return &Exporter{client: c}
}

func formatBodyForError(body []byte) string {
	const maxBodyLen = 4096
	if len(body) <= maxBodyLen {
		return string(body)
	}
	return string(body[:maxBodyLen]) + "...(truncated)"
}

func (e *Exporter) Export(ctx context.Context) (*AccountReport, error) {
	// 1. Fetch Account Summary
	summaryResp, err := e.client.GenClient.GetAccountSummary(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch account summary: %w", err)
	}
	defer summaryResp.Body.Close()
	if summaryResp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(summaryResp.Body)
		return nil, fmt.Errorf("unexpected status fetching account summary: %d, body: %s", summaryResp.StatusCode, string(bodyBytes))
	}
	summary, err := gen.ParseGetAccountSummaryResponse(summaryResp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse account summary: %w", err)
	}
	if summary.JSON200 == nil {
		return nil, fmt.Errorf("account summary payload is nil")
	}

	// 2. Fetch Open Positions
	posResp, err := e.client.GenClient.GetPositions(ctx, &gen.GetPositionsParams{})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch positions: %w", err)
	}
	defer posResp.Body.Close()
	if posResp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(posResp.Body)
		return nil, fmt.Errorf("unexpected status fetching positions: %d, body: %s", posResp.StatusCode, string(bodyBytes))
	}
	positions, err := gen.ParseGetPositionsResponse(posResp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse positions: %w", err)
	}
	if positions.JSON200 == nil {
		return nil, fmt.Errorf("positions payload is nil")
	}

	// 3. Fetch All Pies
	piesResp, err := e.client.GenClient.GetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pies: %w", err)
	}
	defer piesResp.Body.Close()
	if piesResp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(piesResp.Body)
		return nil, fmt.Errorf("unexpected status fetching pies: %d, body: %s", piesResp.StatusCode, string(bodyBytes))
	}
	pies, err := gen.ParseGetAllResponse(piesResp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pies: %w", err)
	}
	if pies.JSON200 == nil {
		return nil, fmt.Errorf("pies payload is nil")
	}

	// Map of Ticker -> list of pies it belongs to
	tickerPies := make(map[string][]PieComposition)
	pieSummaries := []PieSummary{}

	// 4. Fetch Details for each Pie to reconcile holdings
	for _, pie := range *pies.JSON200 {
		if pie.Id == nil {
			continue
		}
		pieID := *pie.Id
		detResp, err := e.client.GenClient.GetDetailed(ctx, pieID)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch details for pie %d: %w", pieID, err)
		}
		bodyBytes, readErr := io.ReadAll(detResp.Body)
		detResp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("failed to read pie details body for %d: %w", pieID, readErr)
		}
		if detResp.StatusCode != 200 {
			return nil, fmt.Errorf("unexpected status fetching pie details for %d: %d, body: %s", pieID, detResp.StatusCode, formatBodyForError(bodyBytes))
		}
		detResp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		detailedPie, err := gen.ParseGetDetailedResponse(detResp)
		detResp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to parse pie details for %d: %w, body: %s", pieID, err, formatBodyForError(bodyBytes))
		}
		if detailedPie.JSON200 == nil {
			return nil, fmt.Errorf("pie details payload is nil for pie %d", pieID)
		}

		pieName := ""
		if detailedPie.JSON200.Settings != nil && detailedPie.JSON200.Settings.Name != nil {
			pieName = *detailedPie.JSON200.Settings.Name
		}

		var pieVal float64
		var piePnL float64
		if pie.Result != nil {
			if pie.Result.PriceAvgValue != nil {
				pieVal = float64(*pie.Result.PriceAvgValue)
			}
			if pie.Result.PriceAvgResult != nil {
				piePnL = float64(*pie.Result.PriceAvgResult)
			}
		}

		var pieCash float64
		if pie.Cash != nil {
			pieCash = float64(*pie.Cash)
		}

		var pieStatus string
		if pie.Status != nil {
			pieStatus = string(*pie.Status)
		}

		pieSummaries = append(pieSummaries, PieSummary{
			PieID:         pieID,
			Name:          pieName,
			Cash:          pieCash,
			TotalValue:    pieVal,
			UnrealizedPnL: piePnL,
			Status:        pieStatus,
		})

		if detailedPie.JSON200.Instruments != nil {
			for _, inst := range *detailedPie.JSON200.Instruments {
				if inst.Ticker == nil {
					continue
				}
				ticker := *inst.Ticker
				var ownedQuantity float64
				if inst.OwnedQuantity != nil {
					ownedQuantity = float64(*inst.OwnedQuantity)
				}
				var currentShare float64
				if inst.CurrentShare != nil {
					currentShare = float64(*inst.CurrentShare)
				}
				var expectedShare float64
				if inst.ExpectedShare != nil {
					expectedShare = float64(*inst.ExpectedShare)
				}

				comp := PieComposition{
					PieID:         pieID,
					PieName:       pieName,
					Quantity:      ownedQuantity,
					CurrentShare:  currentShare,
					ExpectedShare: expectedShare,
				}
				tickerPies[ticker] = append(tickerPies[ticker], comp)
			}
		}
	}

	// 5. Reconcile holdings
	holdings := []Holding{}
	for _, pos := range *positions.JSON200 {
		ticker := ""
		isin := ""
		name := ""
		curr := ""
		if pos.Instrument != nil {
			if pos.Instrument.Ticker != nil {
				ticker = *pos.Instrument.Ticker
			}
			if pos.Instrument.Isin != nil {
				isin = *pos.Instrument.Isin
			}
			if pos.Instrument.Name != nil {
				name = *pos.Instrument.Name
			}
			if pos.Instrument.Currency != nil {
				curr = *pos.Instrument.Currency
			}
		}

		totalQty := 0.0
		qtyInPies := 0.0
		if pos.Quantity != nil {
			totalQty = float64(*pos.Quantity)
		}
		if pos.QuantityInPies != nil {
			qtyInPies = float64(*pos.QuantityInPies)
		}

		qtyNotInPies := totalQty - qtyInPies

		var avgPrice float64
		var currPrice float64
		if pos.AveragePricePaid != nil {
			avgPrice = float64(*pos.AveragePricePaid)
		}
		if pos.CurrentPrice != nil {
			currPrice = float64(*pos.CurrentPrice)
		}

		var totalCost float64
		var currValue float64
		var unrealizedPnL float64
		if pos.WalletImpact != nil {
			if pos.WalletImpact.TotalCost != nil {
				totalCost = float64(*pos.WalletImpact.TotalCost)
			}
			if pos.WalletImpact.CurrentValue != nil {
				currValue = float64(*pos.WalletImpact.CurrentValue)
			}
			if pos.WalletImpact.UnrealizedProfitLoss != nil {
				unrealizedPnL = float64(*pos.WalletImpact.UnrealizedProfitLoss)
			}
		}

		piesForTicker := tickerPies[ticker]
		if piesForTicker == nil {
			piesForTicker = []PieComposition{}
		}

		holdings = append(holdings, Holding{
			Ticker:            ticker,
			Name:              name,
			ISIN:              isin,
			Currency:          curr,
			TotalQuantity:     totalQty,
			QuantityInPies:    qtyInPies,
			QuantityNotInPies: qtyNotInPies,
			AveragePrice:      avgPrice,
			CurrentPrice:      currPrice,
			TotalCost:         totalCost,
			CurrentValue:      currValue,
			UnrealizedPnL:     unrealizedPnL,
			Pies:              piesForTicker,
		})
	}

	var availableToTrade float64
	var cashInPies float64
	var reserved float64
	if summary.JSON200.Cash != nil {
		if summary.JSON200.Cash.AvailableToTrade != nil {
			availableToTrade = float64(*summary.JSON200.Cash.AvailableToTrade)
		}
		if summary.JSON200.Cash.InPies != nil {
			cashInPies = float64(*summary.JSON200.Cash.InPies)
		}
		if summary.JSON200.Cash.ReservedForOrders != nil {
			reserved = float64(*summary.JSON200.Cash.ReservedForOrders)
		}
	}

	var totalVal float64
	if summary.JSON200.TotalValue != nil {
		totalVal = float64(*summary.JSON200.TotalValue)
	}

	var accountID int64
	if summary.JSON200.Id != nil {
		accountID = *summary.JSON200.Id
	}

	var accountCurrency string
	if summary.JSON200.Currency != nil {
		accountCurrency = *summary.JSON200.Currency
	}

	report := &AccountReport{
		AccountID:  accountID,
		Currency:   accountCurrency,
		TotalValue: totalVal,
		Cash: CashSummary{
			AvailableToTrade:  availableToTrade,
			InPies:            cashInPies,
			ReservedForOrders: reserved,
		},
		Holdings: holdings,
		Pies:     pieSummaries,
	}

	return report, nil
}
