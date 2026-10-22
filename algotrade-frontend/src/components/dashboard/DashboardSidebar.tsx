"use client";

import React, { useEffect } from "react";
import Image from "next/image";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { User } from "@/lib/types";
import { useTradingStore } from "@/store/useTradingStore";
import UserAvatar from "./UserAvatar";

export interface DashboardSidebarProps {
  isCollapsed: boolean;
  onToggleCollapse: () => void;
  isMobileOpen: boolean;
  onCloseMobile: () => void;
  user: User | null;
  onLogout: () => void;
}

export default function DashboardSidebar({
  isCollapsed,
  onToggleCollapse,
  isMobileOpen,
  onCloseMobile,
  user,
  onLogout,
}: DashboardSidebarProps) {
  const pathname = usePathname();

  // Close mobile drawer on Escape key press
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape" && isMobileOpen) {
        onCloseMobile();
      }
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [isMobileOpen, onCloseMobile]);

  const { kiteProfile } = useTradingStore();

  const displayName = kiteProfile?.user_name || user?.name || "Ajju";
  const displayEmail = kiteProfile?.email || user?.email;

  const isDashboardActive = pathname === "/dashboard";
  const isUniverseActive = pathname.startsWith("/universe");
  const isProfileActive = pathname.startsWith("/profile");

  return (
    <>
      {/* =========================================================================
          1. Mobile Backdrop Overlay
          ========================================================================= */}
      {isMobileOpen && (
        <div
          role="button"
          tabIndex={0}
          aria-label="Close mobile sidebar"
          onClick={onCloseMobile}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") onCloseMobile();
          }}
          className="fixed inset-0 bg-slate-900/35 backdrop-blur-xs z-40 md:hidden transition-opacity duration-200"
        />
      )}

      {/* =========================================================================
          2. Sidenav Container (Responsive: Mobile Drawer + Desktop Collapsible)
          ========================================================================= */}
      <aside
        className={`fixed md:sticky top-0 left-0 z-50 h-screen bg-white border-r border-slate-200/90 flex flex-col justify-between shrink-0 select-none transition-[width,transform] duration-250 ease-[cubic-bezier(0.23,1,0.32,1)] ${
          isMobileOpen ? "translate-x-0 w-72 shadow-2xl" : "-translate-x-full md:translate-x-0"
        } ${isCollapsed ? "md:w-20" : "md:w-64"}`}
      >
        {/* Floating Circular Toggle Button on the Border */}
        <button
          type="button"
          onClick={onToggleCollapse}
          title={isCollapsed ? "Expand sidebar" : "Collapse sidebar"}
          aria-label={isCollapsed ? "Expand sidebar" : "Collapse sidebar"}
          className="hidden md:flex absolute -right-3.5 top-7 z-30 w-7 h-7 rounded-full bg-white border border-slate-200/90 shadow-2xs hover:shadow-xs items-center justify-center text-slate-500 hover:text-slate-900 transition-all hover:scale-105 active:scale-95 cursor-pointer"
        >
          <svg
            className={`w-3.5 h-3.5 transition-transform duration-250 ease-[cubic-bezier(0.23,1,0.32,1)] ${
              isCollapsed ? "rotate-180" : ""
            }`}
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            strokeWidth="2.5"
          >
            <path strokeLinecap="round" strokeLinejoin="round" d="M15 19l-7-7 7-7" />
          </svg>
        </button>

        {/* =======================================================================
            Top Section: Brand Logo & Title
            ======================================================================= */}
        <div className="p-4 sm:p-5 flex items-center justify-between border-b border-slate-100">
          <Link href="/dashboard" className="flex items-center gap-3 overflow-hidden">
            <div className="w-10 h-10 flex items-center justify-center shadow-2xs shrink-0">
              <Image
                src="/AlgoTrade.png"
                alt="AlgoTrade Logo"
                width={32}
                height={32}
                className="w-full h-full object-contain rounded-2xl"
                priority
              />
            </div>

            <div
              className={`overflow-hidden transition-all duration-150 whitespace-nowrap ${
                isCollapsed ? "md:opacity-0 md:w-0" : "opacity-100 w-auto"
              }`}
            >
              <div className="font-black text-slate-900 tracking-tight text-base leading-none">
                AlgoTrade
              </div>
              <div className="text-[10px] font-mono text-emerald-700 font-semibold tracking-wider uppercase mt-1">
                Terminal v2.4
              </div>
            </div>
          </Link>

          {/* Mobile close button */}
          <button
            type="button"
            onClick={onCloseMobile}
            className="md:hidden p-1.5 rounded-xl text-slate-400 hover:text-slate-700 hover:bg-slate-100 cursor-pointer"
            aria-label="Close menu"
          >
            <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        {/* =======================================================================
            Middle Section: Navigation Links (Real Next.js Links)
            ======================================================================= */}
        <nav className="flex-1 px-3 py-4 space-y-1.5 overflow-y-auto">
          {/* Dashboard Overview */}
          <Link
            href="/dashboard"
            onClick={onCloseMobile}
            title="Dashboard Overview"
            className={`w-full flex items-center gap-3 px-3.5 py-3 rounded-2xl transition-all group cursor-pointer active:scale-[0.98] ${
              isDashboardActive
                ? "bg-emerald-50 text-emerald-800 font-bold border border-emerald-500/20 shadow-2xs"
                : "text-slate-600 hover:text-slate-900 hover:bg-slate-100/80 font-medium"
            } ${isCollapsed ? "md:justify-center md:px-0" : ""}`}
          >
            <svg
              className={`w-5 h-5 shrink-0 transition-colors ${
                isDashboardActive ? "text-emerald-700" : "text-slate-500 group-hover:text-slate-900"
              }`}
              viewBox="0 0 24 24"
              fill="currentColor"
            >
              <path d="M4 4h7v7H4V4zm0 9h7v7H4v-7zm9-9h7v7h-7V4zm0 9h7v7h-7v-7z" />
            </svg>

            <span
              className={`text-xs font-semibold whitespace-nowrap overflow-hidden transition-all duration-150 ${
                isCollapsed ? "md:opacity-0 md:w-0 md:hidden" : "opacity-100"
              }`}
            >
              Dashboard
            </span>
          </Link>

          {/* Stock Universe (F&O Cash Equities) */}
          <Link
            href="/universe"
            onClick={onCloseMobile}
            title="Stock Universe & Scanner"
            className={`w-full flex items-center gap-3 px-3.5 py-3 rounded-2xl transition-all group cursor-pointer active:scale-[0.98] ${
              isUniverseActive
                ? "bg-emerald-50 text-emerald-800 font-bold border border-emerald-500/20 shadow-2xs"
                : "text-slate-600 hover:text-slate-900 hover:bg-slate-100/80 font-medium"
            } ${isCollapsed ? "md:justify-center md:px-0" : ""}`}
          >
            <svg
              className={`w-5 h-5 shrink-0 transition-colors ${
                isUniverseActive ? "text-emerald-700" : "text-slate-500 group-hover:text-slate-900"
              }`}
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              strokeWidth="2"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z"
              />
            </svg>

            <span
              className={`text-xs font-semibold whitespace-nowrap overflow-hidden transition-all duration-150 ${
                isCollapsed ? "md:opacity-0 md:w-0 md:hidden" : "opacity-100"
              }`}
            >
              Stock Universe
            </span>
          </Link>
        </nav>

        {/* =======================================================================
            Bottom Section: Profile Link Card & Logout
            ======================================================================= */}
        <div className="p-3 border-t border-slate-100 space-y-2">
          {/* User Profile Card (Clickable Link to /profile) */}
          <Link
            href="/profile"
            onClick={onCloseMobile}
            title={`View Zerodha Profile: ${displayName} (${displayEmail})`}
            className={`flex items-center gap-3 p-2 rounded-2xl transition-all cursor-pointer group ${
              isProfileActive
                ? "bg-emerald-50 border border-emerald-500/30 text-emerald-950 shadow-2xs"
                : "bg-slate-50/80 border border-slate-200/70 hover:bg-slate-100/80 hover:border-slate-300"
            } ${isCollapsed ? "md:justify-center md:p-1.5" : ""}`}
          >
            <UserAvatar
              avatarUrl={kiteProfile?.avatar_url}
              name={displayName}
              size="sm"
              showOnlineDot
            />

            <div
              className={`overflow-hidden transition-all duration-150 ${
                isCollapsed ? "md:opacity-0 md:w-0 md:hidden" : "opacity-100"
              }`}
            >
              <div className="font-bold text-xs text-slate-900 group-hover:text-emerald-700 transition-colors truncate leading-snug">
                {displayName}
              </div>
            </div>
          </Link>

          {/* Sign Out Button */}
          <button
            type="button"
            onClick={onLogout}
            title="Sign Out of Session"
            className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-2xl text-slate-600 hover:text-rose-700 hover:bg-rose-50 border border-transparent hover:border-rose-200/80 transition-all group cursor-pointer active:scale-[0.98] ${
              isCollapsed ? "md:justify-center md:px-0" : ""
            }`}
          >
            <svg
              className="w-5 h-5 text-slate-400 group-hover:text-rose-600 shrink-0 transition-colors"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              strokeWidth="2"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1"
              />
            </svg>

            <span
              className={`text-xs font-semibold whitespace-nowrap overflow-hidden transition-all duration-150 ${
                isCollapsed ? "md:opacity-0 md:w-0 md:hidden" : "opacity-100"
              }`}
            >
              Sign Out
            </span>
          </button>
        </div>
      </aside>
    </>
  );
}
