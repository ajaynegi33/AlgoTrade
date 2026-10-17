"use client";

import React, { useState, useEffect, useRef, useCallback } from "react";
import { apiFetch } from "@/lib/apiClient";

export interface StockSearchResult {
  symbol: string;
  companyName: string;
  isFno: boolean;
  instrumentToken?: number;
  exchange?: string;
}

interface StockSearchComboboxProps {
  currentSymbol: string;
  onSelectStock: (symbol: string) => void;
  loading?: boolean;
}

/**
 * Isolated, high-performance Stock Search Combobox with typeahead dropdown.
 *
 * Guarantees:
 * 1. Zero parent re-renders while typing.
 * 2. 150ms debounced queries with AbortController cancellation (no race conditions).
 * 3. Full keyboard navigation (ArrowUp, ArrowDown, Enter, Escape).
 * 4. Fuzzy search across Ticker Symbol, Legal Company Name, and Market Aliases.
 */
export const StockSearchCombobox = React.memo(function StockSearchCombobox({
  currentSymbol,
  onSelectStock,
  loading = false,
}: StockSearchComboboxProps) {
  const [query, setQuery] = useState(currentSymbol);
  const [results, setResults] = useState<StockSearchResult[]>([]);
  const [isOpen, setIsOpen] = useState(false);
  const [isSearching, setIsSearching] = useState(false);
  const [highlightedIndex, setHighlightedIndex] = useState(-1);

  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const debounceTimerRef = useRef<NodeJS.Timeout | null>(null);
  const abortControllerRef = useRef<AbortController | null>(null);

  // Sync internal input value when parent symbol changes (e.g. clicking popular stock chip)
  useEffect(() => {
    if (currentSymbol) {
      setQuery(currentSymbol);
    }
  }, [currentSymbol]);

  // Clean up debounce and pending fetch on unmount
  useEffect(() => {
    return () => {
      if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current);
      if (abortControllerRef.current) abortControllerRef.current.abort();
    };
  }, []);

  // Dismiss dropdown on outside clicks
  useEffect(() => {
    const handleOutsideClick = (event: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };
    document.addEventListener("mousedown", handleOutsideClick);
    return () => document.removeEventListener("mousedown", handleOutsideClick);
  }, []);

  // Debounced search query dispatcher
  const performSearch = useCallback((searchTerm: string) => {
    const trimmed = searchTerm.trim();
    if (!trimmed) {
      setResults([]);
      setIsOpen(false);
      setIsSearching(false);
      return;
    }

    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
    }

    const controller = new AbortController();
    abortControllerRef.current = controller;
    setIsSearching(true);

    apiFetch(`/api/dashboard/search-stocks?q=${encodeURIComponent(trimmed)}&limit=8`, {
      signal: controller.signal,
    })
      .then(async (res) => {
        if (res.ok) {
          const data: StockSearchResult[] = await res.json();
          if (Array.isArray(data)) {
            setResults(data);
            setIsOpen(data.length > 0);
            setHighlightedIndex(data.length > 0 ? 0 : -1);
          }
        }
      })
      .catch((err) => {
        if (err.name !== "AbortError") {
          console.warn("Stock search query failed:", err);
        }
      })
      .finally(() => {
        setIsSearching(false);
      });
  }, []);

  // Handle typing input with 150ms debounce
  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const nextVal = e.target.value;
    setQuery(nextVal);

    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current);
    }

    debounceTimerRef.current = setTimeout(() => {
      performSearch(nextVal);
    }, 150);
  };

  const handleSelect = (symbol: string) => {
    const clean = symbol.trim().toUpperCase().replace("NSE:", "");
    if (!clean) return;

    setQuery(clean);
    setIsOpen(false);
    setHighlightedIndex(-1);
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
    }
    onSelectStock(clean);
  };

  const handleFormSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (highlightedIndex >= 0 && results[highlightedIndex]) {
      handleSelect(results[highlightedIndex].symbol);
    } else if (query.trim()) {
      handleSelect(query.trim());
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (!isOpen || results.length === 0) {
      if (e.key === "ArrowDown" && query.trim()) {
        e.preventDefault();
        performSearch(query);
      }
      return;
    }

    if (e.key === "ArrowDown") {
      e.preventDefault();
      setHighlightedIndex((prev) => (prev < results.length - 1 ? prev + 1 : 0));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setHighlightedIndex((prev) => (prev > 0 ? prev - 1 : results.length - 1));
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (highlightedIndex >= 0 && results[highlightedIndex]) {
        handleSelect(results[highlightedIndex].symbol);
      } else if (query.trim()) {
        handleSelect(query.trim());
      }
    } else if (e.key === "Escape") {
      e.preventDefault();
      setIsOpen(false);
    }
  };

  const handleClear = () => {
    setQuery("");
    setResults([]);
    setIsOpen(false);
    inputRef.current?.focus();
  };

  return (
    <div ref={containerRef} className="relative flex-1">
      <form onSubmit={handleFormSubmit} className="flex gap-2">
        <div className="relative flex-1">
          {/* Prefix Badge */}
          <span className="absolute left-3.5 top-1/2 -translate-y-1/2 text-slate-500 text-xs font-mono select-none">
            NSE:
          </span>

          {/* Search Input */}
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={handleInputChange}
            onKeyDown={handleKeyDown}
            onFocus={() => {
              if (query.trim().length > 0 && results.length > 0) {
                setIsOpen(true);
              }
            }}
            placeholder="Search by symbol or name (e.g. State Bank, RELIANCE, Tata Motors, INFY, SBI)..."
            className="w-full bg-slate-950 border border-slate-700/80 focus:border-emerald-500 focus:ring-1 focus:ring-emerald-500/30 rounded-xl pl-14 pr-20 py-3 text-sm font-mono text-white placeholder-slate-500 outline-none transition shadow-inner"
            autoComplete="off"
            spellCheck={false}
          />

          {/* Clear & Loading Indicators inside right of input */}
          <div className="absolute right-3 top-1/2 -translate-y-1/2 flex items-center gap-1.5">
            {isSearching && (
              <span className="inline-block w-4 h-4 border-2 border-emerald-400 border-t-transparent rounded-full animate-spin" />
            )}
            {query.length > 0 && (
              <button
                type="button"
                onClick={handleClear}
                className="w-5 h-5 rounded-full flex items-center justify-center text-slate-400 hover:text-white hover:bg-slate-800 text-xs transition cursor-pointer"
                title="Clear search"
              >
                ✕
              </button>
            )}
          </div>
        </div>

        {/* Submit Search Button */}
        <button
          type="submit"
          disabled={loading || !query.trim()}
          className="px-6 py-3 rounded-xl bg-emerald-500 hover:bg-emerald-400 disabled:opacity-50 text-slate-950 font-bold text-sm tracking-wide transition flex items-center gap-2 cursor-pointer disabled:cursor-not-allowed shadow-md shadow-emerald-950/40 shrink-0"
        >
          {loading ? "Loading..." : "Search"}
        </button>
      </form>

      {/* Autocomplete Dropdown Menu */}
      {isOpen && results.length > 0 && (
        <div className="absolute left-0 right-0 top-full mt-2 bg-slate-900/95 border border-slate-700/90 rounded-xl shadow-2xl backdrop-blur-xl z-50 overflow-hidden divide-y divide-slate-800/80 animate-in fade-in zoom-in-95 duration-100">
          <div className="px-3.5 py-1.5 bg-slate-950/60 flex items-center justify-between text-[11px] text-slate-400 font-medium">
            <span>NSE Equities &amp; Underlyings</span>
            <span className="text-[10px] text-slate-500 font-mono">
              ↑↓ Navigate • ↵ Select • Esc Close
            </span>
          </div>

          <ul className="max-h-72 overflow-y-auto py-1 scrollbar-thin scrollbar-thumb-slate-700">
            {results.map((item, idx) => {
              const isSelected = idx === highlightedIndex;
              return (
                <li
                  key={`${item.symbol}_${idx}`}
                  onMouseEnter={() => setHighlightedIndex(idx)}
                  onClick={() => handleSelect(item.symbol)}
                  className={`px-3.5 py-2.5 flex items-center justify-between gap-3 cursor-pointer transition ${
                    isSelected
                      ? "bg-emerald-500/15 border-l-2 border-emerald-400"
                      : "hover:bg-slate-800/60 border-l-2 border-transparent"
                  }`}
                >
                  <div className="flex items-center gap-2.5 min-w-0">
                    {/* Ticker Symbol */}
                    <span className="font-mono font-bold text-sm text-emerald-400 tracking-wider shrink-0">
                      {item.symbol}
                    </span>

                    {/* Full Company Name */}
                    <span className="text-xs text-slate-300 truncate font-normal">
                      {item.companyName}
                    </span>
                  </div>

                  <div className="flex items-center gap-1.5 shrink-0">
                    {/* F&O Badge */}
                    {item.isFno && (
                      <span className="px-1.5 py-0.5 rounded text-[10px] font-semibold font-mono bg-emerald-500/20 text-emerald-300 border border-emerald-500/40">
                        F&amp;O
                      </span>
                    )}

                    {/* Exchange Badge */}
                    <span className="px-1.5 py-0.5 rounded text-[10px] font-semibold font-mono bg-slate-800 text-slate-400 border border-slate-700">
                      {item.exchange || "NSE"}
                    </span>
                  </div>
                </li>
              );
            })}
          </ul>
        </div>
      )}
    </div>
  );
});
