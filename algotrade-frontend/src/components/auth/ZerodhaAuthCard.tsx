"use client";

import React, { useState } from "react";
import { ZerodhaSessionData } from "../types";

interface ZerodhaAuthCardProps {
  session: ZerodhaSessionData | null;
  loginUrl: string | null;
  onAutoLogin: (password?: string, totpSecret?: string) => Promise<void>;
  onManualTokenSubmit: (token: string) => Promise<void>;
  activatingToken: boolean;
  autoLoggingIn: boolean;
}

export default function ZerodhaAuthCard({
  session,
  loginUrl,
  onAutoLogin,
  onManualTokenSubmit,
  activatingToken,
  autoLoggingIn,
}: ZerodhaAuthCardProps) {
  const [password, setPassword] = useState("");
  const [totpSecret, setTotpSecret] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [showConfigForm, setShowConfigForm] = useState(false);
  const [showManualFallback, setShowManualFallback] = useState(false);
  const [manualToken, setManualToken] = useState("");

  const handleFormSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    await onAutoLogin(password, totpSecret);
    setPassword("");
    setTotpSecret("");
    setShowConfigForm(false);
  };

  const handleManualSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!manualToken.trim()) return;
    await onManualTokenSubmit(manualToken.trim());
    setManualToken("");
  };

  if (session?.active) {
    return (
      <div className="bg-slate-900/60 border border-emerald-500/30 rounded-2xl p-4 sm:p-5 flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3 shadow-lg">
        <div className="flex items-center gap-3">
          <div className="w-10 h-10 rounded-xl bg-emerald-500/10 border border-emerald-500/30 flex items-center justify-center text-xl flex-shrink-0">
            🟢
          </div>
          <div>
            <div className="flex items-center gap-2 flex-wrap">
              <h3 className="font-bold text-white text-sm sm:text-base">
                Kite Connect 4.0 Active
              </h3>
              <span className="text-[10px] px-2 py-0.5 rounded bg-emerald-500/20 text-emerald-300 font-mono font-bold">
                AUTHENTICATED
              </span>
              <span className="text-[10px] px-2 py-0.5 rounded bg-amber-500/10 text-amber-300 font-mono border border-amber-500/20">
                ⚡ 08:45 AM AUTO-RENEWAL ACTIVE
              </span>
            </div>
            <p className="text-xs text-slate-400 font-mono mt-0.5">
              Trader: {session.userName || session.userId} • Available Equity Cash: ₹
              {session.availableCash ? session.availableCash.toLocaleString("en-IN") : "--"}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <button
            onClick={() => onAutoLogin()}
            disabled={autoLoggingIn}
            className="px-3 py-1.5 rounded-lg bg-slate-800 hover:bg-slate-700 text-amber-300 text-xs font-semibold border border-amber-500/30 transition flex items-center gap-1.5 cursor-pointer disabled:opacity-50"
            title="Test or force re-run automated 2FA handshake"
          >
            <span>⚡ Re-test Auto Login</span>
          </button>
          {loginUrl && (
            <a
              href={loginUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="px-3 py-1.5 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 text-xs font-medium border border-slate-700 transition cursor-pointer"
            >
              🔄 Manual Re-auth
            </a>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="bg-gradient-to-br from-slate-900 to-slate-900/90 border-2 border-amber-500/40 rounded-2xl p-6 sm:p-8 shadow-2xl space-y-6">
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 pb-6 border-b border-slate-800">
        <div className="flex items-start gap-4">
          <div className="w-12 h-12 rounded-2xl bg-amber-500/10 border border-amber-500/30 flex items-center justify-center text-2xl flex-shrink-0">
            ⚡
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h2 className="text-xl sm:text-2xl font-bold text-white tracking-tight">
                Automated Zerodha Kite Session
              </h2>
              <span className="px-2.5 py-0.5 rounded-full text-[11px] font-bold bg-amber-500/20 text-amber-300 border border-amber-500/30">
                ZERO DAILY EFFORT
              </span>
            </div>
            <p className="text-xs sm:text-sm text-slate-400 mt-1 max-w-2xl leading-relaxed">
              Never sit down every morning to copy and enter tokens again. The Java backend automatically generates 6-digit TOTP codes (RFC 6238), logs in at 08:45 AM IST, and auto-renews instantly if the session expires.
            </p>
          </div>
        </div>

        {session?.autoLoginConfigured && !showConfigForm && (
          <button
            onClick={() => onAutoLogin()}
            disabled={autoLoggingIn}
            className="inline-flex items-center gap-2 px-6 py-3.5 rounded-xl bg-gradient-to-r from-emerald-500 to-teal-500 hover:from-emerald-400 hover:to-teal-400 text-slate-950 font-black text-sm tracking-wide transition shadow-lg shadow-emerald-950 flex-shrink-0 disabled:opacity-50 cursor-pointer"
          >
            {autoLoggingIn ? (
              <>
                <div className="w-4 h-4 border-2 border-slate-950 border-t-transparent rounded-full animate-spin"></div>
                <span>Logging In Headless...</span>
              </>
            ) : (
              <span>⚡ Auto-Login Now</span>
            )}
          </button>
        )}
      </div>

      {/* Auto-Login Configuration / Action Area */}
      {!session?.autoLoginConfigured || showConfigForm ? (
        <form onSubmit={handleFormSubmit} className="bg-slate-950/80 rounded-2xl p-5 border border-slate-800 space-y-4">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-bold text-white flex items-center gap-2">
              <span>🔐 Configure Automated 2FA Login</span>
              <span className="text-[11px] font-normal text-slate-400">(Saved in backend for daily 08:45 AM cron)</span>
            </h3>
            {session?.autoLoginConfigured && (
              <button
                type="button"
                onClick={() => setShowConfigForm(false)}
                className="text-xs text-slate-400 hover:text-white cursor-pointer"
              >
                ✕ Cancel
              </button>
            )}
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label className="block text-xs font-semibold text-slate-300 uppercase tracking-wider mb-1.5">
                Zerodha Password
              </label>
              <div className="relative">
                <input
                  type={showPassword ? "text" : "password"}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="Enter your Zerodha password..."
                  className="w-full bg-slate-900 border border-slate-700 focus:border-amber-500 rounded-lg pl-3.5 pr-10 py-2.5 text-xs text-white placeholder-slate-500 outline-none transition"
                />
                <button
                  type="button"
                  onClick={() => setShowPassword(!showPassword)}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 text-slate-400 hover:text-white p-1 rounded transition focus:outline-none cursor-pointer"
                  title={showPassword ? "Hide password" : "Show password"}
                >
                  {showPassword ? (
                    <svg xmlns="http://www.w3.org/2000/svg" className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                      <path strokeLinecap="round" strokeLinejoin="round" d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.88 9.88l-3.29-3.29m7.532 7.532l3.29 3.29M3 3l18 18" />
                    </svg>
                  ) : (
                    <svg xmlns="http://www.w3.org/2000/svg" className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                      <path strokeLinecap="round" strokeLinejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                      <path strokeLinecap="round" strokeLinejoin="round" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
                    </svg>
                  )}
                </button>
              </div>
            </div>

            <div>
              <div className="flex items-center justify-between mb-1.5">
                <label className="block text-xs font-semibold text-slate-300 uppercase tracking-wider">
                  TOTP Secret Key (or Current 6-digit OTP)
                </label>
                {totpSecret.trim().match(/^\d{6}$/) && (
                  <span className="text-[10px] text-amber-400 font-mono bg-amber-500/10 px-2 py-0.5 rounded border border-amber-500/30">
                    📱 6-digit App OTP (One-Time Login)
                  </span>
                )}
                {totpSecret.trim().length > 8 && (
                  <span className="text-[10px] text-emerald-400 font-mono bg-emerald-500/10 px-2 py-0.5 rounded border border-emerald-500/30">
                    🔑 Permanent Secret Key (Auto 24/7)
                  </span>
                )}
              </div>
              <input
                type="text"
                value={totpSecret}
                onChange={(e) => setTotpSecret(e.target.value.trim().toUpperCase())}
                placeholder="Paste Base32 key (e.g. JBSWY3D...) or 6-digit code..."
                className="w-full bg-slate-900 border border-slate-700 focus:border-amber-500 rounded-lg px-3.5 py-2.5 text-xs font-mono text-white placeholder-slate-500 outline-none transition"
              />
              <p className="text-[10px] text-slate-400 mt-1">
                {totpSecret.trim().match(/^\d{6}$/)
                  ? "Will authenticate right now with this OTP. To enable automated morning logins, enter the permanent Secret Key."
                  : "Enter your permanent 2FA secret key from Kite to automate logins with 0 human effort."}
              </p>
            </div>
          </div>

          {/* Quick Guide */}
          <div className="bg-slate-900/90 rounded-xl p-3.5 border border-slate-800/80 text-xs text-slate-400 space-y-2">
            <div className="flex items-center gap-1.5 text-amber-300 font-semibold text-[11px] uppercase tracking-wider">
              <span>💡 Quick Guide: 6-Digit Code vs Secret Key</span>
            </div>
            <p className="leading-relaxed">
              • <strong className="text-white">6-digit code (e.g. 481597):</strong> Changes every 30s in Google Authenticator. Works for a 1-time immediate login.
            </p>
            <p className="leading-relaxed">
              • <strong className="text-white">TOTP Secret Key (Permanent):</strong> The ~32-character key (<code className="text-emerald-400 bg-slate-950 px-1 py-0.5 rounded">e.g. JBSWY3DPEHPK3PXP...</code>). Copy from <strong className="text-white">kite.zerodha.com &gt; Profile &gt; Password & Security &gt; Enable TOTP &gt; Copy key</strong> to automate logins forever!
            </p>
          </div>

          <button
            type="submit"
            disabled={autoLoggingIn || (!session?.autoLoginConfigured && (!password || !totpSecret))}
            className="w-full py-3 rounded-xl bg-gradient-to-r from-amber-500 to-emerald-500 hover:from-amber-400 hover:to-emerald-400 disabled:opacity-50 text-slate-950 font-black text-xs uppercase tracking-wider transition cursor-pointer"
          >
            {autoLoggingIn ? "Logging in automatically via TOTP..." : "⚡ Save Credentials & Auto-Login Now"}
          </button>
        </form>
      ) : (
        <div className="bg-slate-950/60 rounded-xl p-4 border border-slate-800 flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <span className="w-2.5 h-2.5 rounded-full bg-emerald-400 animate-pulse"></span>
              <p className="text-sm font-bold text-white">2FA Automated Login Configured</p>
            </div>
            <p className="text-xs text-slate-400">
              Scheduled cron triggers automatically every weekday at <strong className="text-amber-300">08:45:00 AM IST</strong>. Re-auth hook activates immediately on token expiry.
            </p>
          </div>
          <button
            type="button"
            onClick={() => setShowConfigForm(true)}
            className="text-xs font-semibold text-slate-400 hover:text-white underline underline-offset-4 flex-shrink-0 cursor-pointer"
          >
            ⚙️ Update Password / TOTP Secret
          </button>
        </div>
      )}

      {/* Collapsible Manual OAuth Login Fallback */}
      <div className="pt-2 border-t border-slate-800/80">
        <button
          type="button"
          onClick={() => setShowManualFallback(!showManualFallback)}
          className="text-xs text-slate-500 hover:text-slate-300 flex items-center gap-1.5 transition cursor-pointer"
        >
          <span>{showManualFallback ? "▼" : "▶"}</span>
          <span>Manual OAuth Login Flow (Fallback option)</span>
        </button>

        {showManualFallback && (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mt-4">
            <div className="bg-slate-950/60 rounded-xl p-4 border border-slate-800 text-xs text-slate-300 space-y-2">
              <p className="font-semibold text-white uppercase tracking-wider text-[11px]">
                Manual OAuth Redirect:
              </p>
              <p>If you prefer standard browser authentication:</p>
              {loginUrl && (
                <a
                  href={loginUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-2 px-3.5 py-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-white font-bold text-xs transition border border-slate-700 cursor-pointer"
                >
                  <span>🔗 Open Zerodha Login in Browser</span>
                </a>
              )}
            </div>

            <form onSubmit={handleManualSubmit} className="bg-slate-950/60 rounded-xl p-4 border border-slate-800 flex flex-col justify-between gap-3">
              <div>
                <label className="block text-xs font-semibold text-slate-300 uppercase tracking-wider mb-1">
                  Paste request_token:
                </label>
                <input
                  type="text"
                  value={manualToken}
                  onChange={(e) => setManualToken(e.target.value.trim())}
                  placeholder="32-character token from ?request_token=..."
                  className="w-full bg-slate-900 border border-slate-700 rounded-lg px-3 py-1.5 text-xs font-mono text-white placeholder-slate-500 outline-none"
                />
              </div>
              <button
                type="submit"
                disabled={activatingToken || !manualToken.trim()}
                className="w-full py-2 rounded-lg bg-sky-500 hover:bg-sky-400 disabled:bg-slate-800 text-slate-950 font-bold text-xs uppercase cursor-pointer"
              >
                {activatingToken ? "Activating..." : "Verify Token"}
              </button>
            </form>
          </div>
        )}
      </div>
    </div>
  );
}
