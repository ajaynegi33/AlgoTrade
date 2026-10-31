# AlgoTrade Execution Architecture & Deep Dive Engine Manual

## 1. System Overview & Core Philosophy

The **AlgoTrade Backend** is an ultra-low-latency, single-writer algorithmic trading engine engineered in Go. It operates on real-time Level 2 (5-depth) market data streamed via the **Zerodha Kite Connect WebSocket API (Kite Ticker)**, executing intraday cash equity trades on the **National Stock Exchange of India (NSE)**.

---

### Dual-Layer Architectural Philosophy

#### 1. Beginner-Friendly Mental Model: "The Autonomous Trading Submarine"
Imagine a high-speed submarine navigating stormy seas:
- **The Sonar Officer (Actor Ingestion)**: One person listens to the sonar pings (market ticks). Nobody interrupts or fights over the headphones. They write observations on a single giant board with zero delay.
- **The Navigator (Quant Ranking & Breadth)**: Every second, the navigator looks at the entire fleet of 213 ships (F&O stocks) to check if the tide is rising or falling (market breadth) and picks the top 5 fastest ships.
- **The Safety Officer (Gatekeeper & Portfolio Heat)**: Before firing a torpedo (placing a trade), the safety officer checks the sub's electrical circuit breakers. The boat can only handle $2.0\%$ total electrical load (Portfolio Heat) at any time. If a previous torpedo is already safely at target (trailing stop at Breakeven), its risk load drops to zero, allowing another torpedo to be fired.
- **The Torpedo Guidance Computers (Position Workers)**: Each launched trade gets its own dedicated autonomous computer (`PositionWorker`). It watches the target every millisecond, ratchets its defense shield across 4 mechanical gears, locks in Tax-Free Breakeven at $+1.00R$, takes $50\%$ profit off the table at $+1.50R$, and guarantees it never gets stuck (anti-orphan recovery loop).
- **The Black Box & Flight Recorder (EOD Optimization Engine)**: It records every single decision—including torpedoes we chose *not* to fire (shadow signals)—and at 3:45 PM automatically analyzes whether we were smart or just lucky, emailing an executive briefing to headquarters.

#### 2. Senior Quant & Systems Engineering Pillars
1. **Single-Writer Actor Model (`internal/market`)**: Eliminates read/write mutex contention on tick ingestion. All tick processing, technical indicator calculation, and state updates occur inside a single goroutine running on a contiguous array of stock metrics (`[512]StockMetrics`) with **zero heap allocations** and **< 250ns** per-tick processing time.
2. **Lock-Free Atomic Hot Path (`internal/trading/trailing_stop.go`)**: Active position monitoring, high-water mark tracking, and stop-loss breach detection (`OnTickHotPath`) are executed directly from the Actor's tick loop using hardware-level atomic operations (bit-cast IEEE-754 floats to `uint64`), running with **zero mutex locks** and zero heap allocations.
3. **Fail-Safe Autonomous Execution (`internal/trading`)**: Each active position runs on its own dedicated worker goroutine (`PositionWorker`), managing order lifecycles with strict bounds on exchange modification limits (NSE maximum 25 modifications per order), 5-minute limit order timeouts with viable partial-fill promotion, 3-step race-free scale-outs, and a 4-stage anti-orphan exit recovery protocol.
4. **Continuous Portfolio Heat Engine & EOD Optimization Engine (`internal/trading`, `internal/monitoring`)**: Account equity is guarded by a hard $2.0\%$ Portfolio Heat Cap. Once a winning position ratchets its stop to Breakeven (or above), its open downside risk drops to ₹0.00, releasing portfolio heat and allowing the engine to spawn new continuous trades during the day. All signals—both filled trades and rejected candidate setups—are logged asynchronously via a ring buffer to compute Maximum Favorable Excursion (MFE), Maximum Adverse Excursion (MAE), shadow outcomes, and automated 15:45 IST diagnostic reports emailed via Resend.

---

## 2. High-Level Architecture Flowchart

```mermaid
flowchart TB
    subgraph MarketDataIngestion ["1. Market Data Ingestion"]
        KT["Zerodha Kite Ticker (WebSocket)"] -->|Binary Ticks (184B)| BP["kite_tick_parser.go (L2 5-Depth Unpacker)"]
        BP -->|Parsed Ticks| AC["market_hub.go (tickChan: 65,536 buffer)"]
    end

    subgraph SingleWriterEngine ["2. Single-Writer Quant Engine Actor"]
        AC -->|Single Goroutine| SW["Actor.runLoop()"]
        SW -->|Update <250ns| SM["[512]StockMetrics Contiguous Array"]
        SW -->|Every 1.0s| SNAP["snapshot_publisher.go: computeAndBroadcastSnapshot()"]
        SNAP -->|Market Breadth & Regime| REG["Bottom-Up F&O Breadth Classifier (>=58% / <=40%)"]
        SNAP -->|11 Sectors| SEC["Granular Sector Health & Circuit Breakers"]
        SNAP -->|Quant Ranking| SCR["Quicksort Active Indices by BullishScore (~2µs)"]
        SW -->|O(1) Lock-Free Dispatch| HP["worker.OnTickHotPath()"]
        SNAP -->|Execution Hook (1.0s)| MGR["execution_manager.go: OnSecondTick()"]
    end

    subgraph PreTradeGates ["3. Pre-Trade Gatekeeping & Signal Selection"]
        MGR --> GC["session_clock.go: EvaluateGlobalSessionClocks()"]
        GC -->|Playbook Window| PB["NSE Intraday Session Clock (6 Phases)"]
        MGR --> PBK["entry_gatekeeper.go: PickBestLongCandidate()"]
        PBK -->|Gate 0| G0["Warmup & Active Cooldown Check (15m lockout)"]
        PBK -->|Gate 1| G1["Granular Sector Circuit Breaker (Restricted Sector Check)"]
        PBK -->|Gate 2| G2["Bullish Score & Pre-Trade EV Gate (Score >= 75, EV >= +0.25R)"]
        PBK -->|Gate 3| G3["Order Book Microstructure & Ask Wall Filter (Ask1 <= 4x Top-2 Bid)"]
        PBK -->|Gate 4| G4["Structural Setup: VWAP Pullback Reclaim, Bear Trap Spring, OR 15m ORB Retest"]
        PBK -->|Gate 5| G5["position_sizer.go: ComputePositionSize() (4 Constraints + Heat Cap)"]
        PBK -->|Gate 6| G6["Mandatory Net-Profitability Gate at +1.0R (Net >= 1.5x Taxes)"]
    end

    subgraph ExecutionSubsystem ["4. Autonomous Execution Subsystem"]
        PBK -->|Qualified Setup| SPW["Manager.spawnCandidateWorker()"]
        SPW --> PW["PositionWorker (Dedicated Goroutine)"]
        PW -->|1. Place Entry Limit| BRK["BrokerAdapter (Live Zerodha / Paper)"]
        PW -->|2. Register Order| REC["order_reconciler.go (Deduplication)"]
        PW -->|3. 5m Timeout / Fill| SL["Ensure Initial SL-L Order Placed"]
        PW -->|4. Event Loop| WL["position_worker.go: workerLoop()"]
    end

    subgraph ActiveManagement ["5. Trailing Gears, Scale-Out & Bulletproof Exits"]
        WL -->|Gear 1: Mods 1–18| G1R["Agile Dynamic Trail (min 2 ticks / 0.12 BaseBuffer, cooldown 350ms)"]
        WL -->|Gear 3: Mods 19–24| G3R["Coarse 5m Bar Trail (min 0.40 BaseBuffer, cooldown 1500ms)"]
        WL -->|Gear 4: Mod >= 24| G4S["Virtual Shadow Stop in RAM (Mod 25 Reserved for Exit)"]
        WL -->|+1.50R MFE| SO3["3-Step Race-Free Scale-Out (50% Banked + Tax-Free BE)"]
        WL -->|Breach / Shock / 15:14| FLT["position_exit.go: 4-Stage Bulletproof Exit Protocol"]
        FLT -->|Stage A| MA["Mutate SL to Market (Consumes Reserved Mod 25)"]
        FLT -->|Stage B & C| LPP["Cancel SL -> Escalating LPP Limit Sweeps (0.75% to 3.50%)"]
        FLT -->|Stage D| REC_LOOP["Persistent Anti-Orphan Recovery Loop (Every 2.0s)"]
    end

    subgraph FeedbackAndMonitoring ["6. Reconciliation, Capital & EOD Optimization"]
        BRK -->|REST / Postback / WS| REC
        REC -->|Incremental Fill Deltas| DELTA["PositionWorker.OnOrderDelta()"]
        DELTA --> DB[("SQLite Database (WAL Mode)")]
        DELTA --> CM["risk_manager.go: CapitalManager (Ledger & 3 Circuit Breakers)"]
        PBK -->|Rejections & Trades| EVT["monitoring/collector.go: EventCollector (Ring Buffer)"]
        EVT --> SHAD["monitoring/shadow_tracker.go: Shadow Tracking of Rejected Setups"]
        EVT --> SCHED["monitoring/scheduler.go: EOD Scheduler (15:45 IST)"]
        SCHED --> REP["monitoring/reporter.go: Diagnostic Report Generator"]
        REP --> EML["monitoring/email_resend.go: Resend Transactional Email"]
    end
```

---

## 3. End-to-End Execution Pipeline (Step-by-Step Deep Dive)

---

### Step 1: Market Data Ingestion & Single-Writer Processing

#### Beginner Intuition
The stock market sends tens of thousands of price updates every second. If multiple parts of the program try to read and write to the same stock simultaneously using locks, the system slows to a crawl and crashes. Instead, we use a single thread (the Actor) that owns all market data. It receives updates, modifies memory instantly, and passes notes to others. It processes each price update in under 250 nanoseconds—faster than light travels 75 meters!

#### Exact Calling Chain & Flow
```
Kite Ticker WebSocket 
  ──[Raw Binary Frames]──> internal/kite/ticker.go: readPump()
    ──[Bytes]──> internal/market/kite_tick_parser.go: ParseBinaryFrame()
      ──[ParsedTick]──> actor.tickChan (Buffered 65,536)
        ──[Worker Goroutine]──> internal/market/market_hub.go: Actor.runLoop()
          ──[In-Memory Mutation]──> internal/market/tick_ingestion.go: Actor.processTick()
            ──[O(1) Hot Path]──> internal/trading/trailing_stop.go: PositionWorker.OnTickHotPath()
```

#### Zerodha Level 2 Binary Wire Layout (184 Bytes per Packet)
| Byte Range | Field Name | Data Type | Encoding / Unit | Description |
| :--- | :--- | :--- | :--- | :--- |
| `00:04` | `InstrumentToken` | `uint32` | BigEndian | Unique exchange identifier for the stock |
| `04:08` | `LastTradedPrice` | `int32` | Paise / 100.0 | Most recent trade execution price in ₹ |
| `12:16` | `ExchangeVWAP` | `int32` | Paise / 100.0 | Official Zerodha exchange intraday VWAP |
| `16:20` | `DayVolume` | `uint32` | Scalar Units | Cumulative day traded share count |
| `28:32` | `DayOpen` | `int32` | Paise / 100.0 | Official opening price of the trading session |
| `32:36` | `DayHigh` | `int32` | Paise / 100.0 | Session high price in ₹ |
| `36:40` | `DayLow` | `int32` | Paise / 100.0 | Session low price in ₹ |
| `40:44` | `PrevClose` | `int32` | Paise / 100.0 | Previous day closing price in ₹ |
| `60:64` | `ExchangeTimestamp`| `uint32` | Unix Epoch Sec | Official NSE hardware matching engine timestamp |
| `64:124`| `BidDepth[5]` | `[5]Depth` | 12B $\times$ 5 Levels | 4B Qty, 4B Price, 2B Orders, 2B Reserved |
| `124:184`| `AskDepth[5]` | `[5]Depth` | 12B $\times$ 5 Levels | 4B Qty, 4B Price, 2B Orders, 2B Reserved |

#### Checkpoints, Data Points & Rule Filters Table
| Checkpoint | Inspected Data Point | Exact Rule Filter | Action on Breach / Failure |
| :--- | :--- | :--- | :--- |
| **Token Registration** | `actor.stockIndexByToken[token]` | `exists == true` | Drop tick silently (untracked instrument) |
| **Calendar Day Rollover** | `tick.ExchangeTime` | `> today0915Epoch + 64800` | Reset 75 bar slots, pullback counts, and baseline volumes |
| **Out-of-Order Tick Guard** | `tick.Volume` vs `stock.Volume` | `tick.Volume > stock.Volume` | Set $\Delta_{\text{vol}} = 0$ (prevents `uint64` underflow) |
| **5m Candle Rollover** | `barIdx = (t - epoch) / 300` | `barIdx > stock.CurrentBarIdx` | Finalize candle, push to `barFlushChan`, advance slot |
| **Lee-Ready Aggressor Rule**| `LTP` vs `prevAsk1`, `prevBid1` | $\text{LTP} \ge \text{Ask1} \lor \text{LTP} \le \text{Bid1}$ | Classify trade print as Buyer (+) or Seller (-) volume |

#### Mathematical Formulations
1. **Weighted Anti-Spoof Order Book Ratio**:
   Institutional spoof orders sit on depth levels 3 to 5. We weight levels 1–2 (executable quotes) at $60\%$ and outer levels at $40\%$:
   $$\text{WeightedBidVolume} = 0.60 \times (\text{Bid1Qty} + \text{Bid2Qty}) + 0.40 \times \sum_{i=3}^5 \text{BidQty}_i$$
   $$\text{WeightedAskVolume} = 0.60 \times (\text{Ask1Qty} + \text{Ask2Qty}) + 0.40 \times \sum_{i=3}^5 \text{AskQty}_i$$
   $$\text{RawBuyerRatio} = \frac{\text{WeightedBidVolume}}{\text{WeightedBidVolume} + \text{WeightedAskVolume}}$$

2. **Continuous Exponential Time-Decaying Rolling Peak**:
   Tracks adverse tape crashes with a continuous 20-second half-life ($\tau = 20.0\text{s}$):
   $$\Delta t = \text{ExchangeTime} - \text{LastUpdatedUnix}$$
   $$\text{RollingPeak} = \text{LTP} + (\text{RollingPeak} - \text{LTP}) \times e^{-\Delta t / 20.0}$$

3. **Dual-Alpha Exponential Moving Average Smoothing**:
   $$\alpha = \begin{cases} 0.20 & \text{if traded volume printed } (\Delta_{\text{vol}} > 0) \\ 0.03 & \text{if quote depth changed without trade} \end{cases}$$
   $$\text{SmoothedBuyerRatio}_t = \alpha \times \text{RawBuyerRatio} + (1.0 - \alpha) \times \text{SmoothedBuyerRatio}_{t-1}$$

---

### Step 2: 1-Second Snapshot, Market Breadth & Quant Ranking

#### Beginner Intuition
Every single second, the system takes a bird's-eye photo of the entire market. It asks: "Are more than half the stocks trading above their average price today?" If yes, the tide is with us (Bullish Expansion). If most stocks are sinking, the tide is out (Bearish Expansion), and buying is dangerous. It then scores every stock from 0 to 100 based on price momentum, volume explosion, and order book strength, putting the best candidates at the top of the list.

#### Exact Calling Chain & Flow
```
internal/market/market_hub.go: Actor.runLoop()
  ──[1.0s Ticker Trigger]──> internal/market/snapshot_publisher.go: computeAndBroadcastSnapshot()
    ──[All Stocks]──> Calculate Market Breadth Ratio (% > VWAP)
    ──[11 Sectors]──> Compute Sector Health & Evaluate Sector Circuit Breakers
    ──[Quants]──> internal/market/ranking_algorithm.go: CalculateBullishScore()
    ──[Sorting]──> slices.SortFunc(bullishSorted, compareScores) (~2µs)
    ──[Hook Call]──> internal/trading/execution_manager.go: Manager.OnSecondTick()
    ──[WebSocket Hub]──> internal/ws/hub.go: ClientHub.BroadcastSnapshot()
```

#### Checkpoints, Data Points & Rule Filters Table
| Checkpoint | Inspected Data Point | Exact Rule Filter | Action on Breach / Failure |
| :--- | :--- | :--- | :--- |
| **Breadth Sample Viability**| `activeQuotedCount` | `activeQuotedCount > 0` | Fallback breadth to $0.50$ (neutral) |
| **Bullish Expansion State** | `breadthRatio` | $\text{breadthRatio} \ge 0.58$ | Classify `BULLISH_EXPANSION` |
| **Bearish Expansion State** | `breadthRatio` | $0.20 < \text{breadthRatio} \le 0.40$ | Classify `BEARISH_EXPANSION` |
| **Macro Flash Shock State** | `breadthRatio` | $\text{breadthRatio} \le 0.20$ | Classify `FLASH_SHOCK` & Lock Gate (`LOCKED_MACRO_SHOCK`) |
| **Sector Weak Breadth Lock**| `secPerf.breadth` | $\text{breadth} < 40.0\%$ | Mark `IsRestricted = true` ("Weak Breadth") |
| **Sector Heavy Dump Lock**  | `secPerf.avgChg` | $\text{avgChg} < -0.50\%$ | Mark `IsRestricted = true` ("Heavy Selling Pressure") |
| **WebSocket Broadcast Guard**| `actor.wsHub.ClientCount()` | `== 0` | Skip DTO allocations & network serialization |

#### Mathematical Formulations
1. **Bottom-Up Market Breadth Ratio**:
   $$\text{BreadthRatio} = \frac{\sum_{i=1}^N \mathbf{1}_{\{ \text{LTP}_i > \text{ExchangeVWAP}_i \}}}{N_{\text{active}}}$$

2. **5-Factor Institutional Composite Bullish Rank Score ($0 \dots 100$)**:
   $$\text{Score} = (0.26 \times S_{\text{vwap}}) + (0.22 \times S_{\text{rvol}}) + (0.18 \times S_{\text{rs}}) + (0.20 \times S_{\text{flow}}) + (0.14 \times S_{\text{ev}})$$
   - **Gaussian VWAP Sweet-Spot Curve ($S_{\text{vwap}}$)**:
     $$\Delta_{\text{vwap}} = \frac{\text{LTP} - \text{ExchangeVWAP}}{\text{ATR}_{\text{safe}}}$$
     $$S_{\text{vwap}} = \begin{cases} 0.0 & \text{if } \Delta_{\text{vwap}} < -0.10 \\ 45.0 & \text{if } -0.10 \le \Delta_{\text{vwap}} < 0.00 \\ 100.0 - 150.0 \times (\Delta_{\text{vwap}} - 0.25)^2 & \text{if } 0.00 \le \Delta_{\text{vwap}} \le 0.60 \quad (\text{Peaks at } +0.25\text{ ATR}) \\ 80.0 - \frac{\Delta_{\text{vwap}} - 0.60}{0.90} \times 60.0 & \text{if } 0.60 < \Delta_{\text{vwap}} \le 1.50 \\ 10.0 & \text{if } \Delta_{\text{vwap}} > 1.50 \quad (\text{Overextended penalty}) \end{cases}$$
   - **Sigmoidal RVOL Score ($S_{\text{rvol}}$)**:
     $$S_{\text{rvol}} = \min\left(100.0, \frac{105.0}{1.0 + e^{-1.65 \times (\text{CompositeRVOL} - 2.0)}}\right)$$
   - **Hyperbolic Tangent Relative Strength Score ($S_{\text{rs}}$)**:
     $$S_{\text{rs}} = \min(100.0, \max(0.0, 50.0 + 48.4 \times \tanh(0.65 \times \text{CompositeRS})))$$
   - **Order Flow & Aggressor Score ($S_{\text{flow}}$)**:
     $$S_{\text{flow}} = \min(100.0, \max(0.0, (\text{FlowComposite} - 0.35) \times 200.0))$$
   - **Expected Value Score ($S_{\text{ev}}$)**:
     $$S_{\text{ev}} = \min(100.0, \max(0.0, (\text{EV} + 0.40) \times 55.0))$$
   *(Penalty Rule: If spread $> 0.10\%$ or price < previous close, $\text{Score} \gets \text{Score} \times 0.60$ (40% haircut)).*

---

### Step 3: Global Session Clocks & Candidate Pre-Trade Gatekeeping

#### Beginner Intuition
Not all hours of the day are equal. Trading at 9:16 AM is like gambling in a casino during an earthquake (high noise, wide spreads). Trading at 12:30 PM is often slow and sluggish. The Session Clock divides the day into 6 distinct phases with strict rules. When a stock looks good, it must pass 6 tough exams (Gates) before we even consider sizing it. If its sector is crashing, or a massive seller has built an "Ask Wall", it gets rejected immediately.

#### Exact Calling Chain & Flow
```
internal/trading/execution_manager.go: Manager.OnSecondTick()
  ──> internal/trading/session_clock.go: EvaluateGlobalSessionClocks()
  ──> internal/trading/entry_gatekeeper.go: PickBestLongCandidate()
        ├── Scan Top 5 Bullish Outliers
        ├── Gate 0: Warmup & Cooldown Check
        ├── Gate 1: Granular Sector Circuit Breaker
        ├── Gate 2: Score & RVOL Gate
        ├── Gate 3: Microstructure & Ask Wall Filter
        ├── Gate 4: Structural Setup Pattern Match
        └── Gate 5: Sizing & Portfolio Heat Validation
```

#### Checkpoints, Data Points & Rule Filters Table
| Gate Checkpoint | Inspected Data Point | Exact Rule Filter | Rejection Reason / Action |
| :--- | :--- | :--- | :--- |
| **Session Clock** | `sessionClock.IsNewEntryAllowed` | `== true` | Lock entries (`LOCKED_SESSION_WINDOW`) |
| **Gate 0: Warmup** | `stock.IsWarm`, `stock.CurrentBarIdx` | `IsWarm == true && BarIdx >= 1` | `Warming up 5m bars / VWAP feed` |
| **Gate 0: Cooldown**| `symbolCooldowns[symbol]` | `time.Now() < cooldownExpiry` | `Position active or in 15m cooldown` |
| **Gate 1: Sector** | `sectorHealthMap[sectorID].IsRestricted`| `== false` | `Sector is RESTRICTED: [Reason]` |
| **Gate 2: Score** | `stock.BullishScore`, `stock.EV` | `Score >= minScore && EV >= +0.25R` | `Score < threshold or EV < 0.25R` |
| **Gate 2: RVOL** | `stock.CompositeRVOL` | `CompositeRVOL >= minRVOL` | `Composite RVOL < threshold` |
| **Gate 3: Ask Wall**| `Ask1Qty` vs `(Bid1Qty + Bid2Qty)`| $\text{Ask1Qty} \le 4 \times (\text{Bid1} + \text{Bid2})$| `Ask1 Wall > 4x Top-2 Bid Depth` |
| **Gate 4: Pattern**| `PatternMatched` | `!= ""` (Must match Setup 1, 2, or 3) | `Awaiting VWAP Reclaim, Bear Trap Spring, or ORB Retest` |

#### Mathematical Formulations
1. **Strategy Setup 1: VWAP Pullback Reclaim (`VWAP_PULLBACK_RECLAIM`)**:
   - Trend Confluence (Richie Naso Law): $\text{EMA}_9 > 0 \land \text{EMA}_9 \ge \text{ExchangeVWAP} \land \frac{\Delta\text{EMA}_9}{\text{ATR}_{\text{safe}}} \ge -0.05$.
   - Price Zone: $0.05 \le \frac{\text{LTP} - \text{ExchangeVWAP}}{\text{ATR}_{\text{safe}}} \le 0.55$.
   - Dip Depth: $\text{PreviousBar.Low} \le \text{ExchangeVWAP} + 0.35\text{ATR}_{\text{safe}}$ and $\ge \text{ExchangeVWAP} - 0.15\text{ATR}_{\text{safe}}$.
   - Reclaim Condition:
     $$\text{LTP} > \max\left(\text{CurrentBar.Open} + 2\text{ ticks}, \frac{\text{PreviousBar.Open} + \text{PreviousBar.Close}}{2.0}\right)$$
   - Flow Confirmation: $\text{CLV} \ge 0.58$, $\text{AggressorBuyRatio} \ge 0.56$, $\text{FastAggressorEMA} \ge 0.55$, $\text{SmoothedBuyerRatio} \ge 0.55$.
   - Volatility-Buffered Stop: $\min(\text{PreviousBar.Low}, \text{CurrentBar.Low}) - (0.20 \times \text{ATR}_{\text{safe}})$.

2. **Strategy Setup 2: Failed Breakdown Reclaim / Bear Trap Spring (`FAILED_BREAKDOWN_SPRING`)**:
   - Specialist Liquidity Sweep (Richie Naso & Rishi Narang Law): Institutional market makers flush stops below VWAP before surging back into value.
   - Dip Sweep: $\text{ExchangeVWAP} - 0.45\text{ATR}_{\text{safe}} \le \text{PreviousBar.Low} < \text{ExchangeVWAP} - 0.05\text{ATR}_{\text{safe}}$.
   - Rapid Snapback Reclaim: $\text{LTP} \ge \text{ExchangeVWAP} + 0.05\text{ATR}_{\text{safe}} \land \text{LTP} > \text{CurrentBar.Open} + 2\text{ ticks}$.
   - Aggression & Absorption: $\text{CompositeRVOL} \ge 1.5$, $\text{CLV} \ge 0.60$, $\text{AggressorBuyRatio} \ge 0.58$, $\text{FastAggressorEMA} \ge 0.56$.
   - Volatility-Buffered Stop: $\text{PreviousBar.Low} - (0.20 \times \text{ATR}_{\text{safe}})$.

3. **Strategy Setup 3: 15-Minute Opening Range Retest Reclaim (`ORB_RETEST_RECLAIM`)**:
   - Abolishes Naive 80%-Failure First Breakout: First poke beyond 15m range high is a retail trap. We require a completed 5-minute candle close above $\text{ORH}$, followed by a retest of the range high that holds as confirmed support before entry.
   - Range Compression: $\text{ORH} - \text{ORL} \le 1.8 \times \text{ATR}_{\text{safe}}$.
   - Prior Confirmed Breakout Close: At least one completed 5m bar with $\text{Close} > \text{ORH}$.
   - Retest Support Hold: $\text{ORH} - 0.35\text{ATR}_{\text{safe}} \le \text{PreviousBar.Low} \le \text{ORH} + 0.25\text{ATR}_{\text{safe}}$.
   - Reclaim Extension: $\text{ORH} < \text{LTP} \le \text{ORH} + 0.45\text{ATR}_{\text{safe}} \land \text{LTP} > \text{CurrentBar.Open} + 1\text{ tick}$.
   - Volume & Aggression: $\text{CompositeRVOL} \ge 1.8$, $\text{CLV} \ge 0.62$, $\text{AggressorBuyRatio} \ge 0.58$, $\text{FastAggressorEMA} \ge 0.56$.
   - Volatility-Buffered Stop: $\min(\text{PreviousBar.Low}, \text{CurrentBar.Low}) - (0.20 \times \text{ATR}_{\text{safe}})$.

---

### Step 4: Position Sizing, 4 Institutional Constraints & Continuous Portfolio Heat

#### Beginner Intuition
How many shares should we buy? A rookie asks: "How much money do I have?" A professional quant asks: "How much am I allowed to lose if this trade hits its stop loss?" We calculate the distance to our protective stop, cap our risk at a fraction of account equity, and then run 4 strict safety limits (Risk, Margin, Concentration, and Liquidity). Most importantly: we enforce a **2.0% Total Portfolio Heat Cap**. If you have 3 open trades, their combined downside risk cannot exceed 2.0% of your account. Once a trade moves into profit and we trail its stop to breakeven, its risk drops to ₹0, freeing up room for the next trade!

#### Exact Calling Chain & Flow
```
internal/trading/entry_gatekeeper.go: PickBestLongCandidate()
  ──> internal/trading/position_sizer.go: ComputePositionSize()
        ├── internal/trading/stoploss.go: ComputeStopAndBufferSpec()
        ├── Check Scale-Invariant Risk Curve: EffectiveRiskPerTradePct()
        ├── Check Portfolio Heat Budget: min(RiskBudget, RemainingHeatBudgetRupees)
        ├── Apply 4 Institutional Sizing Constraints (Min across all 4)
        ├── internal/trading/tax_calculator.go: CalculateRoundTripCharges()
        └── Validate Mandatory Net-Profitability Gate (NetProfit >= 1.5x Taxes)
```

#### Checkpoints, Data Points & Rule Filters Table
| Sizing Checkpoint | Inspected Data Point | Exact Rule Filter | Rejection Reason / Action |
| :--- | :--- | :--- | :--- |
| **Minimum Capital** | `netEquity`, `freeMargin` | `netEquity >= 500 && freeMargin > 0` | `insufficient capital` |
| **No-Chase Rule** | Stop Distance | $\text{Entry} - \text{RawStop} \le 1.25 \times \Delta_{\max}$ | `stop distance exceeds chase cap` |
| **Portfolio Heat Cap**| `remainingHeatBudgetRupees`| `remainingHeatBudgetRupees > 0` | `portfolio heat capacity exhausted` |
| **Liquidity Cap** | Top-3 Visible Ask Depth | $Q \le 0.20 \times \sum_{i=1}^3 \text{AskQty}_i$ | Cap quantity to prevent market impact |
| **Net Profit Gate** | Net Profit at $+1.0R$ vs Taxes | $\text{NetProfit}_{1R} \ge 1.50 \times \text{TotalTaxes}$ | `net profit at +1R insufficient vs taxes` |
| **Dust Floor** | Viable share count | $Q_{\text{filled}} \ge \text{DustFloorQty}$ | Flag for partial fill dust guard |

#### Mathematical Formulations
1. **Non-Inverting Stop Loss & Breathing Buffer Bounds**:
   $$\Delta_{\min} = \max(4\text{ ticks}, \max(0.35\% \times \text{LTP}, \min(0.35 \times \text{ATR}_{\text{safe}}, 0.80\% \times \text{LTP})))$$
   $$\Delta_{\max} = \max(\Delta_{\min} + 4\text{ ticks}, \min(1.15 \times \text{ATR}_{\text{safe}}, \max(1.50\% \times \text{LTP}, 0.85 \times \text{ATR}_{\text{safe}})))$$
   $$\text{BaseBuffer} = \text{RoundToTick}(\text{clamp}(\Delta_{\min}, \Delta_{\max}, \max(0.75 \times \text{ATR}_{\text{safe}}, 0.0075 \times \text{LTP})))$$
   $$\text{OneR} = \text{RoundUpToTick}(\text{clamp}(\Delta_{\min}, \Delta_{\max}, \text{EntryLimitPrice} - (\text{SignalBarLow} - 2\text{ ticks})))$$
   $$\text{InitialStop} = \text{RoundDownToTick}(\text{EntryLimitPrice} - \text{OneR})$$

2. **Scale-Invariant Adaptive Risk Curve**:
   $$\text{RiskPct} = \begin{cases} 1.25\% & \text{if Net Equity } \le \text{₹}15,000 \quad (\text{utilizes 5x MIS on ₹10k}) \\ 1.00\% & \text{if Net Equity } \le \text{₹}50,000 \\ 0.75\% & \text{if Net Equity } \le \text{₹}2,00,000 \\ 0.50\% & \text{if Net Equity } \le \text{₹}50,00,000 \\ 0.35\% & \text{if Net Equity } > \text{₹}50,00,000 \quad (\text{Institutional wealth preservation}) \end{cases}$$

3. **Continuous Portfolio Heat Ceiling**:
   $$\text{MaxPortfolioHeatRupees} = 0.02 \times \text{NetEquity}$$
   $$\text{TotalOpenRiskRupees} = \sum_{w \in \text{workers}} w.\text{GetOpenRiskRupees}()$$
   $$\text{RemainingHeatBudgetRupees} = \max(0.0, \text{MaxPortfolioHeatRupees} - \text{TotalOpenRiskRupees})$$
   $$\text{RiskBudget} = \min(\text{NetEquity} \times \text{RiskPct} \times \text{SizeMult} \times \text{DrawdownMult}, \text{RemainingHeatBudgetRupees})$$

4. **The 4 Institutional Sizing Constraints**:
   $$Q_{\text{risk}} = \left\lfloor \frac{\text{RiskBudget}}{\text{OneR} + \text{PerShareFriction}} \right\rfloor$$
   $$Q_{\text{margin}} = \left\lfloor \frac{0.95 \times \text{FreeMargin} \times \text{Leverage}}{\text{EntryLimitPrice}} \right\rfloor \quad (\text{5\% RMS safety buffer})$$
   $$Q_{\text{conc}} = \left\lfloor \frac{\text{ConcFactor} \times \text{NetEquity} \times \text{Leverage}}{\text{EntryLimitPrice}} \right\rfloor \quad (\text{ConcFactor} = 0.35 \text{ standard}, 0.95 \text{ micro } < \text{₹}25\text{k})$$
   $$Q_{\text{liq}} = \max\left(1, \left\lfloor 0.20 \times \sum_{i=1}^3 \text{AskQty}_i \right\rfloor\right) \quad (\text{Never consume } > 20\% \text{ of top-3 ask depth})$$
   $$Q_{\text{final}} = \min(Q_{\text{risk}}, Q_{\text{margin}}, Q_{\text{conc}}, Q_{\text{liq}})$$

---

### Step 5: Position Worker Lifecycle, 5-Minute Timeout & Partial Fill Promotion

#### Beginner Intuition
When an entry order is sent to the exchange, we don't buy at market price (which risks slippage). We place a limit order at the best ask price $+ 1$ tick. But what if the market runs away and doesn't fill our order? We don't want an old order sitting around for hours. The worker starts a strict 5-minute timer. If after 5 minutes nothing has filled, we cancel it. If only some shares filled, we don't panic: if the filled shares are enough to cover taxes (the Dust Floor), we promote it to an active trade, calculate a custom stop loss, and manage it!

#### Exact Calling Chain & Flow
```
internal/trading/execution_manager.go: Manager.spawnCandidateWorker()
  ──> internal/trading/position_worker.go: PositionWorker.Start()
        ├── Place Limit Buy Order via broker.PlaceLimitBuy()
        ├── Register order with order_reconciler.go
        ├── go worker.workerLoop() (Dedicated Goroutine)
        └── Start 5-Minute Timeout: time.NewTimer(300 * time.Second)
```

#### Checkpoints, Data Points & Rule Filters Table
| Lifecycle State | Inspected Data Point | Exact Condition / Event | Transition / Next Action |
| :--- | :--- | :--- | :--- |
| `ENTRY_PLACED` | 5m Timer Expiry | `time.After(300s)` triggers | Call `broker.CancelOrder`, sleep 500ms drain window |
| `ENTRY_PLACED` | `filledEntryQty` | `filled == 0` after cancel drain | Transition to `STATE_CLOSED` (`ENTRY_TIMEOUT_UNFILLED`) |
| `ENTRY_PLACED` | `filledEntryQty` | $0 < \text{filled} < \text{DustFloorQty}$ | Transition to `STATE_EMERGENCY_FLATTEN` (`DUST_GUARD`) |
| `ENTRY_PLACED` | `filledEntryQty` | $\text{filled} \ge \text{DustFloorQty}$ | **Promote partial fill!** Recalculate BE stop, place SL-L |
| `PROTECTED_ACTIVE`| Realized R-Multiple | $\text{MFE} \ge +1.00R$ | **Ratchet stop to Tax-Free Breakeven** (avoids Brownian noise) |
| `PROTECTED_ACTIVE`| Realized R-Multiple | $\text{MFE} \ge +1.50R$ | Transition to `STATE_SCALE_OUT_PENDING` (Tranche 1 50% harvest) |
| `PROTECTED_ACTIVE`| Modification Count | `modCount >= 24` | Transition to `STATE_SHADOW_STOP_ACTIVE` (Gear 4) |

#### Partial Fill Promotion & 500ms Drain Window
```go
// Cancel remaining unfilled portion of entry order
if entryOrdID != "" {
    _ = worker.broker.CancelOrder(worker.ctx, entryOrdID)
    time.Sleep(500 * time.Millisecond) // Drain window to absorb late fills before final check
    _, _ = worker.reconciler.SyncOrdersCoalesced(worker.ctx, worker.broker)
}

filled = int(worker.filledEntryQty.Load())
if filled <= 0 {
    // 0 shares filled: close cleanly
    worker.finalizeClosedLocked("ENTRY_TIMEOUT_UNFILLED")
    return
}

if filled < worker.DustFloorQty {
    // Unviable dust fill: flatten immediately
    worker.executeEmergencyFlatten("PARTIAL_FILL_DUST_GUARD")
    return
}

// Viable partial fill: promote to active trade!
runnerQty := max(1, filled - (filled / 2))
beStop := BreakevenTriggerPrice(worker.GetEntryPrice(), filled, runnerQty, worker.TickSize)
worker.InitialQty = filled
worker.BreakevenStop = beStop
worker.ensureInitialSLPlaced()
```

---

### Step 6: Trailing Stop Loss Gears, Scale-Out & Bulletproof Exits

#### Beginner Intuition
A trailing stop is like a one-way ratchet: as the stock price climbs higher, your stop-loss order moves up behind it, locking in profit. If the price turns around, you exit with money banked. But the exchange has a strict law: **maximum 25 modifications per order**. If you modify your order every second, you'll hit 25 in two minutes and the exchange will reject your orders! 
Our engine solves this using **Mechanical Gears**:
- **Gear 1 (Agile)**: Moves fast in small steps (Mods 1–18).
- **Gear 3 (Coarse)**: Steps back and moves only when a full 5-minute candle low moves up (Mods 19–24).
- **Gear 4 (Shadow Stop)**: At modification 24, we stop touching the exchange order completely! The stop trails virtually in RAM. If breached, we use our **strictly preserved Modification #25** to convert the stop directly to a market order.

#### Exact Calling Chain & Flow
```
internal/market/tick_ingestion.go: Actor.processTick()
  ──[<250ns Hot Path]──> internal/trading/trailing_stop.go: PositionWorker.OnTickHotPath()
    ├── Update High-Water Mark (HWM) & Low-Water Mark (LWM) via atomic CAS
    ├── Guard A: 200ms SL breach latency watchdog
    ├── Guard B: Gear 4 Virtual Shadow Stop check (modCount >= 24)
    ├── Guard C: +1.50R Scale-out trigger (isTranche1Pending = true)
    ├── Guard D: Breakeven Ratchet at +1.00R (avoids Brownian noise stop-out)
    ├── Guard E: 45-Minute In-Trade Alpha Decay Stagnation Watchdog (HWM < +0.50R)
    ├── Compute Dynamic Multipliers (M_flow, C_climax)
    └── Call ProposeStopRatchet() ──[wakeWorker()]──> workerLoop
          └── internal/trading/position_exit.go: executeThreeStepScaleOut()
```

#### Trailing Stop Loss Gears Specification
| Gear | Mod Quota | Minimum Advance Requirement | Minimum Cooldown | Architectural Objective |
| :--- | :--- | :--- | :--- | :--- |
| **Gear 1: Agile** | Mods 1–18 | $\ge \max(2\text{ ticks}, 0.12 \times \text{BaseBuffer})$ | **350 ms** | Fast initial protection; moves quickly out of danger zone |
| **Gear 3: Coarse**| Mods 19–24| $\ge \max(0.40 \times \text{BaseBuffer}, 0.35 \times \text{ATR})$ | **1500 ms** | Preserves exchange modifications by anchoring to 5m structural lows |
| **Gear 4: Shadow**| Mod $\ge 24$| Virtual in RAM (`shadowStopBits`) | **0 ms (In-Memory)**| **Preserves Modification #25** exclusively for emergency market exit |

#### Race-Free 3-Step Scale-Out Protocol (`executeThreeStepScaleOut`)
Triggered when trade reaches $+1.50R$ (Tranche 1 Asymmetric Profit Harvest per Dr. Samir Varma):
1. **Step 1 (Shrink SL)**: Shrink resting SL order quantity from $Q \to Q_{\text{runner}}$ while keeping trigger price unchanged.
2. **Step 2 (Marketable Sell)**: Place marketable limit sell for $Q_{\text{scale}} = Q / 2$.
   - **Compensating Rollback**: If Step 2 fails, immediately re-expands resting SL quantity back to $Q$.
3. **Step 3 (Raise to Tax-Free Breakeven)**: Awaits fill confirmation for up to **800ms**. Cancels any unfilled remainder, reconciles, and raises runner SL trigger to **Tax-Free Breakeven**:
   $$\text{BreakevenStop} = \text{EntryPrice} + \frac{\text{TotalTaxesEntireTrade}}{Q_{\text{runner}}} + \max(2\text{ ticks}, 0.15\% \times \text{EntryPrice})$$

#### In-Trade Alpha Decay Stagnation Guard
- **Dr. Samir Varma & Rishi Narang Quant Law**: Intraday momentum signals have an information half-life $\lambda$ of 15–25 minutes. If a position has been active for $\ge 45\text{ minutes}$ and its peak favorable excursion has not even reached $+0.50R$, the edge has decayed into random walk transaction friction.
- **Action**: Flatten immediately at market with exit reason `ALPHA_DECAY_45M_STAGNATION`, reclaiming margin and risk budget for fresh institutional setups.

#### 4-Stage Bulletproof Exit Protocol (`executeEmergencyFlatten`)
- **Stage A (Mutate to Market)**: Mutate resting SL order directly into `MARKET` (consumes reserved Modification #25). Awaits fill for up to **900ms**.
- **Stage B & C (Escalating LPP Limit Sweeps)**: Cancel resting SL $\to$ Reconcile $\to$ 4-attempt escalating Liquidity Protection Price (LPP) sweep sell:
  - Attempt 1: $0.75\%$ slippage band (waits up to **900ms**)
  - Attempt 2: $1.50\%$ slippage band (waits up to **900ms**)
  - Attempt 3: $2.50\%$ slippage band (waits up to **900ms**)
  - Attempt 4: $3.50\%$ slippage band / Market order (waits up to **900ms**)
- **Stage D (Persistent Anti-Orphan Recovery Loop)**: If position remains open after 4 attempts (e.g. lower circuit freeze or network partition), enters a persistent **2.0-second recovery loop** (`time.NewTicker(2 * time.Second)`) retrying until `RemainingQty() == 0`.

---

### Step 7: Multi-Channel Order Reconciliation, Capital Management & Circuit Breakers

#### Beginner Intuition
In live trading, messages get lost or arrive out of order. A fill might arrive via WebSocket before the REST API confirms the order was placed! The Reconciler acts like a strict accountant. It tracks the exact cumulative filled shares per order and only emits positive fill differences ($\Delta_{\text{filled}}$). If an account hits a bad losing streak, the Capital Manager slams on the brakes (Circuit Breakers) to protect your money.

#### Exact Calling Chain & Flow
```
Broker / WebSocket Frame / Postback Webhook
  ──> internal/trading/order_reconciler.go: OrderReconciler.ProcessOrderEvent()
        ├── Deduplicate fills: delta = max(0, ev.FilledQty - lastFilledByOrd[ordID])
        ├── Dispatch delta to PositionWorker.OnOrderDelta()
        └── On Position Closure ──> internal/trading/risk_manager.go: CapitalManager.RecordClosedTrade()
              ├── Update daily PnL counters (Gross, Charges, Net)
              ├── Evaluate 3 Institutional Circuit Breakers
              └── Persist to daily_risk_ledger table in SQLite
```

#### Checkpoints, Data Points & Rule Filters Table
| Circuit Breaker | Inspected Data Point | Exact Rule Filter | System Action |
| :--- | :--- | :--- | :--- |
| **Max Daily Loss** | `cumNetPnL` vs `baseCapital` | $\text{cumNet} \le -0.020 \times \text{baseCapital}$ | Lock trading for the day (-2.0% cap) |
| **Consecutive Losses**| `ledger.ConsecutiveLosses` | $\ge 2$ | Lock trading for the day |
| **Profit-Giveback Lock**| `cumNet` vs `PeakNetPnL` | $\text{Peak} \ge 2.0R \land \text{cumNet} \le 0.55 \times \text{Peak}$ | Lock trading (protect banked green day) |
| **Dynamic Drawdown 1**| `ledger.ConsecutiveLosses` | $== 1$ | Set $\text{DrawdownMult} = 0.85$ (15% size cut) |
| **Dynamic Drawdown 2**| `ledger.ConsecutiveLosses` | $\ge 2$ | Set $\text{DrawdownMult} = 0.50$ (50% size cut) |

---

### Step 8: Monitoring, Logging & End-of-Day (EOD) Optimization Engine

#### Beginner Intuition
Saying "I made ₹4,000 today" is meaningless in trading. Did you trade well, or did you get lucky? The EOD engine measures the true quality of your trading:
- **Maximum Adverse Excursion (MAE)**: How deep in the red did your winning trades go before turning green? If your winners never went below $-0.4R$, your initial stops are way too wide and wasting capital!
- **Maximum Favorable Excursion (MFE)**: How much profit did your trades reach before your trailing stop closed them? If trades regularly hit $+3.0R$ but exit at $+1.1R$, your trailing stop is too loose.
- **Shadow Tracking**: It tracks all the trades we rejected. If rejected trades performed better than our actual trades, our filters are backwards!
At 3:45 PM every day, an automated report is generated and emailed to your inbox.

#### Exact Calling Chain & Flow
```
Execution Engine (Gatekeeper / PositionWorker)
  ──[Non-Blocking Channel]──> internal/monitoring/collector.go: EventCollector (2048 Ring Buffer)
    ──[500ms / 100 items Batch]──> SQLite Tables (shadow_signals, positions)
      ──[15:45 IST Scheduler]──> internal/monitoring/scheduler.go: EODScheduler.watchLoop()
        ──> internal/monitoring/shadow_tracker.go: EvaluateShadowSignals()
        ──> internal/monitoring/metrics.go: ComputeEODMetrics()
        ──> internal/monitoring/reporter.go: GenerateMarkdownReport() & GenerateHTMLReport()
        └── internal/monitoring/email_resend.go: SendEODReportEmail() ──> Trader Inbox
```

#### Diagnostic Metrics Computed at 15:45 IST
1. **MFE vs. MAE Analysis**:
   $$\text{MFE}_R = \frac{\text{HighWaterMark} - \text{EntryPrice}}{\text{OneR}}$$
   $$\text{MAE}_R = \frac{\text{LowWaterMark} - \text{EntryPrice}}{\text{OneR}} \quad (\text{Expressed as negative } R)$$
2. **Shadow Tracking Counterfactuals**:
   Evaluates subsequent 5m bars of rejected setups:
   - Did price hit $+1.0R$ target first? $\implies \text{Result: WIN\_1R}$.
   - Did price hit hypothetical stop first? $\implies \text{Result: LOSS\_1R}$.
   - Neither reached by 15:15 IST? $\implies \text{Result: EOD\_EXPIRE}$.
3. **Slippage & Friction Audit**:
   $$\text{SlippagePct} = \frac{\text{ActualFillPrice} - \text{SignalTriggerPrice}}{\text{SignalTriggerPrice}} \times 100\%$$
   Flags illiquid equities where average entry slippage exceeds $0.08\%$.
4. **Probability Calibration Bucketing**:
   Buckets trades into pre-trade probability tiers (50–60%, 60–70%, 70%+) and compares against realized empirical win rates.

---

### Step 9: Runtime Execution Tracer Flight Recorder & Latency Watchdog

#### Beginner Intuition
Like an airplane's black box, the **Flight Recorder** continuously records everything happening inside the Go runtime—goroutine context switches, lock contention, Garbage Collector stop-the-world pauses, and network stalls—into a 8MB circular memory ring buffer.
Under normal conditions, it performs **zero disk writes** and incurs negligible CPU overhead (< 0.5%). If a sudden latency spike occurs (e.g., market tick processing exceeds 10ms or order placement exceeds 1500ms), the watchdog automatically dumps the black box snapshot to a `.trace` file on disk. You can open this file in Google Chrome to inspect the exact microsecond of delay.

#### Package Architecture (`internal/diagnostics/`)
```
internal/diagnostics/
  ├── flight_recorder.go      # Go runtime/trace circular ring buffer & background snapshot worker
  ├── storage.go              # Atomic file persistence, companion JSON metadata & retention pruning
  ├── handler.go              # Protected HTTP endpoints (/api/diagnostics/flight-recorder/*)
  ├── types.go                # Configuration, metadata DTOs & status structs
  └── flight_recorder_test.go # Comprehensive unit tests with race detection
```

#### Calibrated Watchdog Hook Points
| Watchdog Target | Location | Normal Latency | Spike Threshold | What a Spike Diagnostic Reveals |
| :--- | :--- | :--- | :--- | :--- |
| **Market Snapshot & Broadcast** | `market/snapshot_publisher.go` | 0.2ms – 0.8ms | **> 10ms** | STW GC pauses, CPU core starvation, or lock contention in actor metrics. |
| **Zerodha Order REST Operations**| `trading/broker_adapters.go` | 150ms – 300ms | **> 1500ms** | Zerodha Kite Gateway network congestion, TCP TLS handshake stalls, or connection pool exhaustion. |
| **Emergency Position Flatten** | `trading/position_exit.go` | 10ms – 50ms | **> 800ms** | Order cancellation delays or thread scheduling lag during critical market exit. |
| **SQLite Monitoring Batch Persist**| `monitoring/collector.go` | 1ms – 5ms | **> 50ms** | Disk checkpoint lock, SQLite WAL write contention, or SSD I/O latency. |

#### Developer Inspection Workflow
```bash
# 1. Trigger an on-demand manual trace snapshot (or wait for an automated watchdog spike)
curl -X POST http://localhost:8080/api/diagnostics/flight-recorder/dump -H "Authorization: Bearer <TOKEN>"

# 2. Inspect the saved trace snapshot directly in your browser
go tool trace data/traces/trace_20261030_153012_market_snapshot_broadcast.trace
```

---

## 4. Comprehensive Inventory of System Waits, Pauses & Timers

Every wait, pause, sleep, and timer in the engine is purpose-built to handle network latencies, broker rate limits, or exchange rules. Nothing is guesswork.

| Wait / Timer Duration | Exact Code Location | Trigger / Function | In-Depth Technical Rationale |
| :--- | :--- | :--- | :--- |
| **1.0 second** | `market/market_hub.go:449` | `snapshotTicker` | Periodic quant heartbeat. Balances sub-second market breadth recalculation and sorting with CPU cache efficiency. |
| **5 minutes (300s)** | `trading/position_worker.go:432` | `entryTimeout` | Prevents resting limit orders from remaining open indefinitely as stale liquidity, while providing institutional pullback setups sufficient auction time to fill without chasing. |
| **500 milliseconds** | `trading/position_worker.go:493` | `handleEntryTimeout` (`time.Sleep`) | **Cancellation Drain Window**: After sending cancel request to Zerodha/NSE, absorbs in-flight fill updates racing across the network before taking a final partial-fill inventory. |
| **350 milliseconds** | `trading/trailing_stop.go:210` | `ProposeStopRatchet` (Gear 1) | **Agile Ratchet Cooldown**: Enforces debounce window on fast tick noise, preserving exchange modification quota (NSE max 25 per order). |
| **1500 milliseconds** | `trading/trailing_stop.go:212` | `ProposeStopRatchet` (Gear 3) | **Coarse Trail Cooldown**: Dampens stop movements during the runner phase, allowing 5m candle low support to develop. |
| **800 milliseconds** | `trading/position_exit.go:276` | `executeThreeStepScaleOut` (`waitTimer`) | **Scale-Out Fill Timer**: Awaits marketable sell fill notification from exchange; if unconfirmed at 800ms, triggers coalesced broker sync. |
| **900 milliseconds** | `trading/position_exit.go:381` | `executeEmergencyFlatten` (`waitA`) | **Stage A Market Order Execution Window**: Gives exchange matching engine time to fill mutated market order before escalating to Stage B cancel-and-sweep. |
| **900 milliseconds** | `trading/position_exit.go:420` | `executeEmergencyFlatten` (`waitB`) | **Stage B/C Sweep Execution Window**: Allows 900ms for exchange matching engine to fill escalating LPP sweep sells before escalating to the next liquidity band. |
| **900 milliseconds** | `trading/position_exit.go:473` | `executeEmergencyFlatten` (`waitRec`)| **Stage D Recovery Execution Window**: Allows 900ms for recovery sweep order to fill during persistent recovery probing. |
| **2.0 seconds** | `trading/position_exit.go:445` | `executeEmergencyFlatten` (`recoveryTicker`) | **Anti-Orphan Recovery Cadence**: Probes order book liquidity repeatedly during lower circuit freezes without violating Zerodha 10 req/s rate limits. |
| **10 seconds** | `trading/execution_manager.go:399` | `orderSyncTicker` | Background broker reconciliation loop that detects external manual broker interventions or dropped WebSocket postbacks. |
| **30 seconds** | `trading/execution_manager.go:401` | `marginRefreshTicker` | Refreshes live Zerodha RMS equity margins and cash collateral balances. |
| **15 minutes** | `trading/entry_gatekeeper.go:82` | `symbolCooldowns` | Post-exit psychological & algorithmic churn lockout; prevents immediate re-entry into a freshly exited symbol. |
| **500 ms / 100 items** | `monitoring/collector.go:123` | `batchFlushLoop` | SQLite batch insertion interval; eliminates disk I/O lock contention on the trading hot path. |
| **30 seconds** | `monitoring/scheduler.go:69` | `watchLoop` | Scheduler check interval; monitors wall clock for 15:45 IST EOD trigger. |
| **15:45 IST** | `monitoring/scheduler.go:94` | `GenerateAndSaveDailyReport` | Daily automated EOD trigger time; runs post-market when all trades and auction settlements are finalized. |
| **15:14 IST** | `trading/session_clock.go:63` | `AUTO_SQUAREOFF_SWEEP` | Mandatory intraday square-off window; flattens all MIS positions before Zerodha RMS penalty square-off at 15:15 IST. |
| **60 seconds** | `diagnostics/flight_recorder.go` | `CooldownDuration` | **Cascading Dump Storm Debounce**: Prevents redundant disk I/O and server saturation during sustained downstream stalls. |
| **20 seconds** | `diagnostics/flight_recorder.go` | `StartupGracePeriod` | **Warmup Suppression Window**: Mutes false-positive watchdog dumps during system boot, DB migrations, and historical bar caching. |
| **10 seconds** | `diagnostics/flight_recorder.go` | `MinEventAge` | **Flight Recorder Ring Window**: Guarantees that saved snapshots retain at least the preceding 10s of Go execution events. |

---

## 5. Concurrency Architecture & Lock Hierarchy

```
┌────────────────────────────────────────────────────────────────────────┐
│ TIER 1: Lock-Free Atomic Hot Path (<250ns, Zero Heap Allocations)      │
│ Called directly by Single-Writer Actor: worker.OnTickHotPath()         │
│ - Reads/Writes: latestLTPBits, highWaterMarkBits, confirmedStopBits,   │
│   shadowStopBits, proposedStopBits via atomic.Uint64 bitcasts          │
│ - Ratchets in-memory shadow stops via CAS loops                        │
│ - Signals worker goroutine via non-blocking wakeCh send                │
└────────────────────────────────────┬───────────────────────────────────┘
                                     │ Non-blocking signal
┌────────────────────────────────────▼───────────────────────────────────┐
│ TIER 2: Dedicated Worker Goroutine (Event Loop)                        │
│ go worker.workerLoop()                                                 │
│ - Coordinates all network operations, timers, and order mutations      │
│ - Listens on:                                                          │
│   * wakeCh (order modification requests)                               │
│   * emergReasonCh (emergency exit triggers)                            │
│   * fillNotifyCh (order fill notifications from reconciler)            │
│   * entryTimeout (5-minute limit order fill timeout)                   │
│   * watchdogTicker (500ms gapped-through SL watchdog)                  │
└────────────────────────────────────┬───────────────────────────────────┘
                                     │ State locks
┌────────────────────────────────────▼───────────────────────────────────┐
│ TIER 3: Mutex Protected State Machine (worker.mu sync.RWMutex)         │
│ - Protects: state, order IDs (entryOrderID, slOrderID), PnL totals,    │
│   closedAt, exitReason                                                 │
│ - Acquired during order transitions, DTO serialization, and DB sync    │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 6. Comprehensive Function & File Cross-Reference Table

| Package & File | Primary Function / Symbol | Invoked By | Architectural Responsibility |
| :--- | :--- | :--- | :--- |
| `cmd/api/main.go` | `main()` | OS Runtime | Boots SQLite, KiteService, Actor, TickerClient, ExecutionManager, Scheduler, HTTP Router. |
| `cmd/api/router.go` | `buildRouter()` | `main.go` | Registers all REST endpoints, WebSocket upgrader, and monitoring report routes. |
| `internal/market/tick_ingestion.go` | `processTick()` | `Actor.runLoop` | Ingests L2 depth tick, updates OHLCV, RollingPeak, Aggressors, dispatches hot path. |
| `internal/market/snapshot_publisher.go` | `computeAndBroadcastSnapshot()` | `Actor.runLoop` (1s ticker) | Computes breadth, 3-state regime, sector ranks, sorts bullish stocks, invokes execution hook. |
| `internal/market/ranking_algorithm.go` | `CalculateBullishScore()` | `computeAndBroadcastSnapshot` | 10-factor quant scoring engine (Gaussian VWAP, Orderbook, Aggressors, Penalties). |
| `internal/trading/execution_manager.go` | `OnSecondTick()` | `Actor.computeAndBroadcastSnapshot` | Evaluates session clock, checks portfolio heat, calls candidate picking, handles 15:14 square-off. |
| `internal/trading/session_clock.go` | `EvaluateGlobalSessionClocks()` | `Manager.OnSecondTick` | Computes DST-aware global clocks and selects 1 of 6 NSE intraday phases. |
| `internal/trading/entry_gatekeeper.go` | `PickBestLongCandidate()` | `Manager.OnSecondTick` | Executes Gates 0–5, evaluates VWAP Pullback Reclaim, Bear Trap Spring, & 15m ORB Retest, emits shadow signals. |
| `internal/trading/position_sizer.go` | `ComputePositionSize()` | `PickBestLongCandidate` | Computes 4 constraints (Risk, Margin, Concentration, Liquidity) and enforces 1.5x tax net profit gate. |
| `internal/trading/stoploss.go` | `ComputeStopAndBufferSpec()` | `ComputePositionSize` | Calculates non-inverting stop bounds [DeltaMin, DeltaMax], BaseBuffer, and checks No-Chase rule. |
| `internal/trading/tax_calculator.go` | `CalculateRoundTripCharges()` | `position_sizer.go`, `trade_history.go` | Computes exact Indian Cash MIS statutory taxes (STT, Exch, SEBI, Stamp, GST, Brokerage). |
| `internal/trading/position_worker.go` | `Start()` | `Manager.spawnCandidateWorker` | Places limit buy order via broker, sets 5m timeout, and starts `go workerLoop()`. |
| `internal/trading/position_worker.go` | `handleEntryTimeout()` | `workerLoop` (at 5m) | Cancels unfilled entry, waits 500ms drain window, promotes partial fills $\ge$ DustFloorQty. |
| `internal/trading/trailing_stop.go` | `OnTickHotPath()` | `Actor.processTick` | Lock-free HWM update, SL breach watchdog, Gear 4 shadow stop, ProposeStopRatchet. |
| `internal/trading/position_exit.go` | `executeThreeStepScaleOut()` | `workerLoop` | Executes race-free 3-step scale-out at +1.50R with compensating rollback and 800ms timer. |
| `internal/trading/position_exit.go` | `executeEmergencyFlatten()` | `workerLoop`, `TriggerEmergencyExit` | 4-stage exit protocol: Mutate to Market $\to$ Escalating LPP sweeps $\to$ 2s anti-orphan recovery. |
| `internal/trading/order_reconciler.go` | `ProcessOrderEvent()` | Broker / WebSocket / Postback | Deduplicates fills, updates `lastFilledByOrd`, computes positive incremental fill deltas. |
| `internal/trading/risk_manager.go` | `ComputeCapitalSummary()` | `Manager.OnSecondTick` | Computes NetEquity, FreeMargin, BuyingPower5x, DrawdownMult, and checks 3 circuit breakers. |
| `internal/monitoring/collector.go` | `EmitSignalEvent()`, `EmitTradeEvent()` | Execution Engine | Non-blocking ring buffer event ingestion and 500ms batch persistence. |
| `internal/monitoring/shadow_tracker.go` | `EvaluateShadowSignals()` | `metrics.go` | Evaluates hypothetical outcomes of rejected setups to detect filter inversion. |
| `internal/monitoring/metrics.go` | `ComputeEODMetrics()` | `EODScheduler`, `router.go` | Computes MFE vs MAE, slippage friction audits, probability calibration tables, and regime breakdowns. |
| `internal/monitoring/scheduler.go` | `watchLoop()` | `main.go` | Monitors wall clock for 15:45 IST on weekdays, triggers automated report generation. |
| `internal/monitoring/email_resend.go` | `SendEODReportEmail()` | `EODScheduler` | Dispatches HTML diagnostic report via Resend API to `REPORT_EMAIL`. |

---

## 7. Concrete Opportunities for Improvement

1. **Dynamic Kite Ticker Token Subscriptions**:
   - *Current*: Subscribes to all scanned universe tokens on startup.
   - *Improvement*: Implement dynamic subscription pooling—subscribe to the Top 30 scanned universe leaders and active position tokens to minimize WebSocket network bandwidth and garbage collection overhead.
2. **Externalize Strategy Tunables to SQLite**:
   - *Current*: Parameters like `StopMinTickMultiplier (4.0)`, `StopBaseBufferLTPRatio (0.0075)`, `StopMaxChaseMultiplier (1.25)` are defined in code.
   - *Improvement*: Expose a `strategy_tunables` configuration table in SQLite, allowing risk parameters and ATR multipliers to be tuned live from the frontend without recompilation.
3. **Anchored VWAP (AVWAP) from Signal Origin**:
   - *Current*: Uses session-wide VWAP (`stock.ExchangeVWAP`).
   - *Improvement*: Compute an Anchored VWAP starting from the 15-minute Opening Range breakout bar or the morning low bar to provide cleaner dynamic support/resistance during afternoon trends.
4. **Intraday Short Selling Extension**:
   - *Current*: Executes Long setups (`AT26LONG`).
   - *Improvement*: Extend `PickBestCandidate` to support Short setups during `BEARISH_EXPANSION` regimes (SL placed above signal candle high, covering borrow and statutory charges).
5. **Execution Latency Histograms**:
   - *Improvement*: Add sub-millisecond execution telemetry histograms (order submission latency, broker OMS ACK roundtrip, and slippage vs Ask1 price) to track broker fill quality across market regimes.
