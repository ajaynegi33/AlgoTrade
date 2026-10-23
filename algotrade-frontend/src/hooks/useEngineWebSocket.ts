"use client";

import { useEffect, useRef, useState } from "react";
import { RankedSnapshotData } from "@/lib/types";

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

export function useEngineWebSocket(
  options: UseEngineWebSocketOptions = {}
): UseEngineWebSocketReturn {
  const { enabled = true } = options;

  const [snapshot, setSnapshot] = useState<RankedSnapshotData | null>(null);
  const [isConnected, setIsConnected] = useState(false);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [reconnectCount, setReconnectCount] = useState(0);

  const socketRef = useRef<WebSocket | null>(null);
  const isHiddenRef = useRef(false);
  const hiddenTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const isPausedRef = useRef(false);
  const reconnectTimeoutRef = useRef<NodeJS.Timeout | null>(null);

  // Background tab visibility listener with 60-second debounce
  useEffect(() => {
    const handleVisibilityChange = () => {
      if (document.hidden) {
        isHiddenRef.current = true;
        // If hidden for more than 60 seconds, pause React state updates to save battery & RAM
        hiddenTimeoutRef.current = setTimeout(() => {
          if (isHiddenRef.current) {
            isPausedRef.current = true;
          }
        }, 60000);
      } else {
        isHiddenRef.current = false;
        isPausedRef.current = false;
        if (hiddenTimeoutRef.current) {
          clearTimeout(hiddenTimeoutRef.current);
          hiddenTimeoutRef.current = null;
        }
      }
    };

    document.addEventListener("visibilitychange", handleVisibilityChange);
    return () => {
      document.removeEventListener("visibilitychange", handleVisibilityChange);
      if (hiddenTimeoutRef.current) clearTimeout(hiddenTimeoutRef.current);
    };
  }, []);

  // WebSocket Connection Management
  useEffect(() => {
    if (!enabled) return;

    let isMounted = true;
    let backoffDelay = 1000;

    const connect = () => {
      if (!isMounted) return;

      // Determine WS URL
      let wsUrl = options.url;
      if (!wsUrl) {
        const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
        const host = process.env.NEXT_PUBLIC_WS_HOST || "localhost:8080";
        wsUrl = `${protocol}//${host}/ws/live`;
      }

      try {
        const ws = new WebSocket(wsUrl);
        socketRef.current = ws;

        ws.onopen = () => {
          if (!isMounted) return;
          setIsConnected(true);
          backoffDelay = 1000;
        };

        ws.onmessage = (event) => {
          if (!isMounted || isPausedRef.current) return;
          try {
            const data: RankedSnapshotData = JSON.parse(event.data);
            setSnapshot(data);
            setLastUpdated(new Date());
          } catch {
            // Silently ignore malformed frames
          }
        };

        ws.onclose = () => {
          if (!isMounted) return;
          setIsConnected(false);
          socketRef.current = null;

          // Reconnect with exponential backoff
          setReconnectCount((prev) => prev + 1);
          reconnectTimeoutRef.current = setTimeout(() => {
            connect();
          }, backoffDelay);
          backoffDelay = Math.min(backoffDelay * 1.5, 10000);
        };

        ws.onerror = () => {
          // Let onclose handle reconnect
          ws.close();
        };
      } catch {
        if (!isMounted) return;
        reconnectTimeoutRef.current = setTimeout(() => {
          connect();
        }, backoffDelay);
      }
    };

    connect();

    return () => {
      isMounted = false;
      if (reconnectTimeoutRef.current) clearTimeout(reconnectTimeoutRef.current);
      if (socketRef.current) {
        socketRef.current.close();
        socketRef.current = null;
      }
    };
  }, [enabled, options.url]);

  return {
    snapshot,
    isConnected,
    lastUpdated,
    reconnectCount,
  };
}
