/**
 * Centralized API Client with Transparent JWT Refresh Token Rotation (RTR).
 * 
 * Features:
 * - Automatically attaches Bearer accessToken from localStorage.
 * - Enforces credentials: "include" so the HttpOnly algotrade_refresh_token cookie is passed.
 * - Transparently handles 401 Unauthorized by rotating the refresh token and retrying pending requests.
 * - Emits "algotrade-auth-expired" event when session is completely invalidated.
 */

const BACKEND_URL = process.env.NEXT_PUBLIC_BACKEND_URL || "http://localhost:8080";

export const TOKEN_STORAGE_KEY = "algotrade_access_token";

let isRefreshing = false;
let refreshSubscribers: ((token: string | null) => void)[] = [];

function onRefreshed(token: string | null) {
  refreshSubscribers.forEach((cb) => cb(token));
  refreshSubscribers = [];
}

function addRefreshSubscriber(cb: (token: string | null) => void) {
  refreshSubscribers.push(cb);
}

export async function apiFetch(path: string, options: RequestInit = {}): Promise<Response> {
  const url = path.startsWith("http") ? path : `${BACKEND_URL}${path.startsWith("/") ? path : `/${path}`}`;
  const token = typeof window !== "undefined" ? localStorage.getItem(TOKEN_STORAGE_KEY) : null;

  const headers = new Headers(options.headers || {});
  if (token && !headers.has("Authorization")) {
    headers.set("Authorization", `Bearer ${token}`);
  }
  if (!headers.has("Content-Type") && !(options.body instanceof FormData)) {
    headers.set("Content-Type", "application/json");
  }

  const response = await fetch(url, {
    ...options,
    headers,
    credentials: "include",
  });

  // If 401 Unauthorized and not an auth endpoint, attempt transparent refresh
  if (response.status === 401 && !path.includes("/api/auth/")) {
    if (!isRefreshing) {
      isRefreshing = true;
      try {
        const refreshRes = await fetch(`${BACKEND_URL}/api/auth/refresh`, {
          method: "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json" },
        });

        if (refreshRes.ok) {
          const data = await refreshRes.json();
          if (data.accessToken) {
            localStorage.setItem(TOKEN_STORAGE_KEY, data.accessToken);
            isRefreshing = false;
            onRefreshed(data.accessToken);

            // Retry original request with fresh token
            headers.set("Authorization", `Bearer ${data.accessToken}`);
            return fetch(url, {
              ...options,
              headers,
              credentials: "include",
            });
          }
        }
      } catch (err) {
        console.error("Transparent token refresh failed:", err);
      }

      isRefreshing = false;
      onRefreshed(null);
      if (typeof window !== "undefined") {
        localStorage.removeItem(TOKEN_STORAGE_KEY);
        window.dispatchEvent(new CustomEvent("algotrade-auth-expired"));
      }
    } else {
      // Another request is already refreshing; queue this retry
      return new Promise<Response>((resolve, reject) => {
        addRefreshSubscriber((newToken) => {
          if (newToken) {
            headers.set("Authorization", `Bearer ${newToken}`);
            fetch(url, { ...options, headers, credentials: "include" })
              .then(resolve)
              .catch(reject);
          } else {
            resolve(response);
          }
        });
      });
    }
  }

  return response;
}
