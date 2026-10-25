"use client";

import { useEffect } from "react";
import { RankedSnapshotData } from "@/lib/types";
import { useTradingStore } from "@/store/useTradingStore";

interface UseEngineWebSocketOptions {
  url?: string;
  enabled?: boolean;
}

interface UseEngineWebSocketReturn {
  snapshot: RankedSnapshotData | null;
  isConnected: boolean;
  lastUpdated: Date | null;
  reconnectCount: number;
}

// Module-level Singleton WebSocket Manager (Bug F1 Fix):
// Ensures strictly ONE WebSocket connection across the entire browser tab and zero teardown/skeleton flash on route transitions.
let sharedSocket: WebSocket | null = null;
let subscriberCount = 0;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let closeGraceTimer: ReturnType<typeof setTimeout> | null = null;
let backoffDelay = 1000;
let isTabPaused = false;
let hiddenDebounceTimer: ReturnType<typeof setTimeout> | null = null;
let visibilityListenerAttached = false;

function ensureVisibilityListener() {
  if (visibilityListenerAttached || typeof document === "undefined") return;
  visibilityListenerAttached = true;

  document.addEventListener("visibilitychange", () => {
    if (document.hidden) {
      hiddenDebounceTimer = setTimeout(() => {
        if (document.hidden) {
          isTabPaused = true;
        }
      }, 60000);
    } else {
      isTabPaused = false;
      if (hiddenDebounceTimer) {
        clearTimeout(hiddenDebounceTimer);
        hiddenDebounceTimer = null;
      }
    }
  });
}

function connectSharedSocket(customUrl?: string) {
  if (typeof window === "undefined") return;
  if (
    sharedSocket &&
    (sharedSocket.readyState === WebSocket.OPEN ||
      sharedSocket.readyState === WebSocket.CONNECTING)
  ) {
    return;
  }

  let wsUrl = customUrl;
  if (!wsUrl) {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const host = process.env.NEXT_PUBLIC_WS_HOST || "localhost:8080";
    wsUrl = `${protocol}//${host}/ws/live`;
  }

  try {
    const ws = new WebSocket(wsUrl);
    sharedSocket = ws;

    ws.onopen = () => {
      backoffDelay = 1000;
      useTradingStore.getState().setWsConnectionState(true, 0);
    };

    ws.onmessage = (event) => {
      if (isTabPaused) return;
      try {
        const data: RankedSnapshotData = JSON.parse(event.data);
        useTradingStore.getState().setLiveSnapshot(data);
      } catch {
        // Ignore malformed frames
      }
    };

    ws.onclose = () => {
      sharedSocket = null;
      useTradingStore.getState().setWsConnectionState(false, 1);
      if (subscriberCount > 0) {
        if (reconnectTimer) clearTimeout(reconnectTimer);
        reconnectTimer = setTimeout(() => {
          connectSharedSocket(customUrl);
        }, backoffDelay);
        backoffDelay = Math.min(backoffDelay * 1.5, 10000);
      }
    };

    ws.onerror = () => {
      ws.close();
    };
  } catch {
    if (subscriberCount > 0) {
      if (reconnectTimer) clearTimeout(reconnectTimer);
      reconnectTimer = setTimeout(() => {
        connectSharedSocket(customUrl);
      }, backoffDelay);
    }
  }
}

export function useEngineWebSocket(
  options: UseEngineWebSocketOptions = {}
): UseEngineWebSocketReturn {
  const { enabled = true, url } = options;

  const snapshot = useTradingStore((s) => s.liveSnapshot);
  const isConnected = useTradingStore((s) => s.isWsConnected);
  const lastWsUpdatedMs = useTradingStore((s) => s.lastWsUpdatedMs);
  const reconnectCount = useTradingStore((s) => s.wsReconnectCount);

  useEffect(() => {
    if (!enabled) return;

    ensureVisibilityListener();
    if (closeGraceTimer) {
      clearTimeout(closeGraceTimer);
      closeGraceTimer = null;
    }
    subscriberCount += 1;
    connectSharedSocket(url);

    return () => {
      subscriberCount = Math.max(0, subscriberCount - 1);
      if (subscriberCount === 0) {
        // 1500ms grace period prevents socket teardown during route navigation
        closeGraceTimer = setTimeout(() => {
          if (subscriberCount === 0 && sharedSocket) {
            if (reconnectTimer) {
              clearTimeout(reconnectTimer);
              reconnectTimer = null;
            }
            sharedSocket.close();
            sharedSocket = null;
          }
        }, 1500);
      }
    };
  }, [enabled, url]);

  return {
    snapshot,
    isConnected,
    lastUpdated: lastWsUpdatedMs > 0 ? new Date(lastWsUpdatedMs) : null,
    reconnectCount,
  };
}
