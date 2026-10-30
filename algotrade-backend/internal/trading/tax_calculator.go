package trading

import (
	"math"

	"github.com/ajaynegi33/AlgoTrade/internal/market"
)

// TaxBreakdown aliases market.TaxBreakdownDTO to prevent duplicate struct definitions
type TaxBreakdown = market.TaxBreakdownDTO

// CalculateRoundTripCharges computes the exact Zerodha + NSE Cash MIS round-trip charges in ₹
func CalculateRoundTripCharges(buyPrice, sellPrice float64, qty int) TaxBreakdown {
	return CalculatePositionTaxBreakdown(buyPrice, sellPrice, qty, false)
}

// CalculatePositionTaxBreakdown computes the exact Zerodha + NSE Cash MIS round-trip charges in ₹,
// accounting for 2 sell order legs when isSplitExit (Tranche 1 +1R 50% scale-out + Runner exit) is true.
func CalculatePositionTaxBreakdown(buyPrice, sellPrice float64, qty int, isSplitExit bool) TaxBreakdown {
	if qty <= 0 || buyPrice <= 0 || sellPrice <= 0 {
		return TaxBreakdown{}
	}
	sharesCount := float64(qty)
	buyTurnover := buyPrice * sharesCount
	sellTurnover := sellPrice * sharesCount
	totalTurnover := buyTurnover + sellTurnover

	buyBrokerageFee := math.Min(20.0, 0.0003*buyTurnover)
	var sellBrokerageFee float64
	if isSplitExit && qty >= 2 {
		tranche1Quantity := float64(qty / 2)
		runnerQuantity := float64(qty - (qty / 2))
		sellBrokerageFee = math.Min(20.0, 0.0003*(sellPrice*tranche1Quantity)) + math.Min(20.0, 0.0003*(sellPrice*runnerQuantity))
	} else {
		sellBrokerageFee = math.Min(20.0, 0.0003*sellTurnover)
	}

	stt := math.Round(0.00025 * sellTurnover)
	exch := math.Round(0.0000297*totalTurnover*100.0) / 100.0
	sebi := math.Round(0.000001*totalTurnover*100.0) / 100.0
	stamp := math.Round(0.00003 * buyTurnover)
	gst := math.Round(0.18*(buyBrokerageFee+sellBrokerageFee+exch+sebi)*100.0) / 100.0
	buyBrokerageFee = math.Round(buyBrokerageFee*100.0) / 100.0
	sellBrokerageFee = math.Round(sellBrokerageFee*100.0) / 100.0
	brokerage := math.Round((buyBrokerageFee+sellBrokerageFee)*100.0) / 100.0
	total := math.Round((buyBrokerageFee+sellBrokerageFee+stt+exch+sebi+stamp+gst)*100.0) / 100.0

	return TaxBreakdown{
		BuyBrokerage:  buyBrokerageFee,
		SellBrokerage: sellBrokerageFee,
		Brokerage:     brokerage,
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
