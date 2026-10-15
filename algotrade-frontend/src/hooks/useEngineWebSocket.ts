"use client";

import { useEffect, useRef, useState, useCallback } from "react";
import { OverviewData, ScoredCandidate, EngineActivityEvent, MarketDepthLevel } from "@/components/types";
import { TOKEN_STORAGE_KEY } from "@/lib/apiClient";

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
  open?: number;
  close?: number;
  averagePrice?: number;
  lastTradedQuantity?: number;
  lastTradedTime?: string;
  time: number;
  totalBuyQuantity?: number;
  totalSellQuantity?: number;
  buyDepthRatio?: number;
  depth?: {
    buy: MarketDepthLevel[];
    sell: MarketDepthLevel[];
  };
}

interface UseEngineWebSocketReturn {
  overview: OverviewData | null;
  setOverview: React.Dispatch<React.SetStateAction<OverviewData | null>>;
  leaderboard: ScoredCandidate[];
  setLeaderboard: React.Dispatch<React.SetStateAction<ScoredCandidate[]>>;
  recentDecisions: EngineActivityEvent[];
  connectionStatus: WebSocketConnectionStatus;
  lastUpdated: Date | null;
  reconnect: () => void;
  latestTicks: Record<string, TickUpdate>;
  subscribeSymbol: (symbol: string) => void;
  marketPhase: string;
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
// Calculate current market phase dynamically based on official NSE IST trading hours
export function getCurrentMarketPhase(): string {
  const now = new Date();
  const utc = now.getTime() + now.getTimezoneOffset() * 60000;
  const ist = new Date(utc + 3600000 * 5.5);
  const day = ist.getDay();
  // Saturday (6) or Sunday (0)
  if (day === 0 || day === 6) return "MARKET_CLOSED";

  const totalMinutes = ist.getHours() * 60 + ist.getMinutes();
  if (totalMinutes < 9 * 60) return "PRE_MARKET";
  if (totalMinutes < 9 * 60 + 15) return "PRE_OPEN";
  if (totalMinutes < 9 * 60 + 20) return "OPENING_DISCOVERY";
  if (totalMinutes < 10 * 60 + 30) return "WAVE_1_MORNING_MOMENTUM";
  if (totalMinutes < 12 * 60 + 30) return "MIDDAY_CONSOLIDATION";
  if (totalMinutes < 13 * 60 + 45) return "WAVE_2_EUROPEAN_INFLUX";
  if (totalMinutes < 14 * 60 + 45) return "WAVE_3_AFTERNOON_RALLY";
  if (totalMinutes < 15 * 60 + 5) return "SESSION_SHUTDOWN";
  if (totalMinutes < 15 * 60 + 30) return "SQUARE_OFF";
  return "MARKET_CLOSED";
}

export function useEngineWebSocket(token?: string | null): UseEngineWebSocketReturn {
  const [overview, setOverview] = useState<OverviewData | null>(null);
  const [leaderboard, setLeaderboard] = useState<ScoredCandidate[]>([]);
  const [recentDecisions, setRecentDecisions] = useState<EngineActivityEvent[]>([]);
  const [connectionStatus, setConnectionStatus] = useState<WebSocketConnectionStatus>("connecting");
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [latestTicks, setLatestTicks] = useState<Record<string, TickUpdate>>({});
  const [marketPhase, setMarketPhase] = useState<string>("");

  // Client-side only: compute and periodically refresh marketPhase to naturally advance with the clock
  useEffect(() => {
    setMarketPhase(getCurrentMarketPhase());
    const timer = setInterval(() => {
      setMarketPhase(getCurrentMarketPhase());
    }, 30000);
    return () => clearInterval(timer);
  }, []);

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

        // In-band First-Frame Authentication (RFC 6750: avoids leaking JWT in URL logs)
        const effectiveToken = token || (typeof window !== "undefined" ? localStorage.getItem(TOKEN_STORAGE_KEY) : null);
        if (effectiveToken) {
          try {
            ws.send(JSON.stringify({ type: "AUTH", token: effectiveToken }));
          } catch {}
        }

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
            if (data.overview) {
              setOverview(data.overview);
              if (data.overview.marketPhase) {
                setMarketPhase(data.overview.marketPhase);
              }
              if (data.overview.activePosition?.symbol && ws.readyState === WebSocket.OPEN) {
                try {
                  ws.send(JSON.stringify({ action: "SUBSCRIBE_SYMBOL", symbol: data.overview.activePosition.symbol }));
                  subscribedSymbolsRef.current.add(data.overview.activePosition.symbol);
                } catch {}
              }
            }
            if (typeof data.marketPhase === "string") {
              setMarketPhase(data.marketPhase);
            } else if (typeof data.phase === "string") {
              setMarketPhase(data.phase);
            }
            if (Array.isArray(data.leaderboard)) {
              setLeaderboard(data.leaderboard);
              if (ws.readyState === WebSocket.OPEN) {
                data.leaderboard.slice(0, 10).forEach((c: ScoredCandidate) => {
                  if (c && c.symbol) {
                    try {
                      ws.send(JSON.stringify({ action: "SUBSCRIBE_SYMBOL", symbol: c.symbol }));
                      subscribedSymbolsRef.current.add(c.symbol);
                    } catch {}
                  }
                });
              }
            }
            if (Array.isArray(data.recentDecisions)) {
              setRecentDecisions(data.recentDecisions.slice(0, 150));
            }
            setLastUpdated(new Date());
          } else if (type === "OVERVIEW_UPDATE" && data) {
            setOverview(data);
            if (data.marketPhase) {
              setMarketPhase(data.marketPhase);
            }
            if (data.activePosition?.symbol && ws.readyState === WebSocket.OPEN) {
              try {
                ws.send(JSON.stringify({ action: "SUBSCRIBE_SYMBOL", symbol: data.activePosition.symbol }));
                subscribedSymbolsRef.current.add(data.activePosition.symbol);
              } catch {}
            }
            setLastUpdated(new Date());
          } else if (type === "POSITION_UPDATE") {
            setOverview((prev) => {
              if (!prev) return prev;
              return {
                ...prev,
                status: data ? "IN_TRADE" : "OBSERVING",
                activePosition: data || null,
              };
            });
            setLastUpdated(new Date());
          } else if (type === "TICK_UPDATE" && data && data.symbol) {
            setLatestTicks((prev) => ({
              ...prev,
              [data.symbol]: data as TickUpdate,
            }));
            // Instantly update active position in overview state with zero lag
            setOverview((prev) => {
              if (!prev || !prev.activePosition || prev.activePosition.symbol !== data.symbol) {
                return prev;
              }
              const pos = prev.activePosition;
              const newLtp = data.ltp;
              const entry = pos.entryPrice;
              const qty = pos.quantity;
              const unPnl = (newLtp - entry) * qty;
              const unPnlPct = entry > 0 ? ((newLtp - entry) / entry) * 100.0 : 0;
              const newHwm = Math.max(pos.highWaterMark, newLtp);

              return {
                ...prev,
                activePosition: {
                  ...pos,
                  currentPrice: newLtp,
                  unrealizedPnL: Math.round(unPnl * 100) / 100,
                  unrealizedPnLPct: Math.round(unPnlPct * 100) / 100,
                  highWaterMark: Math.round(newHwm * 100) / 100,
                },
              };
            });
            // Instantly update candidate LTP in leaderboard if this symbol is present
            setLeaderboard((prev) => {
              if (!prev || prev.length === 0) return prev;
              let changed = false;
              const next = prev.map((item) => {
                if (item.symbol === data.symbol && item.ltp !== data.ltp) {
                  changed = true;
                  return { ...item, ltp: data.ltp };
                }
                return item;
              });
              return changed ? next : prev;
            });
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
            if (socketRef.current && socketRef.current.readyState === WebSocket.OPEN) {
              data.slice(0, 10).forEach((c: ScoredCandidate) => {
                if (c && c.symbol) {
                  try {
                    socketRef.current?.send(JSON.stringify({ action: "SUBSCRIBE_SYMBOL", symbol: c.symbol }));
                    subscribedSymbolsRef.current.add(c.symbol);
                  } catch {}
                }
              });
            }
            setLastUpdated(new Date());
          } else if (type === "ALL_DAY_SCAN_UPDATE" && data) {
            if (Array.isArray(data.topCandidates)) {
              setLeaderboard(data.topCandidates);
              if (socketRef.current && socketRef.current.readyState === WebSocket.OPEN) {
                data.topCandidates.slice(0, 10).forEach((c: ScoredCandidate) => {
                  if (c && c.symbol) {
                    try {
                      socketRef.current?.send(JSON.stringify({ action: "SUBSCRIBE_SYMBOL", symbol: c.symbol }));
                      subscribedSymbolsRef.current.add(c.symbol);
                    } catch {}
                  }
                });
              }
            }
            if (typeof data.phase === "string") {
              setMarketPhase(data.phase);
            }
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

        // If unauthorized or policy violation (e.g. invalid/expired JWT), halt reconnect loop
        if (event.code === 1008) {
          setConnectionStatus("error");
          if (typeof window !== "undefined") {
            window.dispatchEvent(new CustomEvent("algotrade-auth-expired"));
          }
          return;
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

  // Mount & unmount lifecycle, plus token update
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
        socketRef.current.close(1000, "Component unmounted or token changed");
        socketRef.current = null;
      }
    };
  }, [connect, token]);

  return {
    overview,
    setOverview,
    leaderboard,
    setLeaderboard,
    recentDecisions,
    connectionStatus,
    lastUpdated,
    reconnect,
    latestTicks,
    subscribeSymbol,
    marketPhase,
  };
}
