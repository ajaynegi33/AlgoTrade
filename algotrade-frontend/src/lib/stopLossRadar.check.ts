import assert from "node:assert";
import { computeStopLossRadar } from "./stopLossRadar.ts";
import type { ActivePositionData } from "./types.ts";

function runStopLossRadarChecks() {
  const basePos: ActivePositionData = {
    position_id: "POS_TEST_1",
    mode: "PAPER",
    symbol: "INFY",
    instrument_token: 408065,
    state: "ACTIVE",
    pattern_type: "VWAP_PULLBACK_RECLAIM",
    entry_price: 1500.0,
    ltp: 1505.0,
    initial_stop: 1480.0,
    current_stop: 1480.0,
    shadow_stop: 0,
    breakeven_stop: 1502.5,
    base_buffer: 5.0,
    high_water_mark: 1508.0,
    one_r: 20.0,
    r_multiple: 0.25,
    mfe_r_multiple: 0.40,
    initial_qty: 37,
    remaining_qty: 37,
    mod_count: 2,
    max_mods: 25,
    current_gear: 1,
    gear_label: "GEAR 1 • AGILE SUB-1R",
    tranche1_done: false,
    unrealised_pnl: 185.0,
    realised_gross_pnl: 0,
    estimated_charges: 92.5,
    net_total_pnl: 92.5,
    opened_at: "09:35:00",
  };

  // Test 1: Gear 1 Sub-1.0R
  {
    const radar = computeStopLossRadar(basePos);
    assert.strictEqual(radar.currentStop, 1480.0, "Gear 1 current stop must be initial stop");
    assert.strictEqual(radar.currentStopType, "EXCHANGE_RESTING");
    assert.strictEqual(radar.nextStopPrice, 1502.5, "Gear 1 next stop must be tax-free breakeven");
    assert.strictEqual(radar.triggerDistanceRupees, 15.0, "Target 1520 - LTP 1505 = 15.0 away");
    assert.strictEqual(radar.isTriggerReached, false);
  }

  // Test 2: Gear 2 (1.0R to 1.5R Pre-Scale)
  {
    const gear2Pos: ActivePositionData = {
      ...basePos,
      ltp: 1524.0,
      current_stop: 1502.5,
      current_gear: 2,
      gear_label: "GEAR 2 • STRUCTURE LOCK",
      r_multiple: 1.2,
      mfe_r_multiple: 1.25,
    };
    const radar = computeStopLossRadar(gear2Pos);
    assert.strictEqual(radar.currentStop, 1502.5, "Gear 2 current stop must be Breakeven Floor");
    assert.strictEqual(radar.nextStopPrice, 1530.0, "Next stop is 1.5R target: 1500 + 1.5*20 = 1530");
    assert.strictEqual(radar.triggerDistanceRupees, 6.0, "1530 - 1524 = 6.0 away");
  }

  // Test 3: Gear 3 (Runner Trailing Stop, Tranche 1 Done)
  {
    const gear3Pos: ActivePositionData = {
      ...basePos,
      ltp: 1545.0,
      current_stop: 1532.0,
      current_gear: 3,
      gear_label: "GEAR 3 • COARSE RATCHET",
      tranche1_done: true,
      remaining_qty: 18,
      r_multiple: 2.25,
      mfe_r_multiple: 2.40,
    };
    const radar = computeStopLossRadar(gear3Pos);
    assert.strictEqual(radar.currentStop, 1532.0, "Gear 3 runner stop must be current stop");
    assert.strictEqual(radar.currentStopType, "EXCHANGE_RESTING");
    assert.strictEqual(radar.isTriggerReached, true, "Runner ratchets on 5m candle close");
  }

  // Test 4: Gear 4 (Mod Count >= 24 Virtual Shadow SL)
  {
    const gear4Pos: ActivePositionData = {
      ...basePos,
      ltp: 1560.0,
      current_stop: 1540.0, // Parked on exchange at Mod 24
      shadow_stop: 1552.0,  // Advancing in RAM
      mod_count: 24,
      current_gear: 4,
      gear_label: "GEAR 4 • VIRTUAL SHADOW SL",
      tranche1_done: true,
      remaining_qty: 18,
      r_multiple: 3.0,
      mfe_r_multiple: 3.1,
    };
    const radar = computeStopLossRadar(gear4Pos);
    assert.strictEqual(radar.currentStop, 1552.0, "Gear 4 must use shadow stop in RAM");
    assert.strictEqual(radar.currentStopType, "RAM_SHADOW");
    assert.strictEqual(radar.nextStopPrice, 1552.0, "Next action is RAM market dump");
  }

  console.log("✓ All 4 Stop Loss Radar tests passed successfully!");
}

runStopLossRadarChecks();
