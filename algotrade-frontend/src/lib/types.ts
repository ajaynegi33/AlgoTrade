export interface User {
  id: string;
  email: string;
  name: string;
  role: string;
}

export interface UserProfileDetails {
  id: string;
  name: string;
  email: string;
  role: string;
  account_status: string;
  tier: string;
  kyc_verified: boolean;
  created_at: string;
}

export interface PortfolioSummary {
  account_number: string;
  currency: string;
  total_equity: number;
  cash_balance: number;
  unrealized_pnl: number;
  unrealized_pnl_percent: number;
  active_bots_count: number;
}

export interface Position {
  symbol: string;
  side: "LONG" | "SHORT";
  size: number;
  entry_price: number;
  current_price: number;
  pnl: number;
  pnl_percent: number;
}

export interface ProfileResponseData {
  profile: UserProfileDetails;
  portfolio_summary: PortfolioSummary;
  active_positions: Position[];
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
