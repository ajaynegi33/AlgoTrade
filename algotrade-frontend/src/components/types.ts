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
  volume?: number;
}

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
  title: string;
  message: string;
  symbol?: string | null;
  metadata?: Record<string, any>;
}

