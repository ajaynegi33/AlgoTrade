"use client";

import React, { useEffect, useState } from "react";
import { useTradingStore } from "@/store/useTradingStore";
import { useEngineWebSocket } from "@/hooks/useEngineWebSocket";
import { MarketPulseBanner } from "@/components/dashboard/MarketPulseBanner";
import { SectorHeatmapBar } from "@/components/dashboard/SectorHeatmapBar";
import { LiveTopOutliers } from "@/components/dashboard/LiveTopOutliers";
import { FullRankedTable } from "@/components/dashboard/FullRankedTable";
import { HistoricalDataAlert } from "@/components/dashboard/HistoricalDataAlert";
import { DashboardOverviewSkeleton } from "@/components/ui/Skeleton";

export default function LeaderboardPage() {
  const {
    kiteStatus,
    universe,
    isKiteLoading,
    isUniverseLoading,
    fetchKiteStatus,
    fetchUniverse,
  } = useTradingStore();

  const [selectedSectorId, setSelectedSectorId] = useState<number | null>(null);

  // Connect to 1.0s real-time engine stream
  const { snapshot, isConnected, lastUpdated } = useEngineWebSocket();

  useEffect(() => {
    fetchKiteStatus();
    fetchUniverse();
  }, [fetchKiteStatus, fetchUniverse]);

  const isInitialLoading = !kiteStatus && !universe && (isKiteLoading || isUniverseLoading);
  if (isInitialLoading) {
    return <DashboardOverviewSkeleton />;
  }

  return (
    <div className="space-y-6">
      {/* 1. Historical API Subscription Warning (Only shows if inactive/403) */}
      {snapshot && (
        <HistoricalDataAlert
          isActive={snapshot.historical_active}
          errorMessage={snapshot.historical_error}
        />
      )}

      {/* 2. Top Market Pulse & Index Breadth Banner */}
      <MarketPulseBanner
        pulse={snapshot?.market_pulse}
        isConnected={isConnected}
        lastUpdated={lastUpdated}
      />

      {/* 3. Real-Time Sector Rotation Bar */}
      {snapshot?.sectors && snapshot.sectors.length > 0 && (
        <SectorHeatmapBar
          sectors={snapshot.sectors}
          selectedSectorId={selectedSectorId}
          onSelectSector={setSelectedSectorId}
        />
      )}

      {/* 4. Top 5 Bullish (Long) & Bearish (Short) Outlier Hero Cards */}
      <LiveTopOutliers
        topBullish={snapshot?.top_bullish ?? []}
        topBearish={snapshot?.top_bearish ?? []}
      />

      {/* 5. Full 213-Stock Ranked Watchlist Table */}
      <FullRankedTable
        stocks={snapshot?.all_stocks ?? []}
        selectedSectorId={selectedSectorId}
      />
    </div>
  );
}
