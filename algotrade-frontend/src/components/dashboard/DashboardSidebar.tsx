"use client";

import React, { useEffect, useCallback } from "react";
import Image from "next/image";
import { User } from "@/lib/types";

export interface DashboardSidebarProps {
  isCollapsed: boolean;
  onToggleCollapse: () => void;
  isMobileOpen: boolean;
  onCloseMobile: () => void;
  activeSection?: string;
  onSelectSection?: (section: string) => void;
  user: User | null;
  onLogout: () => void;
}

export default function DashboardSidebar({
  isCollapsed,
  onToggleCollapse,
  isMobileOpen,
  onCloseMobile,
  activeSection = "dashboard",
  onSelectSection,
  user,
  onLogout,
}: DashboardSidebarProps) {
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

  const handleNavClick = useCallback(
    (section: string) => {
      if (onSelectSection) {
        onSelectSection(section);
      }
      onCloseMobile();
    },
    [onSelectSection, onCloseMobile]
  );

  const userInitial = user?.name ? user.name.charAt(0).toUpperCase() : "U";
  const userRole = user?.role || "PRO_TRADER";
  const userName = user?.name || "Alex Vance";
  const userEmail = user?.email || "alex.vance@algotrade.com";

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
          // Mobile state: off-canvas drawer
          isMobileOpen ? "translate-x-0 w-72 shadow-2xl" : "-translate-x-full md:translate-x-0"
        } ${
          // Desktop state: responsive width
          isCollapsed ? "md:w-20" : "md:w-64"
        }`}
      >
        {/* =======================================================================
            Floating Circular Toggle Button on the Border (Reference Image Match)
            ======================================================================= */}
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
          <div className="flex items-center gap-3 overflow-hidden">
            <div className="w-10 h-10 rounded-2xl bg-white border border-slate-200/80 p-1 flex items-center justify-center shadow-2xs shrink-0">
              <Image
                src="/AlgoTrade.png"
                alt="AlgoTrade Logo"
                width={32}
                height={32}
                className="w-full h-full object-contain"
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
          </div>

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
            Middle Section: Navigation Links
            ======================================================================= */}
        <nav className="flex-1 px-3 py-4 space-y-1.5 overflow-y-auto">
          {/* Dashboard Item (Active Pill - Exact match to reference style) */}
          <button
            type="button"
            onClick={() => handleNavClick("dashboard")}
            title="Dashboard Overview"
            className={`w-full flex items-center gap-3 px-3.5 py-3 rounded-2xl transition-all group cursor-pointer active:scale-[0.98] ${
              activeSection === "dashboard"
                ? "bg-emerald-50 text-emerald-800 font-bold border border-emerald-500/20 shadow-2xs"
                : "text-slate-600 hover:text-slate-900 hover:bg-slate-100/80 font-medium"
            } ${isCollapsed ? "md:justify-center md:px-0" : ""}`}
          >
            {/* 4-square Dashboard Grid Icon */}
            <svg
              className={`w-5 h-5 shrink-0 transition-colors ${
                activeSection === "dashboard" ? "text-emerald-700" : "text-slate-500 group-hover:text-slate-900"
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
          </button>

          {/* Quick jump anchor: Live Positions */}
          <button
            type="button"
            onClick={() => handleNavClick("positions")}
            title="Active Positions"
            className={`w-full flex items-center gap-3 px-3.5 py-3 rounded-2xl transition-all group cursor-pointer active:scale-[0.98] ${
              activeSection === "positions"
                ? "bg-emerald-50 text-emerald-800 font-bold border border-emerald-500/20 shadow-2xs"
                : "text-slate-600 hover:text-slate-900 hover:bg-slate-100/80 font-medium"
            } ${isCollapsed ? "md:justify-center md:px-0" : ""}`}
          >
            {/* Table / Positions Icon */}
            <svg
              className="w-5 h-5 text-slate-500 group-hover:text-slate-900 shrink-0"
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
              Open Positions
            </span>
          </button>

          {/* Quick jump anchor: Token Security */}
          <button
            type="button"
            onClick={() => handleNavClick("security")}
            title="Token Security & RTR"
            className={`w-full flex items-center gap-3 px-3.5 py-3 rounded-2xl transition-all group cursor-pointer active:scale-[0.98] ${
              activeSection === "security"
                ? "bg-emerald-50 text-emerald-800 font-bold border border-emerald-500/20 shadow-2xs"
                : "text-slate-600 hover:text-slate-900 hover:bg-slate-100/80 font-medium"
            } ${isCollapsed ? "md:justify-center md:px-0" : ""}`}
          >
            {/* Key / Shield Icon */}
            <svg
              className="w-5 h-5 text-slate-500 group-hover:text-slate-900 shrink-0"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              strokeWidth="2"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z"
              />
            </svg>

            <span
              className={`text-xs font-semibold whitespace-nowrap overflow-hidden transition-all duration-150 ${
                isCollapsed ? "md:opacity-0 md:w-0 md:hidden" : "opacity-100"
              }`}
            >
              Security &amp; RTR
            </span>
          </button>
        </nav>

        {/* =======================================================================
            Bottom Section: Profile Card & Logout (Exact match to reference style)
            ======================================================================= */}
        <div className="p-3 border-t border-slate-100 space-y-2">
          {/* User Profile Card */}
          <div
            title={`${userName} (${userEmail})`}
            className={`flex items-center gap-3 p-2 rounded-2xl transition-all ${
              isCollapsed ? "md:justify-center md:p-1.5" : "bg-slate-50/80 border border-slate-200/70"
            }`}
          >
            <div className="relative shrink-0">
              <div className="w-9 h-9 rounded-full bg-gradient-to-tr from-emerald-600 to-teal-500 text-white font-bold text-xs flex items-center justify-center shadow-2xs">
                {userInitial}
              </div>
              {/* Online indicator dot */}
              <span className="absolute bottom-0 right-0 w-2.5 h-2.5 bg-emerald-500 rounded-full ring-2 ring-white" />
            </div>

            <div
              className={`overflow-hidden transition-all duration-150 ${
                isCollapsed ? "md:opacity-0 md:w-0 md:hidden" : "opacity-100"
              }`}
            >
              <div className="font-bold text-xs text-slate-900 truncate leading-snug">
                {userName}
              </div>
              <div className="text-[10px] font-mono font-bold text-slate-500 uppercase tracking-wider">
                {userRole}
              </div>
            </div>
          </div>

          {/* Sign Out Button (Reference Image Match) */}
          <button
            type="button"
            onClick={onLogout}
            title="Sign Out of Session"
            className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-2xl text-slate-600 hover:text-rose-700 hover:bg-rose-50 border border-transparent hover:border-rose-200/80 transition-all group cursor-pointer active:scale-[0.98] ${
              isCollapsed ? "md:justify-center md:px-0" : ""
            }`}
          >
            {/* Exit / Door Arrow Icon */}
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
