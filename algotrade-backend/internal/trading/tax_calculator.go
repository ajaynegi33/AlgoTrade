package trading

import "math"

// TaxBreakdown holds the itemised NSE Cash Intraday (MIS) statutory & brokerage charges
type TaxBreakdown struct {
	BuyBrokerage  float64 `json:"buy_brokerage"`
	SellBrokerage float64 `json:"sell_brokerage"`
	STT           float64 `json:"stt"`           // 0.025% on Sell turnover
	ExchangeTxn   float64 `json:"exchange_txn"`  // 0.00297% on total turnover (NSE)
	SEBICharges   float64 `json:"sebi_charges"`  // 0.0001% on total turnover
	StampDuty     float64 `json:"stamp_duty"`    // 0.003% on Buy turnover
	GST           float64 `json:"gst"`           // 18% on (Brokerage + ExchangeTxn + SEBI)
	TotalCharges  float64 `json:"total_charges"` // Exact sum of all statutory & brokerage fees
}

// CalculateRoundTripCharges computes the exact Zerodha + NSE Cash MIS round-trip charges in ₹
func CalculateRoundTripCharges(buyPrice, sellPrice float64, qty int) TaxBreakdown {
	return CalculatePositionTaxBreakdown(buyPrice, sellPrice, qty, false)
}

// CalculatePositionTaxBreakdown computes the exact Zerodha + NSE Cash MIS round-trip charges in ₹,
// accounting for 2 sell order legs when splitExit (Tranche 1 +1R 50% scale-out + Runner exit) is true.
func CalculatePositionTaxBreakdown(buyPrice, sellPrice float64, qty int, splitExit bool) TaxBreakdown {
	if qty <= 0 || buyPrice <= 0 || sellPrice <= 0 {
		return TaxBreakdown{}
	}
	q := float64(qty)
	buyTurnover := buyPrice * q
	sellTurnover := sellPrice * q
	totalTurnover := buyTurnover + sellTurnover

	buyBrok := math.Min(20.0, 0.0003*buyTurnover)
	var sellBrok float64
	if splitExit && qty >= 2 {
		q1 := float64(qty / 2)
		q2 := float64(qty - (qty / 2))
		sellBrok = math.Min(20.0, 0.0003*(sellPrice*q1)) + math.Min(20.0, 0.0003*(sellPrice*q2))
	} else {
		sellBrok = math.Min(20.0, 0.0003*sellTurnover)
	}

	stt := math.Round(0.00025 * sellTurnover)
	exch := math.Round(0.0000297*totalTurnover*100.0) / 100.0
	sebi := math.Round(0.000001*totalTurnover*100.0) / 100.0
	stamp := math.Round(0.00003 * buyTurnover)
	gst := math.Round(0.18*(buyBrok+sellBrok+exch+sebi)*100.0) / 100.0
	buyBrok = math.Round(buyBrok*100.0) / 100.0
	sellBrok = math.Round(sellBrok*100.0) / 100.0
	total := math.Round((buyBrok+sellBrok+stt+exch+sebi+stamp+gst)*100.0) / 100.0

	return TaxBreakdown{
		BuyBrokerage:  buyBrok,
		SellBrokerage: sellBrok,
		STT:           stt,
		ExchangeTxn:   exch,
		SEBICharges:   sebi,
		StampDuty:     stamp,
		GST:           gst,
		TotalCharges:  total,
	}
}


// RoundToTick rounds a price to the nearest valid exchange tick increment
func RoundToTick(price, tickSize float64) float64 {
	if tickSize <= 0 {
		tickSize = 0.05
	}
	return math.Round(price/tickSize) * tickSize
}

// RoundDownToTick rounds a price down to the nearest valid exchange tick increment
func RoundDownToTick(price, tickSize float64) float64 {
	if tickSize <= 0 {
		tickSize = 0.05
	}
	// Add 1e-9 epsilon to avoid IEEE-754 floor drift on exact multiples like 100.05/0.05
	return math.Floor((price+1e-9)/tickSize) * tickSize
}

// RoundUpToTick rounds a price up to the nearest valid exchange tick increment
func RoundUpToTick(price, tickSize float64) float64 {
	if tickSize <= 0 {
		tickSize = 0.05
	}
	return math.Ceil((price-1e-9)/tickSize) * tickSize
}
