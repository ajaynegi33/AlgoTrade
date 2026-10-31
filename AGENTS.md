# Meaningful Variable Naming & Code Clarity Rules

All Go and TypeScript code in this repository MUST strictly follow these variable naming standards on every turn:

1. **Nouns (For General Data):**
   - Never use single-letter variables (`s`, `p`, `q`, `w`, `b`, `c`, `m`, `t`, `r`, `o`).
   - Use clear, expressive nouns (e.g., `stockMetrics`, `entryPrice`, `orderQuantity`, `restingOrder`, `capitalManager`, `orderUpdateEvent`).

2. **"Is/Has/Can/Should" Prefixes (For Booleans):**
   - Every boolean variable or struct field must indicate a clear true/false condition (e.g., `isClosed`, `hasTranche1Completed`, `isAutoExecuteEnabled`, `isBreakevenReached`, `inMacroShock`).

3. **Plural Nouns (For Lists & Collections):**
   - Slices, arrays, and maps must use plural nouns or descriptive collection names (e.g., `restingOrders`, `candidateSignals`, `stockIndices`, `closedPositions`, `orderEvents`).

4. **Action Verbs (For Functions & Methods):**
   - Functions and methods must begin with an active verb describing the operation (e.g., `ComputePositionSize`, `ReserveRiskAndMargin`, `PlaceLimitBuy`, `CalculateRoundTripCharges`).

5. **No Cryptic Receiver Abbreviations:**
   - Method receivers must use clear domain nouns, not abbreviated initials:
     - Use `(capitalManager *CapitalManager)` instead of `(cm *CapitalManager)`.
     - Use `(worker *PositionWorker)` instead of `(w *PositionWorker)`.
     - Use `(broker *PaperBroker)` or `(broker *ZerodhaLiveBroker)` instead of `(p *PaperBroker)`.
     - Use `(manager *Manager)` instead of `(m *Manager)`.

6. **No Truncated Local Names or Generic Loop Counters:**
   - Replace `cp` with `orderCopy`.
   - Replace `remToFill` with `remainingToFill`.
   - Replace `availAtOrBelow` with `availableAtOrBelow`.
   - Replace `chunkAvg` with `chunkAveragePrice`.
   - Replace `p` and `q` with `levelPrice` and `levelQuantity`.
   - Replace `for _, o := range orders` with `for _, order := range orders`.
   - Replace `for _, t := range trades` with `for _, trade := range trades`.
   - Replace `for _, m := range margins` with `for _, orderMargin := range margins`.
   - Replace `for g := 1; g <= 4; g++` with `for gearNumber := 1; gearNumber <= 4; gearNumber++`.
   - Replace `y, m, d := now.Date()` with `year, month, day := now.Date()`.

