import {
  ApiError,
  ApiResponse,
  AuthResponseData,
  NetworkError,
  RefreshResponseData,
  User,
  KiteStatusData,
  KiteUserProfileData,
  UniverseSummaryData,
  StockDetailData,
  ExecutionStateData,
  TradeHistoryResponseData,
} from "./types";

const API_BASE_URL =
  process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

// In-memory token storage (isolated from XSS / localStorage)
let inMemoryAccessToken: string | null = null;

const setAccessToken = (token: string | null) => {
  inMemoryAccessToken = token;
};

export const getAccessToken = () => inMemoryAccessToken;

// Concurrency-safe refresh mutex
let isRefreshing = false;
let refreshPromise: Promise<string | null> | null = null;

/**
 * Performs a silent refresh using the HttpOnly refresh_token cookie.
 * If multiple requests fail with 401 concurrently, they all await this single promise.
 */
export async function refreshAccessToken(): Promise<string | null> {
  if (isRefreshing && refreshPromise) {
    return refreshPromise;
  }

  isRefreshing = true;
  refreshPromise = (async () => {
    try {
      const response = await fetch(`${API_BASE_URL}/api/auth/refresh`, {
        method: "POST",
        credentials: "include", // Transmit HttpOnly cookie
        headers: {
          "Content-Type": "application/json",
        },
      });

      if (!response.ok) {
        setAccessToken(null);
        return null;
      }

      const json: ApiResponse<RefreshResponseData> = await response.json();
      if (json.success && json.data?.access_token) {
        setAccessToken(json.data.access_token);
        return json.data.access_token;
      }

      setAccessToken(null);
      return null;
    } catch (err) {
      // Offline backend or unauthenticated visitor is a normal initial state
      console.debug("[Auth] Silent session refresh skipped (unauthenticated or backend offline):", err);
      setAccessToken(null);
      return null;
    } finally {
      isRefreshing = false;
      refreshPromise = null;
    }
  })();

  return refreshPromise;
}

/**
 * Robust fetch wrapper with automatic Authorization header injection,
 * silent 401 refresh retry, and exhaustive try/catch exception handling.
 */
export async function fetchWithAuth<T>(
  endpoint: string,
  options: RequestInit = {}
): Promise<T> {
  const url = endpoint.startsWith("http")
    ? endpoint
    : `${API_BASE_URL}${endpoint}`;

  const headers = new Headers(options.headers || {});
  headers.set("Content-Type", "application/json");

  const currentToken = getAccessToken();
  if (currentToken) {
    headers.set("Authorization", `Bearer ${currentToken}`);
  }

  let response: Response;
  try {
    response = await fetch(url, {
      ...options,
      headers,
      credentials: "include",
    });
  } catch (err: unknown) {
    const message =
      err instanceof Error
        ? `Network request failed: ${err.message}`
        : "Cannot reach backend server. Please check your connection.";
    throw new NetworkError(message);
  }

  // Handle Token Expiry: 401 Unauthorized
  if (response.status === 401) {
    // Attempt silent refresh using the HttpOnly cookie
    const newToken = await refreshAccessToken();
    if (newToken) {
      // Retry original request with the fresh 5-min access token
      headers.set("Authorization", `Bearer ${newToken}`);
      try {
        response = await fetch(url, {
          ...options,
          headers,
          credentials: "include",
        });
      } catch (retryErr: unknown) {
        const message =
          retryErr instanceof Error
            ? `Retry failed: ${retryErr.message}`
            : "Network error during retry.";
        throw new NetworkError(message);
      }
    } else {
      throw new ApiError(
        "Session has expired. Please log in again.",
        "SESSION_EXPIRED",
        401
      );
    }
  }

  let json: ApiResponse<T>;
  try {
    json = await response.json();
  } catch {
    throw new ApiError(
      `Failed to parse server response (${response.status} ${response.statusText})`,
      "RESPONSE_PARSE_ERROR",
      response.status
    );
  }

  if (!response.ok || !json.success) {
    const code = json.error?.code || `HTTP_${response.status}`;
    const message =
      json.error?.message ||
      json.message ||
      `Request failed with status ${response.status}`;
    throw new ApiError(message, code, response.status);
  }

  return json.data as T;
}

/**
 * Authentication API calls
 */
export async function loginUser(
  email: string,
  password: string
): Promise<AuthResponseData> {
  let response: Response;
  try {
    response = await fetch(`${API_BASE_URL}/api/auth/login`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "include",
      body: JSON.stringify({ email, password }),
    });
  } catch (err: unknown) {
    const msg =
      err instanceof Error
        ? err.message
        : "Failed to connect to login server";
    throw new NetworkError(msg);
  }

  let json: ApiResponse<AuthResponseData>;
  try {
    json = await response.json();
  } catch {
    throw new ApiError("Failed to parse login response", "PARSE_ERROR", response.status);
  }

  if (!response.ok || !json.success || !json.data) {
    throw new ApiError(
      json.error?.message || "Invalid credentials",
      json.error?.code || "LOGIN_FAILED",
      response.status
    );
  }

  setAccessToken(json.data.access_token);
  return json.data;
}

export async function logoutUser(): Promise<void> {
  try {
    await fetch(`${API_BASE_URL}/api/auth/logout`, {
      method: "POST",
      credentials: "include",
    });
  } catch (err) {
    console.warn("Logout request failed:", err);
  } finally {
    setAccessToken(null);
  }
}

export async function getUserProfile(): Promise<User> {
  return fetchWithAuth<User>("/api/user/me");
}

export async function getKiteStatus(): Promise<KiteStatusData> {
  const res = await fetch(`${API_BASE_URL}/api/kite/status`, {
    credentials: "include",
  });
  if (!res.ok) {
    throw new ApiError("Failed to fetch Kite status", "KITE_STATUS_ERROR", res.status);
  }
  const json: ApiResponse<KiteStatusData> = await res.json();
  if (!json.success || !json.data) {
    throw new ApiError(json.error?.message || "Failed to fetch Kite status", json.error?.code || "KITE_ERROR");
  }
  return json.data;
}

export async function getKiteUserProfile(): Promise<KiteUserProfileData> {
  const res = await fetch(`${API_BASE_URL}/api/kite/profile`, {
    credentials: "include",
  });
  if (!res.ok) {
    throw new ApiError("Failed to fetch Kite user profile", "KITE_PROFILE_ERROR", res.status);
  }
  const json: ApiResponse<KiteUserProfileData> = await res.json();
  if (!json.success || !json.data) {
    throw new ApiError(json.error?.message || "Failed to fetch Kite user profile", json.error?.code || "KITE_ERROR");
  }
  return json.data;
}

export async function getKiteLoginURL(): Promise<string> {
  const res = await fetch(`${API_BASE_URL}/api/kite/login-url`, {
    credentials: "include",
  });
  if (!res.ok) {
    throw new ApiError("Failed to get Kite login URL", "KITE_URL_ERROR", res.status);
  }
  const json: ApiResponse<{ login_url: string }> = await res.json();
  if (!json.success || !json.data?.login_url) {
    throw new ApiError(json.error?.message || "Kite not configured", json.error?.code || "KITE_ERROR");
  }
  return json.data.login_url;
}

export async function sendKiteCallback(requestToken: string): Promise<{ message: string; user_id: string; user_name: string; login_time: string }> {
  return fetchWithAuth<{ message: string; user_id: string; user_name: string; login_time: string }>("/api/kite/callback", {
    method: "POST",
    body: JSON.stringify({ request_token: requestToken }),
  });
}

export async function getStockUniverse(date?: string): Promise<UniverseSummaryData> {
  const endpoint = date ? `/api/scanner/universe?date=${encodeURIComponent(date)}` : "/api/scanner/universe";
  const res = await fetch(`${API_BASE_URL}${endpoint}`, {
    credentials: "include",
  });
  if (!res.ok) {
    throw new ApiError("Failed to fetch stock universe", "UNIVERSE_ERROR", res.status);
  }
  const json: ApiResponse<UniverseSummaryData> = await res.json();
  if (!json.success || !json.data) {
    throw new ApiError(json.error?.message || "Failed to fetch stock universe", json.error?.code || "UNIVERSE_ERROR");
  }
  return json.data;
}

export async function triggerUniverseScan(): Promise<UniverseSummaryData> {
  interface ScanApiResponse {
    message: string;
    scan_date: string;
    total_stocks: number;
    last_updated: string;
    universe: UniverseSummaryData["stocks"];
  }
  const res = await fetchWithAuth<ScanApiResponse>("/api/scanner/run", {
    method: "POST",
  });
  return {
    scan_date: res.scan_date,
    total_stocks: res.total_stocks,
    last_updated: res.last_updated,
    stocks: res.universe,
  };
}

export async function getStockDetail(symbol: string): Promise<StockDetailData> {
  const cleanSymbol = encodeURIComponent(symbol.trim().toUpperCase());
  const res = await fetch(`${API_BASE_URL}/api/stocks/${cleanSymbol}`, {
    credentials: "include",
  });
  if (!res.ok) {
    throw new ApiError(`Stock ${symbol} not found or engine offline`, "STOCK_DETAIL_ERROR", res.status);
  }
  const json: ApiResponse<StockDetailData> | StockDetailData = await res.json();
  const stockData: StockDetailData | undefined =
    "success" in json && json.success && json.data
      ? json.data
      : "symbol" in json && json.symbol
      ? (json as StockDetailData)
      : undefined;

  if (!stockData) {
    const errorMsg = ("error" in json && json.error?.message) || `Failed to fetch stock ${symbol}`;
    const errorCode = ("error" in json && json.error?.code) || "STOCK_DETAIL_ERROR";
    throw new ApiError(errorMsg, errorCode);
  }
  return stockData;
}

// =========================================================================
// Phase 2 Autonomous Execution API Calls
// =========================================================================

export async function getExecutionState(): Promise<ExecutionStateData> {
  const res = await fetch(`${API_BASE_URL}/api/execution/state`, {
    credentials: "include",
  });
  if (!res.ok) {
    throw new ApiError("Failed to fetch execution state", "EXECUTION_STATE_ERROR", res.status);
  }
  const json: ApiResponse<ExecutionStateData> = await res.json();
  if (!json.success || !json.data) {
    throw new ApiError(json.error?.message || "Failed to fetch execution state", json.error?.code || "EXECUTION_ERROR");
  }
  return json.data;
}

export async function updateExecutionSettings(payload: {
  mode?: "PAPER" | "LIVE";
  auto_execute?: boolean;
  paper_capital?: number;
  risk_per_trade_pct?: number;
  max_open_positions?: number;
}): Promise<ExecutionStateData> {
  return fetchWithAuth<ExecutionStateData>("/api/execution/settings", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export async function updatePaperCapital(paperCapital: number): Promise<ExecutionStateData> {
  return fetchWithAuth<ExecutionStateData>("/api/execution/paper-capital", {
    method: "POST",
    body: JSON.stringify({ paper_capital: paperCapital }),
  });
}

export async function flattenExecutionPosition(payload: {
  position_id?: string;
  all?: boolean;
}): Promise<{ flattened_count: number; state: ExecutionStateData }> {
  return fetchWithAuth<{ flattened_count: number; state: ExecutionStateData }>("/api/execution/flatten", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export async function getTradeHistory(params?: {
  mode?: string;
  from?: string;
  to?: string;
  limit?: number;
}): Promise<TradeHistoryResponseData> {
  const query = new URLSearchParams();
  if (params?.mode && params.mode !== "ALL") query.set("mode", params.mode);
  if (params?.from) query.set("from", params.from);
  if (params?.to) query.set("to", params.to);
  if (params?.limit) query.set("limit", String(params.limit));

  const qs = query.toString();
  const endpoint = `/api/execution/history${qs ? `?${qs}` : ""}`;
  const res = await fetch(`${API_BASE_URL}${endpoint}`, {
    credentials: "include",
  });
  if (!res.ok) {
    throw new ApiError("Failed to fetch trade history", "TRADE_HISTORY_ERROR", res.status);
  }
  const json: ApiResponse<TradeHistoryResponseData> = await res.json();
  if (!json.success || !json.data) {
    throw new ApiError(
      json.error?.message || "Failed to fetch trade history",
      json.error?.code || "TRADE_HISTORY_ERROR"
    );
  }
  return json.data;
}



