package engine

import (
	"math"
	"time"
)

var istZone = time.FixedZone("IST", 5*3600+1800)

// CalculateBullishScore performs institutional quantification of a long trade setup
func CalculateBullishScore( stock *StockMetrics, nifty *StockMetrics, sector *StockMetrics, isLeaderSector bool, breadthRatio float64 ) (score float64, ev float64, pWin float64, reasons ReasonFlag) {
	if stock.LTP <= 0 || stock.ExchangeVWAP <= 0 {
		return 0, 0, 0.44, 0
	}

	atrSafe := math.Max(stock.ATR14, stock.LTP*0.0025)

	// 1. Friction & Gap Guards
	if stock.Bid1Price > 0 && stock.Ask1Price > 0 {
		spread := float64(stock.Ask1Price-stock.Bid1Price) / stock.LTP
		if spread > 0.0010 { // Spread > 0.10%
			reasons |= PenaltyWideSpread
		}
	}
	if stock.PrevClose > 0 && stock.LTP < stock.PrevClose {
		reasons |= PenaltyBelowPrevClose
	}

	// 2. Gaussian Value Sweet Spot (S_VWAP)
	x := (stock.LTP - stock.ExchangeVWAP) / atrSafe
	var sVWAP float64
	switch {
	case x < -0.10:
		sVWAP = 0.0
	case x < 0.00:
		sVWAP = 45.0 // Testing VWAP support buffer
	case x <= 0.60:
		sVWAP = 100.0 - 150.0*(x-0.25)*(x-0.25) // Peaks at +0.25 ATR = 100.0
	case x <= 1.50:
		sVWAP = 80.0 - ((x-0.60)/0.90)*60.0 // Linear decay down to 20.0
	default:
		sVWAP = 10.0
		reasons |= PenaltyOverextended
	}

	// 3. Close Location Value (CLV) Upper-Wick Filter
	currBar := stock.SessionBars[stock.CurrentBarIdx]
	barRange := currBar.High - currBar.Low
	var clv float64 = 1.0
	if barRange > float64(stock.TickSize)*2.0 {
		clv = (stock.LTP - currBar.Low) / barRange
	}

	if clv < 0.55 || (currBar.Open > 0 && stock.LTP <= currBar.Open) {
		if sVWAP > 40.0 {
			sVWAP = 40.0
		}
		reasons |= PenaltyPullbackRedBar
	} else if sVWAP >= 85.0 {
		reasons |= ReasonVWAPSweetSpot
	}

	// 4. Pullback Count Exhaustion Filter
	switch {
	case stock.PullbackCount == 3:
		sVWAP *= 0.75
	case stock.PullbackCount >= 4:
		if sVWAP > 30.0 {
			sVWAP = 30.0
		}
		reasons |= PenaltyExhaustedTrend
	}

	// 5. Dual RVOL with 30s Floor & ₹5 Lakh Turnover Gate
	elapsed := (stock.LastUpdatedUnix - currBar.Timestamp) % 300
	if elapsed < 0 {
		elapsed = 0
	}
	clampedElapsed := math.Max(float64(elapsed), 30.0)

	rvolSession := float64(stock.Volume) / math.Max(float64(stock.BaselineCumVolume), 1000.0)
	expectedSlotVol := float64(stock.BaselineSlotVolume) * (clampedElapsed / 300.0)
	currBarVol := float64(stock.Volume - stock.BarStartVolume)
	rvolPace5m := currBarVol / math.Max(expectedSlotVol, 500.0)

	currBarTurnover := currBarVol * stock.LTP
	compositeRVOL := rvolSession

	if currBarVol >= float64(stock.BaselineSlotVolume)*0.15 &&
		currBarTurnover >= 500000.0 &&
		(currBar.Open == 0 || stock.LTP > currBar.Open) &&
		stock.SmoothedBuyerRatio >= 0.55 {
		compositeRVOL = math.Max(rvolSession, 0.85*rvolPace5m)
	}
	stock.RVOLSession = rvolSession
	stock.RVOLPace5m = rvolPace5m

	if compositeRVOL >= 1.8 {
		reasons |= ReasonRVOLSurge
	} else if compositeRVOL < 1.0 {
		reasons |= PenaltyLowVolume
	}

	// 6. Dynamic 30m Matched Relative Strength (RS)
	lookbackIdx := 0
	if stock.CurrentBarIdx >= 6 {
		lookbackIdx = int(stock.CurrentBarIdx) - 6
	}
	refPrice := stock.SessionBars[lookbackIdx].Close
	if refPrice <= 0 {
		refPrice = stock.Open
	}
	var stockRet30m float64
	if refPrice > 0 {
		stockRet30m = (stock.LTP - refPrice) / refPrice
	}

	var stockRetOpen float64
	if stock.Open > 0 {
		stockRetOpen = (stock.LTP - stock.Open) / stock.Open
	}

	var niftyRetOpen, niftyRet30m float64
	if nifty != nil {
		if nifty.Open > 0 {
			niftyRetOpen = (nifty.LTP - nifty.Open) / nifty.Open
		}
		nRef := nifty.SessionBars[lookbackIdx].Close
		if nRef <= 0 {
			nRef = nifty.Open
		}
		if nRef > 0 {
			niftyRet30m = (nifty.LTP - nRef) / nRef
		}
	}

	var secRetOpen, secRet30m float64
	if sector != nil {
		if sector.Open > 0 {
			secRetOpen = (sector.LTP - sector.Open) / sector.Open
		}
		sRef := sector.SessionBars[lookbackIdx].Close
		if sRef <= 0 {
			sRef = sector.Open
		}
		if sRef > 0 {
			secRet30m = (sector.LTP - sRef) / sRef
		}
	} else {
		secRetOpen = niftyRetOpen
		secRet30m = niftyRet30m
	}

	rsOpen := stockRetOpen - (0.6*niftyRetOpen + 0.4*secRetOpen)
	rs30m := stockRet30m - (0.6*niftyRet30m + 0.4*secRet30m)
	compositeRS := 0.5*rsOpen + 0.5*rs30m
	stock.CompositeRS = compositeRS * 100.0 // Percentage representation

	// 7. Microstructure Flow & Iceberg Filter
	if stock.SmoothedBuyerRatio >= 0.58 && stock.AggressorBuyRatio >= 0.58 {
		reasons |= ReasonOrderBookWall
	} else if stock.SmoothedBuyerRatio >= 0.60 && stock.AggressorBuyRatio < 0.45 {
		reasons |= PenaltyIcebergSell
	}

	if isLeaderSector {
		reasons |= ReasonSectorLeader
	}

	// 8. Opening Range Breakout (ORB)
	orh, _, orbReady := stock.OpeningRange15m()
	if orbReady && stock.LTP > orh && stock.CurrentBarIdx >= 3 {
		reasons |= ReasonORBBreakout
	}

	// 9. Concrete Structural Proxies & 1.8*1R Overhead Resistance Haircut
	stopProxy := math.Min(stock.ExchangeVWAP-0.25*atrSafe, currBar.Low-0.15*atrSafe)
	oneR := math.Max(stock.LTP-stopProxy, 0.5*atrSafe)
	stock.StopProxy = stopProxy

	roomToHigh := stock.High - stock.LTP
	var targetProxy float64
	if roomToHigh >= 1.8*oneR {
		// Ample room before Day High: lock in profit before resistance wall
		targetProxy = stock.High - 0.10*atrSafe
	} else {
		// Pressing Day High: require high volume & institutional buyer dominance to project breakout
		if compositeRVOL >= 1.8 && stock.SmoothedBuyerRatio >= 0.58 && stock.AggressorBuyRatio >= 0.58 && stock.PullbackCount <= 2 {
			targetProxy = math.Max(stock.High, stock.LTP) + 1.8*atrSafe
		} else {
			targetProxy = stock.High // Capped at Day High (shrinks EV)
		}
	}
	stock.TargetProxy = targetProxy

	// 10. Calibrated Orthogonal Bayesian Confluence PWin
	pWin = 0.44 // Empirical Base Prior for > 1.5R move

	// Bucket A: Macro + Sector Confluence
	if breadthRatio >= 0.55 && isLeaderSector && nifty != nil && nifty.LTP > nifty.ExchangeVWAP {
		pWin += 0.07
	}
	// Bucket B: Microstructure Execution
	if stock.SmoothedBuyerRatio >= 0.58 && stock.AggressorBuyRatio >= 0.58 {
		pWin += 0.07
	}
	// Bucket C: Volume Ignition
	if compositeRVOL >= 1.8 {
		pWin += 0.06
	}
	// Bucket D: Structural Timing
	if x >= 0.0 && x <= 0.50 && clv >= 0.60 && stock.PullbackCount <= 2 {
		pWin += 0.08
	}

	// Midday Lull Penalty (11:30 to 13:15 IST)
	if stock.LastUpdatedUnix > 0 {
		t := time.Unix(stock.LastUpdatedUnix, 0).In(istZone)
		hour, min := t.Hour(), t.Minute()
		totMin := hour*60 + min
		if totMin >= 11*60+30 && totMin <= 13*60+15 && compositeRVOL < 2.2 {
			pWin -= 0.06
		}
	}

	// Penalties Deductions
	if reasons&PenaltyBelowPrevClose != 0 {
		pWin -= 0.08
	}
	if reasons&PenaltyPullbackRedBar != 0 {
		pWin -= 0.08
	}
	if reasons&PenaltyWideSpread != 0 {
		pWin -= 0.05
	}
	if reasons&PenaltyExhaustedTrend != 0 {
		pWin -= 0.10
	}
	if reasons&PenaltyIcebergSell != 0 {
		pWin -= 0.12
	}

	pWin = math.Max(0.20, math.Min(0.75, pWin))
	stock.PWin = pWin

	// 11. Pre-Trade Expected Value (EV)
	rTarget := math.Max(0.1, (targetProxy-stock.LTP)/oneR)
	friction := 0.05 // 0.05R fees & slippage
	ev = (pWin * rTarget) - ((1.0 - pWin) * 1.0) - friction
	stock.EV = ev

	if ev >= 0.40 {
		reasons |= ReasonHighEV
	}

	// 12. 5-Factor Institutional Composite Bullish Rank Score (0 to 100)
	// Falsification Fix (Bug B4): Directly weight Anti-Spoof Order Flow (sFlow) and Logistic RVOL (sRVOL)
	// alongside sVWAP, sRS, and sEV to maximize 60%–70% Long-Only Intraday Win Rate.
	sRVOL := math.Min(100.0, 105.0/(1.0+math.Exp(-1.65*(compositeRVOL-2.0))))
	sRS := math.Min(100.0, math.Max(0.0, 50.0+48.4*math.Tanh(0.65*stock.CompositeRS)))

	effectiveOBI := float64(stock.SmoothedBuyerRatio)
	if effectiveOBI <= 0 {
		effectiveOBI = 0.50
	}
	effectiveAgg := float64(stock.AggressorBuyRatio)
	if stock.FastAggressorEMA > 0 {
		effectiveAgg = 0.60*float64(stock.FastAggressorEMA) + 0.40*float64(stock.AggressorBuyRatio)
	}
	flowComposite := 0.40*effectiveOBI + 0.60*effectiveAgg
	sFlow := math.Min(100.0, math.Max(0.0, (flowComposite-0.35)*200.0))
	sEV := math.Min(100.0, math.Max(0.0, (ev+0.40)*55.0))

	score = (0.26 * sVWAP) +
		(0.22 * sRVOL) +
		(0.18 * sRS) +
		(0.20 * sFlow) +
		(0.14 * sEV)

	if reasons&PenaltyWideSpread != 0 || reasons&PenaltyBelowPrevClose != 0 {
		score *= 0.60
	}

	stock.BullishScore = score
	stock.Reasons = reasons
	return score, ev, pWin, reasons
}

// CalculateBearishScore performs institutional quantification of a short-selling breakdown setup
func CalculateBearishScore(
	stock *StockMetrics,
	nifty *StockMetrics,
	sector *StockMetrics,
	isLaggingSector bool,
	breadthRatio float64,
) (score float64, ev float64, pWin float64, reasons ReasonFlag) {
	if stock.LTP <= 0 || stock.ExchangeVWAP <= 0 {
		return 0, 0, 0.44, 0
	}

	atrSafe := math.Max(stock.ATR14, stock.LTP*0.0025)

	// Inverted Value Sweet Spot: Short push-off below VWAP
	xBear := (stock.ExchangeVWAP - stock.LTP) / atrSafe
	var sVWAPBear float64
	switch {
	case xBear < -0.10:
		sVWAPBear = 0.0
	case xBear < 0.00:
		sVWAPBear = 45.0 // Testing VWAP resistance buffer
	case xBear <= 0.60:
		sVWAPBear = 100.0 - 150.0*(xBear-0.25)*(xBear-0.25)
	case xBear <= 1.50:
		sVWAPBear = 80.0 - ((xBear-0.60)/0.90)*60.0
	default:
		sVWAPBear = 10.0
		reasons |= PenaltyOverextended
	}

	// Inverted CLV (Hammer / Lower Wick Rejection Filter)
	currBar := stock.SessionBars[stock.CurrentBarIdx]
	barRange := currBar.High - currBar.Low
	var invertedCLV float64 = 1.0
	if barRange > float64(stock.TickSize)*2.0 {
		invertedCLV = (currBar.High - stock.LTP) / barRange
	}

	if invertedCLV < 0.55 || (currBar.Open > 0 && stock.LTP >= currBar.Open) {
		if sVWAPBear > 40.0 {
			sVWAPBear = 40.0
		}
	} else if sVWAPBear >= 85.0 {
		reasons |= ReasonBearishBreakdown
	}

	// Aggressor Sell Flow
	aggressorSellRatio := 1.0 - stock.AggressorBuyRatio
	if isLaggingSector && aggressorSellRatio >= 0.58 {
		reasons |= ReasonSectorLagging
	}

	// Support Haircut on Day Low
	stopProxy := math.Max(stock.ExchangeVWAP+0.25*atrSafe, currBar.High+0.15*atrSafe)
	oneR := math.Max(stopProxy-stock.LTP, 0.5*atrSafe)

	// Dual-Horizon RVOL (consistent with CalculateBullishScore so midday 5m breakdown surges are captured)
	compositeRVOL := math.Max(stock.RVOLSession, 0.85*stock.RVOLPace5m)

	roomToLow := stock.LTP - stock.Low
	var targetProxy float64
	if roomToLow >= 1.8*oneR {
		targetProxy = stock.Low + 0.10*atrSafe
	} else {
		if compositeRVOL >= 1.8 && aggressorSellRatio >= 0.58 {
			targetProxy = math.Min(stock.Low, stock.LTP) - 1.8*atrSafe
		} else {
			targetProxy = stock.Low
		}
	}

	pWin = 0.44
	if breadthRatio <= 0.45 && isLaggingSector && nifty != nil && nifty.LTP < nifty.ExchangeVWAP {
		pWin += 0.07
	}
	if aggressorSellRatio >= 0.58 && stock.SmoothedBuyerRatio <= 0.42 {
		pWin += 0.07
	}
	if compositeRVOL >= 1.8 {
		pWin += 0.06
	}
	if xBear >= 0.0 && xBear <= 0.50 && invertedCLV >= 0.60 {
		pWin += 0.08
	}

	pWin = math.Max(0.20, math.Min(0.75, pWin))
	rTarget := math.Max(0.1, (stock.LTP-targetProxy)/oneR)
	ev = (pWin * rTarget) - ((1.0 - pWin) * 1.0) - 0.05

	sRVOLBear := math.Min(100.0, 105.0/(1.0+math.Exp(-1.65*(compositeRVOL-2.0))))
	sRSBear := math.Min(100.0, math.Max(0.0, 50.0+48.4*math.Tanh(0.65*(-stock.CompositeRS))))
	effOBIBear := float64(stock.SmoothedBuyerRatio)
	if effOBIBear <= 0 {
		effOBIBear = 0.50
	}
	sellerOBI := 1.0 - effOBIBear
	bearFlowComp := 0.40*sellerOBI + 0.60*float64(aggressorSellRatio)
	sFlowBear := math.Min(100.0, math.Max(0.0, (bearFlowComp-0.35)*200.0))
	sEVBear := math.Min(100.0, math.Max(0.0, (ev+0.40)*55.0))

	score = (0.26 * sVWAPBear) +
		(0.22 * sRVOLBear) +
		(0.18 * sRSBear) +
		(0.20 * sFlowBear) +
		(0.14 * sEVBear)

	stock.BearishScore = score
	stock.Reasons |= (reasons & (ReasonBearishBreakdown | ReasonSectorLagging))
	return score, ev, pWin, reasons
}
