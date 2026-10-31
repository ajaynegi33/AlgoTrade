---
name: meaningful-variable-naming
description: Enforces clear, expressive, and institutional-grade variable naming standards across Go and TypeScript codebases. Eliminates single-letter names, vague abbreviations, and uninformative counters.
---

# Meaningful Variable Naming Standard

## Philosophy
Code is read far more often than it is written. In quantitative and institutional algorithmic trading, cryptic names (`p`, `q`, `w`, `s`, `c`, `m`, `t`) create cognitive friction, increase audit latency, and conceal logic bugs.

## Core Rules

### 1. Nouns for General Data
- Single-letter variables (`s`, `p`, `q`, `w`, `b`, `c`, `m`, `t`, `r`, `o`) are strictly prohibited in domain code.
- Always use the full domain noun:
  - `stockMetrics` instead of `s`
  - `entryPrice` or `limitPrice` instead of `p`
  - `orderQuantity` instead of `q`
  - `restingOrder` instead of `ord`
  - `capitalManager` instead of `cm`
  - `worker` instead of `w`
  - `orderUpdateEvent` instead of `ev`

### 2. "Is/Has/Can/Should" Prefixes for Booleans
- Every boolean variable, parameter, or struct field must indicate a clear true/false condition:
  - `isClosed`
  - `hasTranche1Completed`
  - `isAutoExecuteEnabled`
  - `isBreakevenReached`
  - `isSplitExit`
  - `inMacroShock`

### 3. Plural Nouns for Collections
- Slices, arrays, and maps must be named as plural nouns or explicit collections:
  - `restingOrders` instead of `list`
  - `candidateSignals` instead of `candidates`
  - `stockIndices` instead of `indices`
  - `orderEvents` instead of `out`

### 4. Action Verbs for Functions
- Functions and methods must begin with an active verb describing the operation:
  - `ComputePositionSize`
  - `ReserveRiskAndMargin`
  - `PlaceLimitBuy`
  - `CalculateRoundTripCharges`

### 5. Descriptive Method Receivers
- Method receivers must use clear domain nouns, not abbreviated initials:
  - `func (capitalManager *CapitalManager)` instead of `func (cm *CapitalManager)`
  - `func (worker *PositionWorker)` instead of `func (w *PositionWorker)`
  - `func (broker *PaperBroker)` instead of `func (p *PaperBroker)`
  - `func (broker *ZerodhaLiveBroker)` instead of `func (b *ZerodhaLiveBroker)`
  - `func (manager *Manager)` instead of `func (m *Manager)`

### 6. Meaningful Range Loops
- Never use single-letter items in range loops:
  - `for _, order := range orders` instead of `for _, o := range orders`
  - `for _, trade := range trades` instead of `for _, t := range trades`
  - `for _, orderMargin := range margins` instead of `for _, m := range margins`
  - `for gearNumber := 1; gearNumber <= 4; gearNumber++` instead of `for g := 1; g <= 4; g++`

