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
