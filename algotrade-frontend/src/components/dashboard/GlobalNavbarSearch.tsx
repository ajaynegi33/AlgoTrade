"use client";

import React, { useState, useEffect, useRef, useMemo, useCallback } from "react";
import { useRouter } from "next/navigation";
import { useTradingStore } from "@/store/useTradingStore";
import { useEngineWebSocket } from "@/hooks/useEngineWebSocket";

export function GlobalNavbarSearch() {
  const router = useRouter();
  const { universe } = useTradingStore();
  const { snapshot } = useEngineWebSocket();

  const [isOpen, setIsOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [selectedIndex, setSelectedIndex] = useState(0);

  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // Combine universe stocks with live snapshot prices
  const allStocks = useMemo(() => {
    const liveMap = new Map<string, { ltp: number; change_percent: number }>();
    if (snapshot?.all_stocks) {
      for (const s of snapshot.all_stocks) {
        liveMap.set(s.symbol.toUpperCase(), {
          ltp: s.ltp,
          change_percent: s.change_percent,
        });
      }
    }

    if (universe?.stocks && universe.stocks.length > 0) {
      return universe.stocks.map((st) => {
        const sym = st.tradingsymbol.toUpperCase();
        const live = liveMap.get(sym);
        return {
          symbol: sym,
          name: st.name || sym,
          ltp: live ? live.ltp : st.last_price,
          changePercent: live ? live.change_percent : 0,
        };
      });
    }

    if (snapshot?.all_stocks && snapshot.all_stocks.length > 0) {
      return snapshot.all_stocks.map((s) => ({
        symbol: s.symbol.toUpperCase(),
        name: s.sector || s.symbol,
        ltp: s.ltp,
        changePercent: s.change_percent,
      }));
    }

    return [];
  }, [universe, snapshot]);

  // Filter stocks matching query
  const filteredStocks = useMemo(() => {
    const q = query.trim().toUpperCase();
    if (!q) {
      // If empty query, show top 8 volume / high move stocks as suggestions
      return allStocks.slice(0, 8);
    }
    return allStocks
      .filter((s) => s.symbol.includes(q) || s.name.toUpperCase().includes(q))
      .slice(0, 8);
  }, [allStocks, query]);

  // Handle global ⌘K shortcut
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        setIsOpen(true);
        setTimeout(() => inputRef.current?.focus(), 50);
      } else if (e.key === "Escape") {
        setIsOpen(false);
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);

  // Handle outside click to close dropdown
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setIsOpen(false);
      }
    };

    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  const handleSelectStock = useCallback(
    (symbol: string) => {
      setIsOpen(false);
      setQuery("");
      router.push(`/stock/${symbol.toUpperCase()}`);
    },
    [router]
  );

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelectedIndex((prev) => (prev + 1) % Math.max(1, filteredStocks.length));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelectedIndex((prev) =>
        prev <= 0 ? filteredStocks.length - 1 : prev - 1
      );
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (filteredStocks.length > 0 && selectedIndex < filteredStocks.length) {
        handleSelectStock(filteredStocks[selectedIndex].symbol);
      } else if (!query.trim()) {
        setIsOpen(false);
        router.push("/search");
      }
    }
  };

  return (
    <div ref={containerRef} className="relative w-full max-w-lg lg:max-w-xl">
      {/* Search Input Bar */}
      <div className="relative flex items-center">
        <svg
          className="absolute left-3.5 top-1/2 -translate-y-1/2 w-4.5 h-4.5 text-slate-400 pointer-events-none"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          strokeWidth="2"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"
          />
        </svg>

        <input
          ref={inputRef}
          type="text"
          value={query}
          onFocus={() => setIsOpen(true)}
          onChange={(e) => {
            setQuery(e.target.value);
            setSelectedIndex(0);
            setIsOpen(true);
          }}
          onKeyDown={handleKeyDown}
          placeholder="Search stock (e.g. RELIANCE, TCS)..."
          className={`w-full h-10 sm:h-11 text-xs sm:text-[13px] font-mono text-slate-900 placeholder:text-slate-400 pl-10.5 sm:pl-11 pr-14 py-2 sm:py-2.5 transition-[background-color,border-color,box-shadow] duration-150 ${
            isOpen
              ? "bg-white rounded-t-xl rounded-b-none border border-slate-300 border-b-slate-100 shadow-xl focus:outline-none"
              : "bg-slate-100/90 hover:bg-slate-100 focus:bg-white rounded-xl border border-slate-200/90 hover:border-slate-300 focus:border-slate-300 focus:outline-none"
          }`}
        />

        {/* Action button: Clear when query exists, else ⌘K Shortcut Badge */}
        {query ? (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              setQuery("");
              setSelectedIndex(0);
              inputRef.current?.focus();
            }}
            className="absolute right-3 top-1/2 -translate-y-1/2 p-1 text-slate-400 hover:text-slate-600 rounded-md hover:bg-slate-200/60 transition-colors cursor-pointer"
            aria-label="Clear search"
          >
            <svg className="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
              <path
                fillRule="evenodd"
                d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z"
                clipRule="evenodd"
              />
            </svg>
          </button>
        ) : (
          <kbd className="absolute right-3 top-1/2 -translate-y-1/2 hidden sm:inline-flex items-center gap-0.5 text-[10px] font-mono font-medium text-slate-400 bg-white border border-slate-200/90 px-1.5 py-0.5 rounded-md shadow-2xs pointer-events-none select-none">
            <span className="text-xs">⌘</span>K
          </kbd>
        )}
      </div>

      {/* Autocomplete Dropdown Modal (Flush Attached with Zero Lag) */}
      {isOpen && (
        <div className="absolute top-full left-0 right-0 mt-0 bg-white border border-t-0 border-slate-300 rounded-b-xl shadow-2xl overflow-hidden z-50">
          <div className="max-h-84 overflow-y-auto divide-y divide-slate-100/70 p-1.5">
            {filteredStocks.length === 0 ? (
              <div className="p-4 text-center text-xs font-mono text-slate-400">
                No stocks match &quot;{query}&quot;
              </div>
            ) : (
              filteredStocks.map((stock, idx) => {
                const isSelected = idx === selectedIndex;
                const isUp = stock.changePercent >= 0;

                return (
                  <div
                    key={stock.symbol}
                    onMouseEnter={() => setSelectedIndex(idx)}
                    onClick={() => handleSelectStock(stock.symbol)}
                    className={`flex items-center justify-between px-3.5 py-2.5 rounded-lg text-xs font-mono cursor-pointer transition-colors ${
                      isSelected
                        ? "bg-slate-900 text-white shadow-xs"
                        : "hover:bg-slate-50 text-slate-800"
                    }`}
                  >
                    <div className="flex items-center gap-2.5 min-w-0">
                      <span className="font-bold tracking-tight text-[13px]">
                        {stock.symbol}
                      </span>
                      <span
                        className={`text-[11px] truncate max-w-[130px] sm:max-w-[190px] ${
                          isSelected ? "text-slate-300" : "text-slate-400"
                        }`}
                      >
                        {stock.name}
                      </span>
                    </div>

                    <div className="flex items-center gap-2.5 shrink-0">
                      <span className="font-semibold tabular-nums text-[13px]">
                        ₹{stock.ltp > 0 ? stock.ltp.toFixed(2) : "--"}
                      </span>
                      <span
                        className={`text-[11px] font-bold px-2 py-0.5 rounded-md ${
                          isSelected
                            ? isUp
                              ? "bg-emerald-500/20 text-emerald-300"
                              : "bg-rose-500/20 text-rose-300"
                            : isUp
                            ? "bg-emerald-50 text-emerald-700 border border-emerald-200/60"
                            : "bg-rose-50 text-rose-700 border border-rose-200/60"
                        }`}
                      >
                        {isUp ? "+" : ""}
                        {stock.changePercent.toFixed(2)}%
                      </span>
                    </div>
                  </div>
                );
              })
            )}
          </div>

          <div className="px-3.5 py-2.5 bg-slate-50 border-t border-slate-100 flex items-center justify-between text-[11px] font-mono text-slate-500">
            <span className="flex items-center gap-1.5">
              <kbd className="px-1.5 py-0.5 bg-white border border-slate-200 rounded text-[10px]">↑↓</kbd> Navigate
              <kbd className="ml-1 px-1.5 py-0.5 bg-white border border-slate-200 rounded text-[10px]">↵</kbd> Select
              <kbd className="ml-1 px-1.5 py-0.5 bg-white border border-slate-200 rounded text-[10px]">esc</kbd> Dismiss
            </span>
            <span className="text-slate-400 font-semibold">{allStocks.length > 0 ? allStocks.length : 213} F&O Stocks</span>
          </div>
        </div>
      )}
    </div>
  );
}
