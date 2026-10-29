package market

import (
	"math"
	"time"
)

var istZone = ISTLocation

// CalculateBullishScore performs institutional quantification of a long trade setup
func CalculateBullishScore(stock *StockMetrics, breadthRatio float64) (score float64, ev float64, winProbability float64, reasons ReasonFlag) {
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

	// 2. Gaussian Value Sweet Spot (vwapScore)
	normalizedVWAPDistance := (stock.LTP - stock.ExchangeVWAP) / atrSafe
	var vwapScore float64
	switch {
	case normalizedVWAPDistance < -0.10:
		vwapScore = 0.0
	case normalizedVWAPDistance < 0.00:
		vwapScore = 45.0 // Testing VWAP support buffer
	case normalizedVWAPDistance <= 0.60:
		vwapScore = 100.0 - 150.0*(normalizedVWAPDistance-0.25)*(normalizedVWAPDistance-0.25) // Peaks at +0.25 ATR = 100.0
	case normalizedVWAPDistance <= 1.50:
		vwapScore = 80.0 - ((normalizedVWAPDistance-0.60)/0.90)*60.0 // Linear decay down to 20.0
	default:
		vwapScore = 10.0
		reasons |= PenaltyOverextended
	}

	// 3. Close Location Value (CLV) Upper-Wick Filter
	currentBar := stock.SessionBars[stock.CurrentBarIdx]
	barRange := currentBar.High - currentBar.Low
	var closeLocationValue float64 = 1.0
	if barRange > float64(stock.TickSize)*2.0 {
		closeLocationValue = (stock.LTP - currentBar.Low) / barRange
	}

	if closeLocationValue < 0.55 || (currentBar.Open > 0 && stock.LTP <= currentBar.Open) {
		if vwapScore > 40.0 {
			vwapScore = 40.0
		}
		reasons |= PenaltyPullbackRedBar
	} else if vwapScore >= 85.0 {
		reasons |= ReasonVWAPSweetSpot
	}

	// 4. Pullback Count Exhaustion Filter
	switch {
	case stock.PullbackCount == 3:
		vwapScore *= 0.75
	case stock.PullbackCount >= 4:
		if vwapScore > 30.0 {
			vwapScore = 30.0
		}
		reasons |= PenaltyExhaustedTrend
	}

	// 5. Dual RVOL with 30s Floor & ₹5 Lakh Turnover Gate
	elapsed := (stock.LastUpdatedUnix - currentBar.Timestamp) % 300
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
		(currentBar.Open == 0 || stock.LTP > currentBar.Open) &&
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

	compositeRS := 0.5*stockRetOpen + 0.5*stockRet30m
	stock.CompositeRS = compositeRS * 100.0 // Percentage representation

	// 7. Microstructure Flow & Iceberg Filter
	if stock.SmoothedBuyerRatio >= 0.58 && stock.AggressorBuyRatio >= 0.58 {
		reasons |= ReasonOrderBookWall
	} else if stock.SmoothedBuyerRatio >= 0.60 && stock.AggressorBuyRatio < 0.45 {
		reasons |= PenaltyIcebergSell
	}

	// 8. Opening Range Breakout (ORB)
	orh, _, orbReady := stock.OpeningRange15m()
	if orbReady && stock.LTP > orh && stock.CurrentBarIdx >= 3 {
		reasons |= ReasonORBBreakout
	}

	// 9. Concrete Structural Proxies & 1.8*1R Overhead Resistance Haircut
	stopProxy := math.Min(stock.ExchangeVWAP-0.25*atrSafe, currentBar.Low-0.15*atrSafe)
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
	winProbability = 0.44 // Empirical Base Prior for > 1.5R move

	// Bucket A: Market Breadth Confluence
	if breadthRatio >= 0.55 {
		winProbability += 0.07
	}
	// Bucket B: Microstructure Execution
	if stock.SmoothedBuyerRatio >= 0.58 && stock.AggressorBuyRatio >= 0.58 {
		winProbability += 0.07
	}
	// Bucket C: Volume Ignition
	if compositeRVOL >= 1.8 {
		winProbability += 0.06
	}
	// Bucket D: Structural Timing
	if normalizedVWAPDistance >= 0.0 && normalizedVWAPDistance <= 0.50 && closeLocationValue >= 0.60 && stock.PullbackCount <= 2 {
		winProbability += 0.08
	}

	// Midday Lull Penalty (11:30 to 13:15 IST)
	if stock.LastUpdatedUnix > 0 {
		t := time.Unix(stock.LastUpdatedUnix, 0).In(istZone)
		hour, min := t.Hour(), t.Minute()
		totMin := hour*60 + min
		if totMin >= 11*60+30 && totMin <= 13*60+15 && compositeRVOL < 2.2 {
			winProbability -= 0.06
		}
	}

	// Penalties Deductions
	if reasons&PenaltyBelowPrevClose != 0 {
		winProbability -= 0.08
	}
	if reasons&PenaltyPullbackRedBar != 0 {
		winProbability -= 0.08
	}
	if reasons&PenaltyWideSpread != 0 {
		winProbability -= 0.05
	}
	if reasons&PenaltyExhaustedTrend != 0 {
		winProbability -= 0.10
	}
	if reasons&PenaltyIcebergSell != 0 {
		winProbability -= 0.12
	}

	winProbability = math.Max(0.20, math.Min(0.75, winProbability))
	stock.PWin = winProbability

	// 11. Pre-Trade Expected Value (EV)
	rTarget := math.Max(0.1, (targetProxy-stock.LTP)/oneR)
	friction := 0.05 // 0.05R fees & slippage
	ev = (winProbability * rTarget) - ((1.0 - winProbability) * 1.0) - friction
	stock.EV = ev

	if ev >= 0.40 {
		reasons |= ReasonHighEV
	}

	// 12. 5-Factor Institutional Composite Bullish Rank Score (0 to 100)
	rvolScore := math.Min(100.0, 105.0/(1.0+math.Exp(-1.65*(compositeRVOL-2.0))))
	rsScore := math.Min(100.0, math.Max(0.0, 50.0+48.4*math.Tanh(0.65*stock.CompositeRS)))

	effectiveOBI := float64(stock.SmoothedBuyerRatio)
	if effectiveOBI <= 0 {
		effectiveOBI = 0.50
	}
	effectiveAgg := float64(stock.AggressorBuyRatio)
	if stock.FastAggressorEMA > 0 {
		effectiveAgg = 0.60*float64(stock.FastAggressorEMA) + 0.40*float64(stock.AggressorBuyRatio)
	}
	flowComposite := 0.40*effectiveOBI + 0.60*effectiveAgg
	flowScore := math.Min(100.0, math.Max(0.0, (flowComposite-0.35)*200.0))
	evScore := math.Min(100.0, math.Max(0.0, (ev+0.40)*55.0))

	score = (0.26 * vwapScore) +
		(0.22 * rvolScore) +
		(0.18 * rsScore) +
		(0.20 * flowScore) +
		(0.14 * evScore)

	if reasons&PenaltyWideSpread != 0 || reasons&PenaltyBelowPrevClose != 0 {
		score *= 0.60
	}

	stock.BullishScore = score
	stock.Reasons = reasons
	return score, ev, winProbability, reasons
}

// CalculateBearishScore performs institutional quantification of a short-selling breakdown setup
func CalculateBearishScore(
	stock *StockMetrics,
	breadthRatio float64,
) (score float64, ev float64, winProbability float64, reasons ReasonFlag) {
	if stock.LTP <= 0 || stock.ExchangeVWAP <= 0 {
		return 0, 0, 0.44, 0
	}

	atrSafe := math.Max(stock.ATR14, stock.LTP*0.0025)

	// Inverted Value Sweet Spot: Short push-off below VWAP
	normalizedVWAPDistanceBear := (stock.ExchangeVWAP - stock.LTP) / atrSafe
	var sVWAPBear float64
	switch {
	case normalizedVWAPDistanceBear < -0.10:
		sVWAPBear = 0.0
	case normalizedVWAPDistanceBear < 0.00:
		sVWAPBear = 45.0 // Testing VWAP resistance buffer
	case normalizedVWAPDistanceBear <= 0.60:
		sVWAPBear = 100.0 - 150.0*(normalizedVWAPDistanceBear-0.25)*(normalizedVWAPDistanceBear-0.25)
	case normalizedVWAPDistanceBear <= 1.50:
		sVWAPBear = 80.0 - ((normalizedVWAPDistanceBear-0.60)/0.90)*60.0
	default:
		sVWAPBear = 10.0
		reasons |= PenaltyOverextended
	}

	// Inverted CLV (Hammer / Lower Wick Rejection Filter)
	currentBar := stock.SessionBars[stock.CurrentBarIdx]
	barRange := currentBar.High - currentBar.Low
	var invertedCloseLocationValue float64 = 1.0
	if barRange > float64(stock.TickSize)*2.0 {
		invertedCloseLocationValue = (currentBar.High - stock.LTP) / barRange
	}

	if invertedCloseLocationValue < 0.55 || (currentBar.Open > 0 && stock.LTP >= currentBar.Open) {
		if sVWAPBear > 40.0 {
			sVWAPBear = 40.0
		}
	} else if sVWAPBear >= 85.0 {
		reasons |= ReasonBearishBreakdown
	}

	// Aggressor Sell Flow
	aggressorSellRatio := 1.0 - stock.AggressorBuyRatio

	// Support Haircut on Day Low
	stopProxy := math.Max(stock.ExchangeVWAP+0.25*atrSafe, currentBar.High+0.15*atrSafe)
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

	winProbability = 0.44
	if breadthRatio <= 0.45 {
		winProbability += 0.07
	}
	if aggressorSellRatio >= 0.58 && stock.SmoothedBuyerRatio <= 0.42 {
		winProbability += 0.07
	}
	if compositeRVOL >= 1.8 {
		winProbability += 0.06
	}
	if normalizedVWAPDistanceBear >= 0.0 && normalizedVWAPDistanceBear <= 0.50 && invertedCloseLocationValue >= 0.60 {
		winProbability += 0.08
	}

	winProbability = math.Max(0.20, math.Min(0.75, winProbability))
	rTarget := math.Max(0.1, (stock.LTP-targetProxy)/oneR)
	ev = (winProbability * rTarget) - ((1.0 - winProbability) * 1.0) - 0.05

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
	return score, ev, winProbability, reasons
}
