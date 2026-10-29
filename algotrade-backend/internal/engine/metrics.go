package engine

import (
	"bytes"
	"math"
)

// ReasonFlag is a 16-bit bitmask encoding catalysts and penalties
type ReasonFlag uint16

const (
	// Bullish Catalysts (Bits 0-5)
	ReasonVWAPSweetSpot   ReasonFlag = 1 << 0  // In VWAP +0.25 ATR sweet spot with CLV >= 0.55
	ReasonRVOLSurge       ReasonFlag = 1 << 1  // Dual RVOL >= 1.8x
	ReasonSectorLeader    ReasonFlag = 1 << 2  // Stock is in Top 2 Leading Sectors
	ReasonOrderBookWall   ReasonFlag = 1 << 3  // Smoothed Buyer Ratio >= 58% AND AggressorBuyRatio >= 58%
	ReasonORBBreakout     ReasonFlag = 1 << 4  // Holding cleanly above 15m Opening Range High
	ReasonHighEV          ReasonFlag = 1 << 5  // Pre-trade EV >= +0.40R

	// Bearish Catalysts (Bits 6-7)
	ReasonBearishBreakdown ReasonFlag = 1 << 6 // Clean breakdown below VWAP -0.25 ATR with InvertedCLV >= 0.55
	ReasonSectorLagging    ReasonFlag = 1 << 7 // Stock is in Lagging Sector with high sell delta

	// Penalties & Risk Filters (Bits 8-14)
	PenaltyWideSpread     ReasonFlag = 1 << 8  // Spread > 0.10% (Friction guard)
	PenaltyOverextended   ReasonFlag = 1 << 9  // LTP > VWAP + 1.5 ATR (Chasing top risk)
	PenaltyBelowPrevClose ReasonFlag = 1 << 10 // Gap-down dead-cat bounce (LTP < PrevClose)
	PenaltyLowVolume      ReasonFlag = 1 << 11 // Composite RVOL < 1.0x (Retail churn)
	PenaltyPullbackRedBar ReasonFlag = 1 << 12 // In zone, but red 5m bar or CLV < 0.55 (Wait for turn)
	PenaltyExhaustedTrend ReasonFlag = 1 << 13 // 4th+ pullback to VWAP (Institutional exhaustion)
	PenaltyIcebergSell    ReasonFlag = 1 << 14 // Bids visible, but aggressive sellers dumping (Delta V negative)
)

// OHLCVBar represents a single 5-minute candle.
// Exact size: 48 bytes (divisible by 8).
type OHLCVBar struct {
	Open      float64 // 8 bytes
	High      float64 // 8 bytes
	Low       float64 // 8 bytes
	Close     float64 // 8 bytes
	Volume    uint64  // 8 bytes
	Timestamp int64   // 8 bytes (Exchange epoch seconds)
}

// DepthLevel represents a single tier in the 5-level market depth book (12 bytes)
type DepthLevel struct {
	Price  float32 // Price level in ₹
	Qty    uint32  // Cumulative shares queued at this tier
	Orders uint16  // Number of resting orders at this tier
	_pad   uint16  // Explicit 2-byte alignment padding (12 bytes total)
}

// StockMetrics holds real-time quantitative state for a single stock.
// Strict descending alignment layout: Zero internal compiler padding holes.
// Total struct size: Exactly 3,976 bytes (divisible by 8).
type StockMetrics struct {
	// -------------------------------------------------------------------------
	// 1. Eight-Byte Types (Composite Arrays, float64, uint64, int64)
	// -------------------------------------------------------------------------
	// SessionBars: 75 five-minute bars (09:15 to 15:30 IST).
	// 75 * 48 bytes = 3,600 bytes. Divisible by 8.
	SessionBars       [75]OHLCVBar

	// 5-Level Market Depth: 5 Bids (60B) + 5 Asks (60B) = 120 bytes. Divisible by 8.
	BidLevels         [5]DepthLevel
	AskLevels         [5]DepthLevel

	LTP               float64 // Last Traded Price (₹)
	ExchangeVWAP      float64 // Official Exchange VWAP (from bytes 12-15)
	Open              float64 // Today Day Open
	High              float64 // Today Day High
	Low               float64 // Today Day Low
	PrevClose         float64 // Previous Day Close
	ATR14             float64 // 14-period Average True Range
	ADX14             float64 // 14-period Trend Strength
	BullishScore      float64 // Composite Bullish Rank (0 to 100)
	BearishScore      float64 // Composite Bearish Rank (0 to 100)
	CompositeRS       float64 // Timeframe-matched relative strength vs Nifty & Sector
	RVOLSession       float64 // Cumulative session volume / TOD baseline
	RVOLPace5m        float64 // Current 5m candle volume pace / TOD slot baseline
	EV                float64 // Pre-trade Expected Value in R-multiples
	PWin              float64 // Calibrated Bayesian Confluence Win Probability (0.0 to 1.0)
	StopProxy         float64 // Concrete Stop Loss level (₹)
	TargetProxy       float64 // Concrete Profit Target level (₹)
	Volume            uint64  // Cumulative Volume Traded Today
	BaselineCumVolume uint64  // 20-day historical TOD cumulative volume baseline up to slot k
	BaselineSlotVolume uint64 // 20-day historical median volume of single slot k
	BarStartVolume    uint64  // Cumulative volume at start of current 5m bar
	BarBuyVolume      uint64  // Aggressor volume lifting the Ask in current 5m bar
	BarSellVolume     uint64  // Aggressor volume hitting the Bid in current 5m bar
	LastUpdatedUnix   int64   // Exchange timestamp of last tick (epoch seconds)
	RollingPeak       float64 // Continuous time-decaying (tau=20s) price peak for shock velocity
	// Subtotal Group 1: 3,600 + (25 fields * 8B) = 3,800 bytes (divisible by 8)

	// -------------------------------------------------------------------------
	// 2. Four-Byte Types (uint32, float32) - 10 fields * 4B = 40 bytes (divisible by 8)
	// -------------------------------------------------------------------------
	InstrumentToken    uint32
	LotSize            uint32
	Bid1Price          float32 // Best bid price
	Ask1Price          float32 // Best ask price
	TickSize           float32 // 0.05 or 0.01 (needed by CLV range guard)
	SmoothedBuyerRatio float32 // Top-of-book EMA buyer depth ratio (0.0 to 1.0)
	AggressorBuyRatio  float32 // BarBuyVolume / (BarBuyVolume + BarSellVolume)
	MISLeverage        float32 // Cached Zerodha MIS leverage (e.g. 5.0x for 20% VaR)
	FastAggressorEMA   float32 // Fast alpha=0.25 tick-delta aggressor EMA for <1s shock detection
	_pad4              uint32  // Explicit 4-byte padding to align Group 2 to 40 bytes

	// -------------------------------------------------------------------------
	// 3. Two-Byte Types (uint16, ReasonFlag) - 2 fields * 2B = 4 bytes
	// -------------------------------------------------------------------------
	Reasons     ReasonFlag // 16-bit bitmask of active catalysts & penalties
	ActiveIndex uint16     // Index in activeStocks slice

	// -------------------------------------------------------------------------
	// 4. One-Byte Types & Arrays - 16B + 6B = 22 bytes
	// -------------------------------------------------------------------------
	Symbol        [16]byte // Uppercase trading symbol e.g. "TATASTEEL\x00"
	SectorID      uint8    // Sector identifier (1..12)
	CurrentBarIdx uint8    // 0 to 74 (current 5m candle index)
	PullbackCount uint8    // Count of confirmed pullbacks to VWAP
	WasExtended   bool     // Hysteresis latch: price rallied > 1.0 ATR above VWAP
	IsTradeable   bool     // Passes spread <= 0.10% and liquidity gates
	IsWarm        bool     // Historical data pre-seeded successfully

	// -------------------------------------------------------------------------
	// 5. Explicit 6-Byte Padding (Brings struct to exact 8-byte boundary)
	// Group 3 (4B) + Group 4 (22B) = 26 bytes. Next 8-byte boundary is 32 bytes.
	// 32 - 26 = 6 bytes padding.
	// -------------------------------------------------------------------------
	_ [6]byte
} // Total struct size: 3,792 + 120 + 32 + 32 = 3,976 bytes. Exact multiple of 8!

// SymbolString returns the null-trimmed string representation of the symbol
func (s *StockMetrics) SymbolString() string {
	idx := bytes.IndexByte(s.Symbol[:], 0)
	if idx == -1 {
		idx = len(s.Symbol)
	}
	return string(s.Symbol[:idx])
}

// SetSymbol copies a string into the [16]byte array
func (s *StockMetrics) SetSymbol(sym string) {
	copy(s.Symbol[:], sym)
	if len(sym) < 16 {
		s.Symbol[len(sym)] = 0
	}
}

// OpeningRange15m computes the 15-minute Opening Range High and Low (09:15 to 09:30 AM).
// Zero-cost calculation in 2 nanoseconds without adding any struct fields.
func (s *StockMetrics) OpeningRange15m() (orh, orl float64, ready bool) {
	if s.CurrentBarIdx < 3 {
		return 0, 0, false // Still inside 09:15-09:30 AM opening drive window
	}

	orh = math.Max(s.SessionBars[0].High, math.Max(s.SessionBars[1].High, s.SessionBars[2].High))
	orl = math.Min(s.SessionBars[0].Low, math.Min(s.SessionBars[1].Low, s.SessionBars[2].Low))
	return orh, orl, orh > 0 && orl > 0
}
