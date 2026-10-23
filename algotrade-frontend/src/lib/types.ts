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

