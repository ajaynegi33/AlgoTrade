export interface ActivePosition {
  symbol: string;
  quantity: number;
  entryPrice: number;
  currentPrice: number;
  initialStopLoss: number;
  activeTrailingStop: number;
  highWaterMark: number;
  unrealizedPnL: number;
  unrealizedPnLPct: number;
  entryTime: string;
}

export interface OverviewData {
  mode: "PAPER" | "LIVE";
  strategyId: string;
  strategyName: string;
  capital: number;
  riskBudget: number;
  canTradeToday: boolean;
  isMarketOpen?: boolean;
  nextMarketOpen?: string;
  marketPhase?: string;
  status: "IDLE" | "OBSERVING" | "IN_TRADE" | "DAY_COMPLETED" | "MARKET_CLOSED";
  activePosition: ActivePosition | null;
}

export interface ScoredCandidate {
  rank: number;
  symbol: string;
  ltp: number;
  gapPct: number;
  rvolScore: number;
  vwapScore: number;
  depthImbalanceScore: number;
  pdhScore: number;
  totalScore: number;
  eligible?: boolean;
  status: string;
}

export interface TradeHistoryItem {
  id: number;
  symbol: string;
  quantity: number;
  buyPrice: number;
  sellPrice: number;
  entryTime: string;
  exitTime: string;
  exitReason: string;
  grossPnL: number;
  netPnL: number;
  totalCharges: number;
  brokerage: number;
  stt: number;
  exchangeCharges: number;
  sebiCharges: number;
  stampDuty: number;
  gst: number;
}

export interface AnalyticsData {
  todayGrossPnL: number;
  todayNetPnL: number;
  todayTotalTaxes: number;
  todayTradesCount: number;
  todayTrades: TradeHistoryItem[];
  cumulativeNetPnL: number;
  totalGrossWins: number;
  totalGrossLosses: number;
  cumulativeTaxesPaid: number;
  winRatePct: number;
  profitFactor: number;
  allTradesHistory: TradeHistoryItem[];
}

export interface MarketDepthLevel {
  price: number;
  quantity: number;
  orders: number;
}

export interface StockQuoteData {
  symbol: string;
  instrument: string;
  authenticated?: boolean;
  liveFeed?: boolean;
  lastPrice: number;
  change?: number;
  changePct?: number;
  volume: number;
  averagePrice: number;
  lastTradedQuantity?: number;
  lastTradedTime?: string;
  timestamp?: string;
  ohlc?: {
    open: number;
    high: number;
    low: number;
    close: number;
  };
  lowerCircuitLimit?: number;
  upperCircuitLimit?: number;
  pdh?: number;
  pdl?: number;
  totalBuyQuantity?: number;
  totalSellQuantity?: number;
  buyDepthRatio?: number;
  depth?: {
    buy: MarketDepthLevel[];
    sell: MarketDepthLevel[];
  };
  error?: string;
  message?: string;
}

export interface StockChartPoint {
  time: string;
  price: number;
  open?: number;
  high?: number;
  low?: number;
  close?: number;
  volume?: number;
  vwap?: number;
  rawDate?: string;
}

export type ChartTimeframe = "1D" | "1W" | "1M" | "3M" | "6M" | "1Y" | "3Y" | "5Y" | "ALL";
export type ChartInterval = "1m" | "3m" | "5m" | "15m";
export type ChartType = "area" | "candle";

export interface ZerodhaSessionData {
  active: boolean;
  userName?: string;
  userId?: string;
  email?: string;
  availableCash?: number;
  autoLoginConfigured?: boolean;
}

export interface EngineActivityEvent {
  id: number;
  timestamp: string;
  category: "ORDERS" | "STRATEGY" | "RISK" | "LIFECYCLE" | "SYSTEM" | string;
  level: "INFO" | "WARN" | "ERROR" | string;
  executionMode?: "PAPER" | "LIVE" | string;
  tradeId?: string | null;
  title: string;
  message: string;
  symbol?: string | null;
  metadata?: Record<string, any>;
}

export interface BacktestRequest {
  startDate: string;
  endDate: string;
  initialCapital: number;
  riskPerTradePct: number;
  minScoreThreshold: number;
  trailingSlPct: number;
  slippagePct: number;
  leverageMultiplier?: number;
}

export interface BacktestTrade {
  id: number;
  date: string;
  symbol: string;
  score: number;
  entryPrice: number;
  exitPrice: number;
  quantity: number;
  entryTime: string;
  exitTime: string;
  exitReason: "INITIAL_SL" | "TRAILING_SL" | "MARKET_CLOSE_1505" | "MARKET_CLOSE_1515" | string;
  grossPnL: number;
  totalCharges: number;
  netPnL: number;
  returnPct: number;
}

export interface BacktestEquityPoint {
  date: string;
  capital: number;
  grossPnL: number;
  netPnL: number;
  drawdownPct: number;
}

export interface BacktestSummary {
  initialCapital: number;
  finalCapital: number;
  totalGrossPnL: number;
  totalTaxes: number;
  totalNetPnL: number;
  totalReturnPct: number;
  totalTrades: number;
  winTrades: number;
  lossTrades: number;
  winRatePct: number;
  profitFactor: number;
  maxDrawdownPct: number;
  maxDrawdownRupees: number;
  maxConsecutiveLosses: number;
  averageTradeNetPnL: number;
  sharpeRatio: number;
}

export interface BacktestResult {
  summary: BacktestSummary;
  equityCurve: BacktestEquityPoint[];
  trades: BacktestTrade[];
}

export interface BacktestPreset {
  id: string;
  label: string;
  startDate: string;
  endDate: string;
  description: string;
}


