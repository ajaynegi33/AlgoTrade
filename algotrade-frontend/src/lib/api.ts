import {
  ApiError,
  ApiResponse,
  AuthResponseData,
  NetworkError,
  ProfileResponseData,
  RefreshResponseData,
} from "./types";

const API_BASE_URL =
  process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

// In-memory token storage (isolated from XSS / localStorage)
let inMemoryAccessToken: string | null = null;

export const setAccessToken = (token: string | null) => {
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

export async function getUserProfile(): Promise<ProfileResponseData> {
  return fetchWithAuth<ProfileResponseData>("/api/user/profile");
}
