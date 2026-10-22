import React from "react";

interface SkeletonProps extends React.HTMLAttributes<HTMLDivElement> {
  className?: string;
}

export function Skeleton({ className = "", ...props }: SkeletonProps) {
  return (
    <div
      className={`animate-shimmer rounded-2xl bg-slate-200/60 ${className}`}
      {...props}
    />
  );
}

/**
 * Pre-composed skeleton representing the /dashboard overview page
 */
export function DashboardOverviewSkeleton() {
  return (
    <div className="space-y-6 animate-in fade-in duration-300">
      {/* Hero Banner Skeleton */}
      <div className="bg-white border border-slate-200/90 rounded-3xl p-6 sm:p-8 shadow-xs space-y-4">
        <Skeleton className="h-6 w-52 rounded-full" />
        <Skeleton className="h-9 w-80 max-w-full" />
        <Skeleton className="h-4 w-full max-w-xl" />
        <div className="flex gap-3 pt-2">
          <Skeleton className="h-10 w-36" />
          <Skeleton className="h-10 w-32" />
        </div>
      </div>

      {/* 3 Bento Cards Skeleton */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-5">
        {[1, 2, 3].map((i) => (
          <div
            key={i}
            className="bg-white border border-slate-200/90 rounded-3xl p-6 shadow-xs flex flex-col justify-between h-56 space-y-4"
          >
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <Skeleton className="h-3.5 w-24" />
                <Skeleton className="h-5 w-20 rounded-full" />
              </div>
              <Skeleton className="h-8 w-44" />
              <Skeleton className="h-3.5 w-full" />
              <Skeleton className="h-3.5 w-3/4" />
            </div>
            <div className="pt-3 border-t border-slate-100 flex items-center justify-between">
              <Skeleton className="h-4 w-28" />
              <Skeleton className="h-3 w-16" />
            </div>
          </div>
        ))}
      </div>

      {/* Action Banner Skeleton */}
      <div className="bg-white border border-slate-200/90 rounded-3xl p-6 shadow-xs flex flex-col sm:flex-row items-center justify-between gap-4">
        <div className="space-y-2 w-full sm:w-auto">
          <Skeleton className="h-5 w-60" />
          <Skeleton className="h-3.5 w-96 max-w-full" />
        </div>
        <div className="flex gap-3">
          <Skeleton className="h-9 w-28 rounded-xl" />
          <Skeleton className="h-9 w-28 rounded-xl" />
        </div>
      </div>
    </div>
  );
}

/**
 * Pre-composed skeleton for the Stock Universe table
 */
export function StockUniverseTableSkeleton() {
  return (
    <div className="divide-y divide-slate-100">
      {[1, 2, 3, 4, 5, 6, 7].map((i) => (
        <tr key={i} className="animate-pulse">
          <td className="py-4 px-4">
            <Skeleton className="h-4 w-20" />
          </td>
          <td className="py-4 px-4">
            <Skeleton className="h-4 w-40" />
          </td>
          <td className="py-4 px-4">
            <Skeleton className="h-5 w-12 rounded-md" />
          </td>
          <td className="py-4 px-4">
            <Skeleton className="h-4 w-14" />
          </td>
          <td className="py-4 px-4">
            <Skeleton className="h-4 w-10" />
          </td>
          <td className="py-4 px-4">
            <Skeleton className="h-4 w-12" />
          </td>
          <td className="py-4 px-4">
            <Skeleton className="h-5 w-12 rounded-md" />
          </td>
          <td className="py-4 px-4">
            <Skeleton className="h-5 w-24 rounded-full" />
          </td>
        </tr>
      ))}
    </div>
  );
}

/**
 * Pre-composed skeleton for the Zerodha Profile page
 */
export function KiteProfileSkeleton() {
  return (
    <div className="space-y-6 animate-in fade-in duration-300">
      {/* Identity Card Skeleton */}
      <div className="bg-white border border-slate-200/90 rounded-3xl p-6 sm:p-7 shadow-xs">
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-5">
          <div className="flex items-center gap-4 sm:gap-5">
            <Skeleton className="w-16 h-16 rounded-2xl shrink-0" />
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <Skeleton className="h-7 w-48" />
                <Skeleton className="h-5 w-20 rounded-full" />
              </div>
              <div className="flex gap-4">
                <Skeleton className="h-3.5 w-24" />
                <Skeleton className="h-3.5 w-32" />
                <Skeleton className="h-3.5 w-28" />
              </div>
            </div>
          </div>
          <div className="space-y-1">
            <Skeleton className="h-3 w-20" />
            <Skeleton className="h-5 w-28 rounded-full" />
          </div>
        </div>
      </div>

      {/* 4 Margin Bento Cards Skeleton */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        {[1, 2, 3, 4].map((i) => (
          <div
            key={i}
            className="bg-white border border-slate-200/90 rounded-3xl p-5 shadow-xs space-y-2"
          >
            <Skeleton className="h-3 w-28" />
            <Skeleton className="h-7 w-36" />
            <Skeleton className="h-3 w-24" />
          </div>
        ))}
      </div>

      {/* Trading Capabilities Skeleton */}
      <div className="bg-white border border-slate-200/90 rounded-3xl p-6 sm:p-7 shadow-xs space-y-4">
        <Skeleton className="h-5 w-48" />
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4 pt-2">
          {[1, 2, 3].map((i) => (
            <div key={i} className="p-4 rounded-2xl bg-slate-50 border border-slate-200/70 space-y-2">
              <Skeleton className="h-3.5 w-28" />
              <div className="flex gap-1.5 pt-1">
                <Skeleton className="h-6 w-12 rounded-lg" />
                <Skeleton className="h-6 w-12 rounded-lg" />
                <Skeleton className="h-6 w-12 rounded-lg" />
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
