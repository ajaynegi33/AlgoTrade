package engine

import (
	"math"
	"time"
)

// processTick executes the zero-allocation hot-path tick update on the actor mailbox goroutine.
func (a *Actor) processTick(tick *ParsedTick) {
	idx, exists := a.tokenMap[tick.Token]
	if !exists {
		return
	}

	a.ticksProcessed.Add(1)
	stock := &a.stocks[idx]

	// Ensure today0915Epoch rolls over cleanly if server stays up across calendar days
	if tick.ExchangeTime > a.today0915Epoch+64800 {
		tIST := time.Unix(tick.ExchangeTime, 0).In(istZone)
		y, m, d := tIST.Date()
		newEpoch := time.Date(y, m, d, 9, 15, 0, 0, istZone).Unix()
		if newEpoch > a.today0915Epoch {
			a.today0915Epoch = newEpoch
			// Reset all tracked stocks' intraday session state for the new trading day
			for _, sIdx := range a.activeIndices {
				st := &a.stocks[sIdx]
				st.LastUpdatedUnix = 0
				st.CurrentBarIdx = 0
				st.PullbackCount = 0
				st.WasExtended = false
				st.BarStartVolume = 0
				st.BarBuyVolume = 0
				st.BarSellVolume = 0
				st.SessionBars = [75]OHLCVBar{}
			}
		}
	}

	// First-tick baseline seeding & bar slot initialization
	if stock.LastUpdatedUnix == 0 {
		stock.Volume = tick.Volume
		stock.BarStartVolume = tick.Volume
		stock.Bid1Price = tick.Bid1Price
		stock.Ask1Price = tick.Ask1Price
		stock.BidLevels = tick.BidLevels
		stock.AskLevels = tick.AskLevels
		stock.LTP = tick.LTP
		stock.RollingPeak = tick.LTP
		stock.Open = tick.Open
		stock.High = tick.High
		stock.Low = tick.Low
		if tick.PrevClose > 0 {
			stock.PrevClose = tick.PrevClose
		}
		if tick.IsIndex {
			if tick.Open > 0 {
				stock.ExchangeVWAP = tick.Open
			} else {
				stock.ExchangeVWAP = tick.LTP
			}
		} else {
			stock.ExchangeVWAP = tick.VWAP
		}
		stock.LastUpdatedUnix = tick.ExchangeTime

		barIdx := (tick.ExchangeTime - a.today0915Epoch) / 300
		if barIdx < 0 {
			barIdx = 0
		} else if barIdx >= 75 {
			barIdx = 74
		}
		stock.CurrentBarIdx = uint8(barIdx)
		stock.SessionBars[barIdx] = OHLCVBar{
			Open:      tick.LTP,
			High:      tick.LTP,
			Low:       tick.LTP,
			Close:     tick.LTP,
			Volume:    0,
			Timestamp: a.today0915Epoch + barIdx*300,
		}
		if stock.IsWarm {
			stock.BaselineSlotVolume = a.slotBaselines[idx][barIdx].MedianVolume
			stock.BaselineCumVolume = a.slotBaselines[idx][barIdx].MedianCumVol
		}
		if w := a.workers[idx]; w != nil {
			w.OnTickHotPath(stock, a.inMacroShock.Load())
		}
		return
	}

	// Update continuous time-decaying RollingPeak (tau = 20s) before overwriting LastUpdatedUnix
	dt := float64(tick.ExchangeTime - stock.LastUpdatedUnix)
	if dt < 0 {
		dt = 0
	}
	if tick.LTP >= stock.RollingPeak || stock.RollingPeak <= 0 {
		stock.RollingPeak = tick.LTP
	} else if dt > 0 {
		decay := math.Exp(-dt / 20.0)
		stock.RollingPeak = tick.LTP + (stock.RollingPeak-tick.LTP)*decay
	}

	// Prevent out-of-order ticks from underflowing uint64
	var tickDeltaVol uint64
	if tick.Volume > stock.Volume {
		tickDeltaVol = tick.Volume - stock.Volume
	}

	// Check 5-minute bar rollover (exchTime / 300)
	barIdx := (tick.ExchangeTime - a.today0915Epoch) / 300
	if barIdx > int64(stock.CurrentBarIdx) && barIdx < 75 {
		// Finalize old bar
		oldIdx := stock.CurrentBarIdx
		stock.SessionBars[oldIdx].Close = stock.LTP
		if stock.Volume >= stock.BarStartVolume {
			stock.SessionBars[oldIdx].Volume = stock.Volume - stock.BarStartVolume
		}

		// Queue old bar for background SQLite flush
		select {
		case a.barFlushChan <- BarFlushItem{Token: stock.InstrumentToken, Bar: stock.SessionBars[oldIdx]}:
		default:
		}

		// Advance to new bar & update time-of-day slot baselines
		stock.CurrentBarIdx = uint8(barIdx)
		if stock.IsWarm {
			stock.BaselineSlotVolume = a.slotBaselines[idx][barIdx].MedianVolume
			stock.BaselineCumVolume = a.slotBaselines[idx][barIdx].MedianCumVol
		}
		stock.SessionBars[barIdx] = OHLCVBar{
			Open:      tick.LTP,
			High:      tick.LTP,
			Low:       tick.LTP,
			Close:     tick.LTP,
			Timestamp: a.today0915Epoch + barIdx*300,
		}

		// Carry over 25% flow memory via bit shift (prevents 0.50 reset drop)
		stock.BarStartVolume = tick.Volume
		stock.BarBuyVolume = stock.BarBuyVolume >> 2
		stock.BarSellVolume = stock.BarSellVolume >> 2
	}

	// Update current 5m bar High/Low/Close/Volume
	currBar := &stock.SessionBars[stock.CurrentBarIdx]
	if currBar.Open == 0 {
		currBar.Open = tick.LTP
	}
	if tick.LTP > currBar.High || currBar.High == 0 {
		currBar.High = tick.LTP
	}
	if tick.LTP < currBar.Low || currBar.Low == 0 {
		currBar.Low = tick.LTP
	}
	currBar.Close = tick.LTP
	if tick.Volume >= stock.BarStartVolume {
		currBar.Volume = tick.Volume - stock.BarStartVolume
	}

	// Save previous quotes BEFORE overwriting scalar fields so Lee-Ready Quote & Tick rules are exact
	prevLTP := stock.LTP
	prevBid1 := stock.Bid1Price
	prevAsk1 := stock.Ask1Price

	// Update stock scalar quotes
	stock.LTP = tick.LTP
	if tick.IsIndex {
		// Indices do not transmit exchange VWAP; maintain synthetic intraday anchor so Nifty > VWAP works
		if stock.ExchangeVWAP <= 0 {
			if tick.Open > 0 {
				stock.ExchangeVWAP = tick.Open
			} else {
				stock.ExchangeVWAP = tick.LTP
			}
		} else {
			stock.ExchangeVWAP = 0.995*stock.ExchangeVWAP + 0.005*tick.LTP
		}
	} else if tick.VWAP > 0 {
		stock.ExchangeVWAP = tick.VWAP
	}
	if tick.Open > 0 {
		stock.Open = tick.Open
	}
	if tick.High > 0 {
		stock.High = tick.High
	}
	if tick.Low > 0 {
		stock.Low = tick.Low
	}
	if tick.PrevClose > 0 {
		stock.PrevClose = tick.PrevClose
	}
	stock.Volume = tick.Volume
	stock.LastUpdatedUnix = tick.ExchangeTime

	if !tick.IsIndex {
		stock.Bid1Price = tick.Bid1Price
		stock.Ask1Price = tick.Ask1Price
		stock.BidLevels = tick.BidLevels
		stock.AskLevels = tick.AskLevels

		// Dual-alpha EMA Smoothing & Lee-Ready Aggressor Flow using prevAsk1/prevBid1/prevLTP
		var alpha float32 = 0.03
		if tickDeltaVol > 0 {
			alpha = 0.20
			var isBuyTick bool
			if tick.LTP >= float64(prevAsk1) && prevAsk1 > 0 {
				isBuyTick = true
			} else if tick.LTP <= float64(prevBid1) && prevBid1 > 0 {
				isBuyTick = false
			} else {
				midpoint := float64(prevBid1+prevAsk1) / 2.0
				if prevBid1 > 0 && prevAsk1 > 0 && tick.LTP != midpoint {
					isBuyTick = tick.LTP > midpoint
				} else {
					isBuyTick = tick.LTP >= prevLTP
				}
			}

			if isBuyTick {
				stock.BarBuyVolume += tickDeltaVol
				stock.FastAggressorEMA = 0.25*1.0 + 0.75*stock.FastAggressorEMA
			} else {
				stock.BarSellVolume += tickDeltaVol
				stock.FastAggressorEMA = 0.25*0.0 + 0.75*stock.FastAggressorEMA
			}
		}

		stock.SmoothedBuyerRatio = alpha*tick.RawBuyerRatio + (1.0-alpha)*stock.SmoothedBuyerRatio

		if totalBarVol := stock.BarBuyVolume + stock.BarSellVolume; totalBarVol >= 500 {
			stock.AggressorBuyRatio = float32(stock.BarBuyVolume) / float32(totalBarVol)
		} else {
			stock.AggressorBuyRatio = 0.50
		}

		// Pullback Hysteresis Latch
		atrSafe := math.Max(stock.ATR14, stock.LTP*0.0025)
		if stock.LTP > stock.ExchangeVWAP+1.0*atrSafe {
			stock.WasExtended = true
		} else if stock.WasExtended && stock.LTP <= stock.ExchangeVWAP+0.50*atrSafe {
			stock.PullbackCount++
			stock.WasExtended = false
		}
	} else if a.hasNifty && idx == a.niftyIdx && stock.RollingPeak > 0 {
		// Instantaneous Nifty 50 Flash Shock detector (-0.18% drop from 20s RollingPeak)
		dropPct := ((stock.LTP - stock.RollingPeak) / stock.RollingPeak) * 100.0
		if dropPct <= -0.18 {
			a.macroShockExp = tick.ExchangeTime + 180
			a.inMacroShock.Store(true)
		}
	}

	// O(1) lock-free hot-path dispatch to active PositionWorker if bound
	if w := a.workers[idx]; w != nil {
		w.OnTickHotPath(stock, a.inMacroShock.Load())
	}
}

