export interface User {
  id: string;
  email: string;
  name: string;
  role: string;
}

export interface AuthResponseData {
  access_token: string;
  token_type: string;
  expires_in: number;
  user: User;
}

export interface RefreshResponseData {
  access_token: string;
  token_type: string;
  expires_in: number;
}

export interface KiteStatusData {
  is_configured: boolean;
  is_connected: boolean;
  user_id?: string;
  user_name?: string;
  login_time?: string;
}

export interface EquityMarginsData {
  net: number;
  available_cash: number;
  collateral: number;
  opening_balance: number;
  live_balance: number;
  utilised_debits: number;
}

export interface KiteUserProfileData {
  is_connected: boolean;
  user_id?: string;
  user_name?: string;
  user_shortname?: string;
  email?: string;
  broker?: string;
  user_type?: string;
  avatar_url?: string;
  exchanges?: string[];
  products?: string[];
  order_types?: string[];
  login_time?: string;
  equity_margin?: EquityMarginsData;
}

export interface ScannedStock {
  instrument_token: number;
  tradingsymbol: string;
  name: string;
  exchange: string;
  segment: string;
  instrument_type: string;
  lot_size: number;
  tick_size: number;
  last_price: number;
  scan_date: string;
  updated_at: string;
}

export interface UniverseSummaryData {
  scan_date: string;
  total_stocks: number;
  last_updated: string;
  stocks: ScannedStock[];
}

export interface ApiResponse<T> {
  success: boolean;
  data?: T;
  error?: {
    code: string;
    message: string;
  };
  message?: string;
}

export class ApiError extends Error {
  code: string;
  statusCode: number;

  constructor(message: string, code = "UNKNOWN_ERROR", statusCode = 500) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.statusCode = statusCode;
  }
}

export class NetworkError extends ApiError {
  constructor(message = "Network connection failed. Please check your internet or server.") {
    super(message, "NETWORK_ERROR", 0);
    this.name = "NetworkError";
  }
}

// =========================================================================
// Real-Time Quant Engine DTO Types
// =========================================================================

export interface MarketPulseData {
  market_state: string;
  nifty_ltp: number;
  nifty_change_percent: number;
  banknifty_ltp: number;
  banknifty_change_percent: number;
  breadth_above_vwap: number;
  market_regime: string;
}

export interface SectorRankData {
  sector_id: number;
  sector_name: string;
  change_percent: number;
  breadth_above_vwap: number;
  is_leader: boolean;
  is_lagging: boolean;
}

export interface StockRankData {
  rank: number;
  token: number;
  symbol: string;
  sector: string;
  sector_id: number;
  ltp: number;
  change_percent: number;
  vwap: number;
  bullish_score: number;
  bearish_score: number;
  rvol_session: number;
  rvol_pace_5m: number;
  composite_rs: number;
  buyer_ratio: number;
  aggressor_buy_ratio: number;
  ev: number;
  p_win: number;
  stop_proxy: number;
  target_proxy: number;
  reasons: number;
  pullback_count: number;
}

export interface RankedSnapshotData {
  timestamp: number;
  historical_active: boolean;
  historical_error?: string;
  market_pulse: MarketPulseData;
  sectors: SectorRankData[];
  top_bullish: StockRankData[];
  top_bearish: StockRankData[];
  all_stocks: StockRankData[];
  execution?: ExecutionStateData;
}

// =========================================================================
// Phase 2 Autonomous Execution Cockpit DTO Types
// =========================================================================

export interface MarketSessionItemData {
  id: string;
  name: string;
  timezone: string;
  local_time: string;
  status: "OPEN" | "PRE_OPEN" | "LUNCH" | "CLOSED" | string;
  status_label: string;
  is_open: boolean;
}

export interface GlobalSessionClockData {
  ist_time: string;
  phase: string;
  phase_label: string;
  playbook_note: string;
  tape_override_active: boolean;
  allow_new_entries: boolean;
  min_bullish_score: number;
  min_rvol: number;
  size_multiplier: number;
  clocks: MarketSessionItemData[];
}

export interface CapitalSummaryData {
  mode: "PAPER" | "LIVE" | string;
  paper_capital: number;
  net_equity: number;
  free_margin: number;
  blocked_margin: number;
  buying_power_5x?: number;
  deployed_exposure?: number;
  free_buying_power_5x?: number;
  realised_gross_pnl: number;
  total_charges: number;
  realised_net_pnl: number;
  unrealised_pnl: number;
  net_day_pnl: number;
  risk_per_trade_pct: number;
  current_risk_budget: number;
  max_open_positions: number;
  active_positions_count: number;
}

export interface DailyRiskData {
  trading_date: string;
  trades_taken: number;
  wins: number;
  losses: number;
  win_rate_pct: number;
  consecutive_losses: number;
  peak_net_pnl: number;
  circuit_breaker_locked: boolean;
  lock_reason?: string;
}

export interface ActivePositionData {
  position_id: string;
  mode: string;
  symbol: string;
  instrument_token: number;
  state: string;
  pattern_type: string;
  entry_price: number;
  exit_price?: number;
  ltp: number;
  initial_stop: number;
  current_stop: number;
  shadow_stop: number;
  breakeven_stop: number;
  base_buffer: number;
  high_water_mark: number;
  one_r: number;
  r_multiple: number;
  mfe_r_multiple: number;
  initial_qty: number;
  remaining_qty: number;
  mod_count: number;
  max_mods: number;
  current_gear: number;
  gear_label: string;
  tranche1_done: boolean;
  unrealised_pnl: number;
  realised_gross_pnl: number;
  estimated_charges: number;
  net_total_pnl: number;
  exit_reason?: string;
  opened_at: string;
  opened_at_unix?: number;
  closed_at?: string;
}

export interface CandidateGateDiagnosticData {
  rank: number;
  symbol: string;
  sector: string;
  ltp: number;
  bullish_score: number;
  composite_rvol: number;
  ev: number;
  regime_ok: boolean;
  score_ok: boolean;
  rvol_ok: boolean;
  book_quality_ok: boolean;
  pattern_matched: string;
  sizing_approved: boolean;
  overall_status: "READY" | "ACTIVE_OR_COOLDOWN" | "BLOCKED" | string;
  blocking_reason: string;
}

export interface ExecutionStateData {
  mode: "PAPER" | "LIVE" | string;
  auto_execute: boolean;
  macro_shock_active: boolean;
  gate_status: string;
  gate_reason: string;
  capital: CapitalSummaryData;
  daily_risk: DailyRiskData;
  session_clock: GlobalSessionClockData;
  active_positions: ActivePositionData[];
  closed_positions: ActivePositionData[];
  top_candidate_gates?: CandidateGateDiagnosticData[];
}

export interface TaxBreakdownData {
  buy_brokerage: number;
  sell_brokerage: number;
  brokerage: number;
  stt: number;
  exchange_txn: number;
  sebi_charges: number;
  stamp_duty: number;
  gst: number;
  total_charges: number;
}

export interface TradeOrderLegData {
  order_id: string;
  position_id: string;
  leg_type: "ENTRY" | "SCALE_OUT" | "SL" | "EMERGENCY_EXIT" | string;
  transaction_type: "BUY" | "SELL" | string;
  order_type: string;
  quantity: number;
  filled_quantity: number;
  price: number;
  trigger_price: number;
  average_price: number;
  status: string;
  mod_count: number;
  updated_at: string;
}

export interface TradeHistoryItemData {
  position_id: string;
  trading_date: string;
  mode: "PAPER" | "LIVE" | string;
  symbol: string;
  company_name: string;
  sector: string;
  instrument_token: number;
  state: string;
  pattern_type: string;
  purchase_time: string;
  sell_time: string;
  hold_duration_sec: number;
  hold_duration_label: string;
  quantity: number;
  buy_price: number;
  sell_price: number;
  total_invested: number;
  margin_blocked: number;
  total_sell_value: number;
  price_change_per_share: number;
  gross_pnl: number;
  taxes: TaxBreakdownData;
  net_pnl_after_tax: number;
  return_on_invested_pct: number;
  return_on_margin_pct: number;
  initial_stop: number;
  final_stop: number;
  breakeven_stop: number;
  high_water_mark: number;
  one_r: number;
  realised_r_multiple: number;
  peak_mfe_r: number;
  r_capture_efficiency_pct: number;
  mod_count: number;
  final_gear: number;
  tranche1_done: boolean;
  exit_reason: string;
  orders?: TradeOrderLegData[];
}

export interface PatternPerformanceData {
  pattern_type: string;
  trades: number;
  wins: number;
  win_rate_pct: number;
  avg_r_multiple: number;
  gross_pnl: number;
  total_taxes: number;
  net_pnl_after_tax: number;
}

export interface ExitReasonStatData {
  exit_reason: string;
  count: number;
  avg_r_multiple: number;
  gross_pnl: number;
  total_taxes: number;
  net_pnl_after_tax: number;
}

export interface TradeAnalyticsSummaryData {
  total_trades: number;
  wins: number;
  losses: number;
  breakeven_trades: number;
  win_rate_pct: number;
  total_invested_turnover: number;
  total_sell_turnover: number;
  total_margin_deployed: number;
  gross_profit_rupees: number;
  gross_loss_rupees: number;
  realised_gross_pnl: number;
  taxes: TaxBreakdownData;
  tax_drag_pct_of_gross_win: number;
  realised_net_pnl: number;
  profit_factor: number;
  expectancy_per_trade: number;
  avg_r_multiple: number;
  avg_win_r: number;
  avg_loss_r: number;
  avg_win_rupees: number;
  avg_loss_rupees: number;
  max_win_rupees: number;
  max_loss_rupees: number;
  avg_hold_duration_sec: number;
  tranche1_scale_out_rate: number;
  avg_r_capture_efficiency: number;
  by_pattern: PatternPerformanceData[];
  by_exit_reason: ExitReasonStatData[];
}

export interface TradeHistoryResponseData {
  summary: TradeAnalyticsSummaryData;
  trades: TradeHistoryItemData[];
}

export interface ReasonBadge {
  label: string;
  type: "catalyst" | "penalty";
  description: string;
}

export function decodeReasonFlags(reasons: number): ReasonBadge[] {
  const badges: ReasonBadge[] = [];

  // Catalysts
  if (reasons & (1 << 0)) badges.push({ label: "VWAP Sweet Spot", type: "catalyst", description: "In VWAP +0.25 ATR sweet spot with CLV >= 0.55" });
  if (reasons & (1 << 1)) badges.push({ label: "RVOL Surge", type: "catalyst", description: "Dual RVOL >= 1.8x institutional volume pace" });
  if (reasons & (1 << 2)) badges.push({ label: "Sector Leader", type: "catalyst", description: "Stock belongs to Top 2 Leading Sectors" });
  if (reasons & (1 << 3)) badges.push({ label: "Buyer Wall", type: "catalyst", description: "Order book buyer depth and aggressor flow >= 58%" });
  if (reasons & (1 << 4)) badges.push({ label: "ORB Breakout", type: "catalyst", description: "Holding cleanly above 15m Opening Range High" });
  if (reasons & (1 << 5)) badges.push({ label: "High EV (+0.4R+)", type: "catalyst", description: "Statistical pre-trade expected value >= +0.40R" });
  if (reasons & (1 << 6)) badges.push({ label: "VWAP Breakdown", type: "catalyst", description: "Bearish push-off below VWAP -0.25 ATR" });
  if (reasons & (1 << 7)) badges.push({ label: "Sector Laggard", type: "catalyst", description: "Stock belongs to Lagging Sector with sell flow" });

  // Penalties
  if (reasons & (1 << 8)) badges.push({ label: "Wide Spread", type: "penalty", description: "Bid-ask spread > 0.10% friction guard" });
  if (reasons & (1 << 9)) badges.push({ label: "Overextended", type: "penalty", description: "LTP > VWAP + 1.5 ATR chasing risk" });
  if (reasons & (1 << 10)) badges.push({ label: "Below Prev Close", type: "penalty", description: "Gap-down dead-cat bounce (LTP < PrevClose)" });
  if (reasons & (1 << 11)) badges.push({ label: "Low RVOL", type: "penalty", description: "Composite RVOL < 1.0x retail churn" });
  if (reasons & (1 << 12)) badges.push({ label: "Upper Wick / Red", type: "penalty", description: "In zone, but red 5m candle or shooting star wick" });
  if (reasons & (1 << 13)) badges.push({ label: "Trend Exhausted", type: "penalty", description: "4th+ pullback to VWAP; risk of institutional distribution" });
  if (reasons & (1 << 14)) badges.push({ label: "Iceberg Selling", type: "penalty", description: "Bids visible, but aggressive sellers dumping" });

  return badges;
}

export interface DepthLevelData {
  price: number;
  qty: number;
  orders: number;
}

export interface SessionBarData {
  timestamp: number;
  time: string;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
}

export interface StockDetailData {
  symbol: string;
  token: number;
  sector: string;
  sector_id: number;
  lot_size: number;
  tick_size: number;
  is_tradeable: boolean;
  ltp: number;
  open: number;
  high: number;
  low: number;
  prev_close: number;
  change: number;
  change_percent: number;
  vwap: number;
  vwap_distance: number;
  vwap_distance_pct: number;
  orh: number;
  orl: number;
  has_or15m: boolean;
  clv: number;
  atr14: number;
  adx14: number;
  volume: number;
  rvol_session: number;
  rvol_pace_5m: number;
  bullish_score: number;
  bearish_score: number;
  composite_rs: number;
  buyer_ratio: number;
  aggressor_buy_ratio: number;
  ev: number;
  p_win: number;
  stop_proxy: number;
  target_proxy: number;
  reasons: string[];
  bids: DepthLevelData[];
  asks: DepthLevelData[];
  total_bid_qty: number;
  total_ask_qty: number;
  anti_spoof_ratio: number;
  bars: SessionBarData[];
  last_updated: number;
}

export interface CandleData {
  timestamp: number;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
}

export interface OrderBlockLevelData {
  symbol: string;
  instrument_token: number;
  direction: "BULLISH" | "BEARISH";
  zone_level_z: number;
  zone_low: number;
  zone_high: number;
  invalidation_s1: number;
  invalidation_r1?: number;
  target_r1: number;
  target_r2: number;
  target_s1?: number;
  target_s2?: number;
  one_r: number;
  risk_reward_ratio: number;
  breakeven_win_rate_pct: number;
  zone_age: "IMPULSE" | "MARKED" | "FIRST_RETEST" | "LATER_RETEST" | "BROKEN" | string;
  retest_count: number;
  is_chasing: boolean;
  chasing_risk_per_share: number;
  chasing_rr_ratio: number;
  chasing_be_win_rate_pct: number;
  candles_5m: CandleData[];
}

export interface MinimaxEvaluationData {
  symbol: string;
  instrument_token: number;
  direction: "BULLISH" | "BEARISH";
  minimax_ev: number;
  conviction_pct: number;
  action_verdict: "ACT_AT_ZONE_Z" | "WAIT_FOR_PULLBACK_TO_Z" | "PRUNED_CHOP" | string;
  verdict_reason: string;
  predicted_path: string[];
  pruned_branches: number;
  evaluated_nodes: number;
  order_block: OrderBlockLevelData;
}

export interface SectorMoveData {
  name: string;
  avg_move_pct: number;
}

export interface MarketPictureData {
  bullish_count: number;
  bearish_count: number;
  total_stocks: number;
  leading_sectors: SectorMoveData[];
  lagging_sectors: SectorMoveData[];
  day_bias: "ONE_SIDED_BULLISH" | "ONE_SIDED_BEARISH" | "BALANCED" | string;
}

export interface TrajectoryData {
  symbol: string;
  direction: "BULLISH" | "BEARISH";
  change_pct: number;
  points: number[];
  is_pick: boolean;
  sector: string;
}

export interface DecisionGateItemData {
  symbol: string;
  sector: string;
  change_pct: number;
  score: number;
  buildup_type: "LONG BUILD-UP" | "SHORT BUILD-UP";
  passed_gate1: boolean;
  passed_gate2: boolean;
  passed_gate3: boolean;
  passed_gate4: boolean;
  is_pick: boolean;
}

export interface OrderBlockTreeResponseData {
  market_picture: MarketPictureData;
  trajectories: TrajectoryData[];
  bullish_gates: DecisionGateItemData[];
  bearish_gates: DecisionGateItemData[];
  top_bullish_picks: MinimaxEvaluationData[];
  top_bearish_picks: MinimaxEvaluationData[];
}

