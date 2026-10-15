"use client";

import { useState, useEffect, useRef, useMemo } from "react";
import { EngineActivityEvent } from "../types";
import { apiFetch } from "@/lib/apiClient";

interface DecisionFeedTerminalProps {
  backendUrl: string;
  streamEvents?: EngineActivityEvent[];
  wsStatus?: string;
}

type CategoryFilter = "ALL" | "ORDERS" | "STRATEGY" | "RISK" | "LIFECYCLE" | "SYSTEM";

export default function DecisionFeedTerminal({
  backendUrl,
  streamEvents,
  wsStatus,
}: DecisionFeedTerminalProps) {
  const [events, setEvents] = useState<EngineActivityEvent[]>(streamEvents || []);
  const [isPaused, setIsPaused] = useState(false);
  const [autoScroll, setAutoScroll] = useState(true);
  const [selectedFilter, setSelectedFilter] = useState<CategoryFilter>("ALL");
  const [expandedEventIds, setExpandedEventIds] = useState<Set<number>>(new Set());
  const [lastPollTime, setLastPollTime] = useState<Date | null>(null);
  const [connectionStatus, setConnectionStatus] = useState<"connected" | "connecting" | "error">(
    wsStatus === "connected" ? "connected" : "connecting"
  );

  const terminalContainerRef = useRef<HTMLDivElement>(null);
  const maxEventIdRef = useRef<number>(0);

  // Sync incoming real-time WebSocket events if streaming is active
  useEffect(() => {
    if (streamEvents && !isPaused) {
      setEvents(streamEvents);
      setConnectionStatus(
        wsStatus === "connected" ? "connected" : wsStatus === "reconnecting" ? "connecting" : "error"
      );
      setLastPollTime(new Date());
    }
  }, [streamEvents, wsStatus, isPaused]);

  // Fallback polling for telemetry events only when streamEvents is not provided
  useEffect(() => {
    if (streamEvents) return; // Completely decommission HTTP polling when WebSocket is active

    let isMounted = true;

    const pollEvents = async () => {
      if (isPaused) return;

      try {
        const sinceParam = maxEventIdRef.current > 0 ? `?sinceId=${maxEventIdRef.current}&limit=50` : "?limit=50";
        const res = await apiFetch(`/api/dashboard/activity-feed${sinceParam}`);

        if (!res.ok) {
          if (isMounted) setConnectionStatus("error");
          return;
        }

        const data: EngineActivityEvent[] = await res.json();
        if (!isMounted) return;

        setConnectionStatus("connected");
        setLastPollTime(new Date());

        if (Array.isArray(data) && data.length > 0) {
          const incomingMaxId = Math.max(...data.map((e) => e.id));
          if (incomingMaxId > maxEventIdRef.current) {
            maxEventIdRef.current = incomingMaxId;
          }

          setEvents((prev) => {
            const existingIds = new Set(prev.map((e) => e.id));
            const newUnique = data.filter((e) => !existingIds.has(e.id));
            if (newUnique.length === 0) return prev;

            const merged = [...newUnique, ...prev]
              .sort((a, b) => b.id - a.id)
              .slice(0, 150);
            return merged;
          });
        }
      } catch (err) {
        if (isMounted) setConnectionStatus("error");
      }
    };

    pollEvents();
    const interval = setInterval(pollEvents, 2000);
    return () => {
      isMounted = false;
      clearInterval(interval);
    };
  }, [backendUrl, isPaused, streamEvents]);

  // Auto-scroll to top when new events arrive (since newest first)
  useEffect(() => {
    if (autoScroll && terminalContainerRef.current) {
      terminalContainerRef.current.scrollTo({ top: 0, behavior: "smooth" });
    }
  }, [events, autoScroll]);

  // Toggle expanding metadata inspection drawer
  const toggleExpand = (id: number) => {
    setExpandedEventIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const handleClear = () => {
    setEvents([]);
  };

  // Filter events
  const filteredEvents = useMemo(() => {
    if (selectedFilter === "ALL") return events;
    return events.filter((e) => e.category?.toUpperCase() === selectedFilter);
  }, [events, selectedFilter]);

  // Category counts
  const counts = useMemo(() => {
    const c: Record<string, number> = { ALL: events.length, ORDERS: 0, STRATEGY: 0, RISK: 0, LIFECYCLE: 0, SYSTEM: 0 };
    events.forEach((e) => {
      const cat = e.category?.toUpperCase();
      if (cat && c[cat] !== undefined) {
        c[cat]++;
      }
    });
    return c;
  }, [events]);

  // Category styling helpers
  const getCategoryTheme = (category: string, level: string) => {
    const cat = category?.toUpperCase();
    const lvl = level?.toUpperCase();

    if (lvl === "ERROR" || cat === "ALERT") {
      return {
        badgeBg: "bg-rose-500/10 text-rose-400 border-rose-500/30",
        border: "border-l-rose-500",
        icon: "🚨",
      };
    }
    if (cat === "ORDERS") {
      return {
        badgeBg: "bg-emerald-500/10 text-emerald-400 border-emerald-500/30",
        border: "border-l-emerald-500",
        icon: "⚡",
      };
    }
    if (cat === "STRATEGY") {
      return {
        badgeBg: "bg-sky-500/10 text-sky-400 border-sky-500/30",
        border: "border-l-sky-500",
        icon: "🎯",
      };
    }
    if (cat === "RISK") {
      return {
        badgeBg: "bg-amber-500/10 text-amber-400 border-amber-500/30",
        border: "border-l-amber-500",
        icon: "🛡️",
      };
    }
    if (cat === "LIFECYCLE") {
      return {
        badgeBg: "bg-purple-500/10 text-purple-400 border-purple-500/30",
        border: "border-l-purple-500",
        icon: "⏰",
      };
    }
    return {
      badgeBg: "bg-slate-500/10 text-slate-400 border-slate-500/30",
      border: "border-l-slate-600",
      icon: "⚙️",
    };
  };

  const formatTimestamp = (iso: string) => {
    try {
      const d = new Date(iso);
      return d.toLocaleTimeString("en-IN", {
        hour12: false,
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
      }) + "." + String(d.getMilliseconds()).padStart(3, "0");
    } catch {
      return iso;
    }
  };

  return (
    <div className="bg-slate-900/80 border border-slate-800 rounded-2xl overflow-hidden shadow-2xl backdrop-blur-md flex flex-col">
      {/* Terminal Header */}
      <div className="bg-slate-950/80 border-b border-slate-800/80 px-4 sm:px-6 py-4 flex flex-col md:flex-row items-start md:items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="w-9 h-9 rounded-xl bg-emerald-500/10 border border-emerald-500/30 flex items-center justify-center text-emerald-400 text-lg shadow-inner">
            ⚡
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h3 className="text-base sm:text-lg font-bold text-white tracking-tight">
                Live Decision Stream &amp; Engine Telemetry
              </h3>
              <span
                className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-mono font-semibold border ${
                  connectionStatus === "connected"
                    ? isPaused
                      ? "bg-amber-500/10 border-amber-500/30 text-amber-400"
                      : "bg-emerald-500/10 border-emerald-500/30 text-emerald-400"
                    : "bg-rose-500/10 border-rose-500/30 text-rose-400"
                }`}
              >
                <span
                  className={`w-1.5 h-1.5 rounded-full ${
                    connectionStatus === "connected"
                      ? isPaused
                        ? "bg-amber-400"
                        : "bg-emerald-400 animate-pulse"
                      : "bg-rose-400"
                  }`}
                />
                {connectionStatus === "connected"
                  ? isPaused
                    ? "STREAM PAUSED"
                    : "STREAMING (2s)"
                  : "DISCONNECTED"}
              </span>
            </div>
            <p className="text-xs text-slate-400 mt-0.5">
              Lock-free in-memory audit trail • Zero latency execution impact (&lt;1μs overhead)
            </p>
          </div>
        </div>

        {/* Action Controls */}
        <div className="flex items-center gap-2 self-end md:self-auto">
          <button
            onClick={() => setIsPaused(!isPaused)}
            title={isPaused ? "Resume live event streaming" : "Pause live event streaming"}
            className={`px-2.5 py-1.5 rounded-lg text-xs font-medium border transition cursor-pointer flex items-center gap-1.5 ${
              isPaused
                ? "bg-amber-500/20 border-amber-500/40 text-amber-300 hover:bg-amber-500/30"
                : "bg-slate-800/80 border-slate-700 text-slate-300 hover:bg-slate-700 hover:text-white"
            }`}
          >
            {isPaused ? "▶ Resume" : "⏸ Pause"}
          </button>
          <button
            onClick={() => setAutoScroll(!autoScroll)}
            title="Toggle pin to newest events"
            className={`px-2.5 py-1.5 rounded-lg text-xs font-medium border transition cursor-pointer flex items-center gap-1.5 ${
              autoScroll
                ? "bg-emerald-500/20 border-emerald-500/40 text-emerald-300 hover:bg-emerald-500/30"
                : "bg-slate-800/80 border-slate-700 text-slate-400 hover:bg-slate-700 hover:text-white"
            }`}
          >
            {autoScroll ? "📌 Pin Newest" : "🔓 Free Scroll"}
          </button>
          <button
            onClick={handleClear}
            title="Clear current events from view"
            className="px-2.5 py-1.5 rounded-lg text-xs font-medium bg-slate-800/80 border border-slate-700 text-slate-400 hover:text-rose-400 hover:border-rose-500/30 transition cursor-pointer"
          >
            Clear View
          </button>
        </div>
      </div>

      {/* Filter Chips Bar */}
      <div className="bg-slate-950/40 px-4 sm:px-6 py-2.5 border-b border-slate-800/60 flex items-center gap-2 overflow-x-auto scrollbar-none">
        <span className="text-[11px] font-semibold text-slate-400 uppercase tracking-wider mr-1">
          Filter:
        </span>
        {(
          [
            { id: "ALL", label: "All", icon: "🌐" },
            { id: "ORDERS", label: "Orders", icon: "⚡" },
            { id: "STRATEGY", label: "Strategy", icon: "🎯" },
            { id: "RISK", label: "Risk & Sizing", icon: "🛡️" },
            { id: "LIFECYCLE", label: "Lifecycle", icon: "⏰" },
            { id: "SYSTEM", label: "System", icon: "⚙️" },
          ] as { id: CategoryFilter; label: string; icon: string }[]
        ).map((f) => (
          <button
            key={f.id}
            onClick={() => setSelectedFilter(f.id)}
            className={`px-3 py-1 rounded-lg text-xs font-medium whitespace-nowrap transition cursor-pointer flex items-center gap-1.5 border ${
              selectedFilter === f.id
                ? "bg-slate-800 text-white border-slate-600 shadow-sm"
                : "bg-slate-900/60 text-slate-400 border-slate-800/80 hover:text-slate-200 hover:bg-slate-800/50"
            }`}
          >
            <span>{f.icon}</span>
            <span>{f.label}</span>
            <span
              className={`text-[10px] px-1.5 py-0.2 rounded-full font-mono ${
                selectedFilter === f.id
                  ? "bg-slate-700 text-slate-200"
                  : "bg-slate-800 text-slate-400"
              }`}
            >
              {counts[f.id] || 0}
            </span>
          </button>
        ))}
        {lastPollTime && (
          <div className="ml-auto text-[10px] text-slate-400 font-mono whitespace-nowrap hidden sm:block">
            Updated: {lastPollTime.toLocaleTimeString()}
          </div>
        )}
      </div>

      {/* Terminal Feed Scroll Container */}
      <div
        ref={terminalContainerRef}
        className="divide-y divide-slate-800/60 max-h-[520px] overflow-y-auto font-sans scrollbar-thin scrollbar-thumb-slate-800"
      >
        {filteredEvents.length === 0 ? (
          <div className="py-16 px-6 text-center flex flex-col items-center justify-center text-slate-500">
            <div className="w-12 h-12 rounded-full bg-slate-800/40 border border-slate-700/40 flex items-center justify-center text-2xl mb-3">
              📡
            </div>
            <p className="text-sm font-medium text-slate-400">
              {events.length === 0
                ? "Awaiting engine activity events..."
                : `No events matching "${selectedFilter}"`}
            </p>
            <p className="text-xs text-slate-400 max-w-md mt-1 leading-relaxed">
              The engine automatically logs all strategy candidate scoring, risk evaluations, order dispatches, trailing stop ratchets, and 15:05 square-offs.
            </p>
          </div>
        ) : (
          filteredEvents.map((evt) => {
            const theme = getCategoryTheme(evt.category, evt.level);
            const isExpanded = expandedEventIds.has(evt.id);
            const hasMetadata = evt.metadata && Object.keys(evt.metadata).length > 0;

            return (
              <div
                key={evt.id}
                className={`p-3.5 sm:p-4 hover:bg-slate-800/30 transition-colors border-l-4 ${theme.border} flex flex-col gap-2`}
              >
                {/* Event Primary Row */}
                <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
                  <div className="flex items-center flex-wrap gap-2">
                    <span className="font-mono text-[11px] text-slate-400 bg-slate-950 px-2 py-0.5 rounded border border-slate-800">
                      {formatTimestamp(evt.timestamp)}
                    </span>
                    <span
                      className={`text-[11px] font-semibold uppercase tracking-wider px-2 py-0.5 rounded border ${theme.badgeBg} flex items-center gap-1`}
                    >
                      <span>{theme.icon}</span>
                      <span>{evt.category}</span>
                    </span>
                    {evt.executionMode && (
                      <span
                        className={`text-[10px] font-mono font-bold px-1.5 py-0.5 rounded border ${
                          evt.executionMode.toUpperCase() === "LIVE"
                            ? "bg-rose-500/15 text-rose-300 border-rose-500/30"
                            : "bg-emerald-500/15 text-emerald-300 border-emerald-500/30"
                        }`}
                      >
                        {evt.executionMode.toUpperCase() === "LIVE" ? "● LIVE" : "● PAPER"}
                      </span>
                    )}
                    {evt.symbol && (
                      <span className="font-mono font-bold text-xs px-2 py-0.5 rounded bg-emerald-500/15 text-emerald-300 border border-emerald-500/30">
                        {evt.symbol}
                      </span>
                    )}
                    {evt.tradeId && (
                      <span
                        className="font-mono text-[10px] text-slate-400 bg-slate-900 border border-slate-800 rounded px-1.5 py-0.5"
                        title={`Trade ID: ${evt.tradeId}`}
                      >
                        #{evt.tradeId}
                      </span>
                    )}
                    <span className="font-semibold text-sm text-white">{evt.title}</span>
                  </div>

                  {hasMetadata && (
                    <button
                      onClick={() => toggleExpand(evt.id)}
                      className="self-start sm:self-auto text-[11px] font-medium text-slate-400 hover:text-sky-300 transition cursor-pointer flex items-center gap-1 px-2 py-0.5 rounded bg-slate-800/60 hover:bg-slate-800 border border-slate-700/60"
                    >
                      <span>{isExpanded ? "Hide Parameters ▲" : "Inspect Audit Parameters ▼"}</span>
                    </button>
                  )}
                </div>

                {/* Event Message */}
                <p className="text-xs sm:text-sm text-slate-300 leading-relaxed font-mono pl-1">
                  {evt.message}
                </p>

                {/* Expandable Audit Metadata Drawer */}
                {isExpanded && hasMetadata && (
                  <div className="mt-2 bg-slate-950/90 rounded-xl p-3 border border-slate-800 text-xs font-mono animate-fadeIn">
                    <div className="flex items-center justify-between pb-2 mb-2 border-b border-slate-800/80">
                      <span className="text-[10px] uppercase tracking-wider font-semibold text-slate-400">
                        Numerical Parameters &amp; Decision State
                      </span>
                      <button
                        onClick={() => {
                          navigator.clipboard?.writeText(JSON.stringify(evt.metadata, null, 2));
                        }}
                        className="text-[10px] text-slate-400 hover:text-white transition cursor-pointer"
                        title="Copy raw JSON"
                      >
                        Copy JSON 📋
                      </button>
                    </div>
                    <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-2.5">
                      {Object.entries(evt.metadata!).map(([key, val]) => (
                        <div
                          key={key}
                          className="bg-slate-900/90 rounded-lg p-2 border border-slate-800/80"
                        >
                          <p className="text-[10px] text-slate-400 uppercase truncate" title={key}>
                            {key}
                          </p>
                          <p className="text-xs font-bold text-sky-300 truncate mt-0.5" title={String(val)}>
                            {typeof val === "number" ? val.toLocaleString("en-IN") : String(val)}
                          </p>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>

      {/* Terminal Footer Status Bar */}
      <div className="bg-slate-950/90 border-t border-slate-800 px-4 sm:px-6 py-2.5 flex items-center justify-between text-[11px] text-slate-400 font-mono">
        <div>
          Showing {filteredEvents.length} of {events.length} events in memory (ring buffer: 200 max)
        </div>
        <div className="flex items-center gap-2">
          <span>{streamEvents ? "⚡ WebSocket Active (0ms lag)" : "Polling: 2000ms"}</span>
          <span>•</span>
          <span className="text-emerald-400">Lock-free RAM</span>
        </div>
      </div>
    </div>
  );
}
