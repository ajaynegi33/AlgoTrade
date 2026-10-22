"use client";

import React, { useState, useCallback } from "react";
import Image from "next/image";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useAuth } from "../../context/AuthContext";
import { toast } from "sonner";

export default function Navbar() {
  const router = useRouter();
  const { isAuthenticated, user } = useAuth();
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);

  const handleLaunchTerminal = useCallback(() => {
    if (isAuthenticated) {
      router.push("/dashboard");
    } else {
      router.push("/login");
    }
  }, [isAuthenticated, router]);

  const handleSimulateSignalToast = useCallback(() => {
    toast.success("⚡ Live Autonomous Signal Dispatched", {
      description: "BUY 100 INFY @ ₹1,540.20 | Trailing Stop ratcheted to ₹1,515.00 (Risk Cap: 1.5%)",
      duration: 4000,
    });
  }, []);

  return (
    <header className="sticky top-0 z-50 bg-white/85 backdrop-blur-md border-b border-slate-200/80 shadow-[0_1px_3px_rgba(0,0,0,0.03)] transition-colors">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 h-16 flex items-center justify-between gap-4">
        {/* Left: Brand with Logo Image from public */}
        <Link href="/" className="flex items-center gap-2.5 shrink-0 group">
          <div className="w-9 h-9 rounded-xl bg-slate-100 border border-slate-200/90 flex items-center justify-center p-0.5 overflow-hidden shadow-2xs group-hover:scale-105 transition-transform">
            <Image
              src="/AlgoTrade.png"
              alt="AlgoTrade Logo"
              width={34}
              height={34}
              className="w-full h-full object-contain rounded-lg"
              priority
            />
          </div>
          <div className="flex items-center gap-2">
            <span className="font-extrabold text-base sm:text-lg tracking-tight text-slate-900 font-sans">
              AlgoTrade
            </span>
            <span className="hidden sm:inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-mono font-bold bg-emerald-50 text-emerald-700 border border-emerald-200/70">
              v2.4 Pro
            </span>
          </div>
        </Link>

        {/* Center: Desktop Navigation Anchors */}
        <nav className="hidden md:flex items-center gap-6 text-xs font-semibold text-slate-600">
          <a
            href="#chart-section"
            className="hover:text-slate-900 transition-colors py-1 cursor-pointer"
          >
            Strategy Alpha
          </a>
          <a
            href="#pipeline-section"
            className="hover:text-slate-900 transition-colors py-1 cursor-pointer"
          >
            Execution Pipeline
          </a>
          <a
            href="#simulator-section"
            className="hover:text-slate-900 transition-colors py-1 cursor-pointer"
          >
            Trailing Simulator
          </a>
          <a
            href="#features-section"
            className="hover:text-slate-900 transition-colors py-1 cursor-pointer"
          >
            Architecture
          </a>
        </nav>

        {/* Right: Actions */}
        <div className="flex items-center gap-2.5">
          <button
            type="button"
            onClick={handleSimulateSignalToast}
            className="hidden sm:inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl bg-slate-100 hover:bg-slate-200/70 border border-slate-200 text-slate-700 text-xs font-semibold tracking-wide transition-all cursor-pointer active:scale-95"
          >
            <span>⚡</span>
            <span>Simulate Signal</span>
          </button>

          <button
            type="button"
            onClick={handleLaunchTerminal}
            className="px-4 sm:px-5 py-2 rounded-xl bg-slate-900 hover:bg-slate-800 text-white font-semibold text-xs sm:text-sm tracking-wide shadow-xs transition-all flex items-center gap-1.5 cursor-pointer active:scale-95 shrink-0"
          >
            {isAuthenticated ? (
              <>
                <span className="w-2 h-2 rounded-full bg-emerald-400 animate-pulse" />
                <span>{user?.name?.split(" ")[0] || "Trader"} (Terminal)</span>
                <span>→</span>
              </>
            ) : (
              <>
                <span>Sign In</span>
                <span>→</span>
              </>
            )}
          </button>

          {/* Mobile Menu Toggle Button (320px support) */}
          <button
            type="button"
            onClick={() => setMobileMenuOpen((prev) => !prev)}
            aria-label="Toggle Navigation Menu"
            className="md:hidden p-2 rounded-lg text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition cursor-pointer"
          >
            <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              {mobileMenuOpen ? (
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              ) : (
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
              )}
            </svg>
          </button>
        </div>
      </div>

      {/* Mobile Drawer (320px - 640px) */}
      {mobileMenuOpen && (
        <div className="md:hidden border-t border-slate-200/90 bg-white/95 backdrop-blur-md px-4 py-3 space-y-2 text-xs font-semibold text-slate-700 animate-in slide-in-from-top-2">
          <a
            href="#chart-section"
            onClick={() => setMobileMenuOpen(false)}
            className="block py-2 px-3 rounded-lg hover:bg-slate-100 transition"
          >
            Strategy Alpha Curve
          </a>
          <a
            href="#pipeline-section"
            onClick={() => setMobileMenuOpen(false)}
            className="block py-2 px-3 rounded-lg hover:bg-slate-100 transition"
          >
            3-Stage Execution Pipeline
          </a>
          <a
            href="#simulator-section"
            onClick={() => setMobileMenuOpen(false)}
            className="block py-2 px-3 rounded-lg hover:bg-slate-100 transition"
          >
            Trailing Stop Simulator
          </a>
          <a
            href="#features-section"
            onClick={() => setMobileMenuOpen(false)}
            className="block py-2 px-3 rounded-lg hover:bg-slate-100 transition"
          >
            Institutional Capabilities
          </a>

          <div className="pt-2 border-t border-slate-100 flex items-center justify-between">
            <button
              type="button"
              onClick={() => {
                setMobileMenuOpen(false);
                handleSimulateSignalToast();
              }}
              className="w-full py-2 px-3 text-center rounded-xl bg-slate-100 text-slate-800 font-semibold text-xs"
            >
              ⚡ Fire Test Order Signal
            </button>
          </div>
        </div>
      )}
    </header>
  );
}
