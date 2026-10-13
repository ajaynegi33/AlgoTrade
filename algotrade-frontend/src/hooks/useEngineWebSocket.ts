"use client";

import { useEffect, useRef, useState, useCallback } from "react";
import { OverviewData, ScoredCandidate, EngineActivityEvent } from "@/components/types";

export type WebSocketConnectionStatus =
  | "connected"
  | "connecting"
  | "reconnecting"
  | "disconnected"
  | "error";

export interface TickUpdate {
  symbol: string;
  ltp: number;
  volume: number;
  change: number;
  changePct: number;
  high: number;
  low: number;
  time: number;
}

interface UseEngineWebSocketReturn {
  overview: OverviewData | null;
  setOverview: React.Dispatch<React.SetStateAction<OverviewData | null>>;
  leaderboard: ScoredCandidate[];
  recentDecisions: EngineActivityEvent[];
  connectionStatus: WebSocketConnectionStatus;
  lastUpdated: Date | null;
  reconnect: () => void;
  latestTicks: Record<string, TickUpdate>;
  subscribeSymbol: (symbol: string) => void;
}

/**
 * Institutional-grade native WebSocket hook for AlgoTrade live streaming.
 * 
 * Features:
 * - Immediate initial snapshot delivery on connect (0ms UI lag)
 * - Event-driven streaming updates (OVERVIEW_UPDATE, DECISION_EVENT, LEADERBOARD_UPDATE, TICK_UPDATE)
 * - Dynamic symbol subscription over WebSocket (SUBSCRIBE_SYMBOL)
 * - Heartbeat PING every 15s to prevent proxy/NAT timeout
 * - Exponential backoff auto-reconnect (1s to 10s)
 * - Explicit, clean socket closure on unmount (code 1000)
 */
export function useEngineWebSocket(): UseEngineWebSocketReturn {
  const [overview, setOverview] = useState<OverviewData | null>(null);
  const [leaderboard, setLeaderboard] = useState<ScoredCandidate[]>([]);
  const [recentDecisions, setRecentDecisions] = useState<EngineActivityEvent[]>([]);
  const [connectionStatus, setConnectionStatus] = useState<WebSocketConnectionStatus>("connecting");
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [latestTicks, setLatestTicks] = useState<Record<string, TickUpdate>>({});

  const socketRef = useRef<WebSocket | null>(null);
  const pingIntervalRef = useRef<NodeJS.Timeout | null>(null);
  const reconnectTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const retryCountRef = useRef<number>(0);
  const isIntentionalCloseRef = useRef<boolean>(false);
  const subscribedSymbolsRef = useRef<Set<string>>(new Set());

  // Helper to resolve ws:// or wss:// URL from backend environment variable
  const getWebSocketUrl = useCallback((): string => {
    const rawUrl = process.env.NEXT_PUBLIC_BACKEND_URL || "http://localhost:8080";
    try {
      const parsed = new URL(rawUrl);
      const protocol = parsed.protocol === "https:" ? "wss:" : "ws:";
      return `${protocol}//${parsed.host}/ws/live`;
    } catch {
      return "ws://localhost:8080/ws/live";
    }
  }, []);

  const connect = useCallback(() => {
    // Avoid double instantiation
    if (
      socketRef.current &&
      (socketRef.current.readyState === WebSocket.CONNECTING ||
        socketRef.current.readyState === WebSocket.OPEN)
    ) {
      return;
    }

    // Clear any pending reconnection timer
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }

    isIntentionalCloseRef.current = false;
    const wsUrl = getWebSocketUrl();

    try {
      setConnectionStatus(retryCountRef.current > 0 ? "reconnecting" : "connecting");
      const ws = new WebSocket(wsUrl);
      socketRef.current = ws;

      ws.onopen = () => {
        setConnectionStatus("connected");
        retryCountRef.current = 0;

        // Resubscribe any previously tracked symbols on reconnect
        if (subscribedSymbolsRef.current.size > 0) {
          subscribedSymbolsRef.current.forEach((sym) => {
            try {
              ws.send(JSON.stringify({ action: "SUBSCRIBE_SYMBOL", symbol: sym }));
            } catch {}
          });
        }

        // Start 15-second heartbeat ping
        if (pingIntervalRef.current) clearInterval(pingIntervalRef.current);
        pingIntervalRef.current = setInterval(() => {
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ type: "PING" }));
          }
        }, 15000);
      };

      ws.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data);
          const type = message.type;
          const data = message.data;

          if (type === "INITIAL_SNAPSHOT" && data) {
            if (data.overview) setOverview(data.overview);
            if (Array.isArray(data.leaderboard)) setLeaderboard(data.leaderboard);
            if (Array.isArray(data.recentDecisions)) {
              setRecentDecisions(data.recentDecisions.slice(0, 150));
            }
            setLastUpdated(new Date());
          } else if (type === "OVERVIEW_UPDATE" && data) {
            setOverview(data);
            setLastUpdated(new Date());
          } else if (type === "TICK_UPDATE" && data && data.symbol) {
            setLatestTicks((prev) => ({
              ...prev,
              [data.symbol]: data as TickUpdate,
            }));
            setLastUpdated(new Date());
          } else if (type === "DECISION_EVENT" && data) {
            setRecentDecisions((prev) => {
              // Deduplicate and prepend newest event
              const exists = prev.some((e) => e.id === data.id);
              if (exists) return prev;
              const next = [data, ...prev].slice(0, 150);
              return next;
            });
            setLastUpdated(new Date());
          } else if (type === "LEADERBOARD_UPDATE" && Array.isArray(data)) {
            setLeaderboard(data);
            setLastUpdated(new Date());
          } else if (type === "PONG") {
            // Heartbeat acknowledged
          }
        } catch (err) {
          console.warn("Failed to parse incoming WebSocket message:", err);
        }
      };

      ws.onerror = () => {
        setConnectionStatus("error");
      };

      ws.onclose = (event) => {
        if (pingIntervalRef.current) {
          clearInterval(pingIntervalRef.current);
          pingIntervalRef.current = null;
        }

        // If closed cleanly with code 1000 upon component unmount, do not attempt reconnect
        if (isIntentionalCloseRef.current || event.code === 1000) {
          setConnectionStatus("disconnected");
          return;
        }

        // Drop occurred: compute exponential backoff (1s, 1.5s, 2.25s, max 10s)
        setConnectionStatus("reconnecting");
        const backoffMs = Math.min(
          1000 * Math.pow(1.5, retryCountRef.current),
          10000
        );
        retryCountRef.current += 1;

        reconnectTimeoutRef.current = setTimeout(() => {
          connect();
        }, backoffMs);
      };
    } catch {
      setConnectionStatus("error");
    }
  }, [getWebSocketUrl]);

  // Explicit reconnect trigger
  const reconnect = useCallback(() => {
    if (socketRef.current) {
      isIntentionalCloseRef.current = true;
      socketRef.current.close(1000, "Manual Reconnect");
    }
    retryCountRef.current = 0;
    connect();
  }, [connect]);

  // Dynamic on-demand symbol subscription over WebSocket
  const subscribeSymbol = useCallback((symbol: string) => {
    if (!symbol) return;
    const clean = symbol.trim().toUpperCase().replace("NSE:", "");
    subscribedSymbolsRef.current.add(clean);
    if (socketRef.current && socketRef.current.readyState === WebSocket.OPEN) {
      try {
        socketRef.current.send(JSON.stringify({ action: "SUBSCRIBE_SYMBOL", symbol: clean }));
      } catch (err) {
        console.warn("Failed to send SUBSCRIBE_SYMBOL:", err);
      }
    }
  }, []);

  // Mount & unmount lifecycle
  useEffect(() => {
    connect();

    return () => {
      isIntentionalCloseRef.current = true;
      if (pingIntervalRef.current) {
        clearInterval(pingIntervalRef.current);
        pingIntervalRef.current = null;
      }
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
        reconnectTimeoutRef.current = null;
      }
      if (socketRef.current) {
        socketRef.current.close(1000, "Component unmounted");
        socketRef.current = null;
      }
    };
  }, [connect]);

  return {
    overview,
    setOverview,
    leaderboard,
    recentDecisions,
    connectionStatus,
    lastUpdated,
    reconnect,
    latestTicks,
    subscribeSymbol,
  };
}
