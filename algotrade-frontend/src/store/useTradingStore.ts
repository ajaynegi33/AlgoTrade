import { create } from "zustand";
import {
  KiteStatusData,
  KiteUserProfileData,
  UniverseSummaryData,
  ExecutionStateData,
  RankedSnapshotData,
} from "@/lib/types";
import {
  getKiteStatus,
  getKiteUserProfile,
  getStockUniverse,
  triggerUniverseScan,
  getExecutionState,
  updateExecutionSettings,
  updatePaperCapital,
  flattenExecutionPosition,
} from "@/lib/api";

interface TradingState {
  // State
  kiteStatus: KiteStatusData | null;
  kiteProfile: KiteUserProfileData | null;
  universe: UniverseSummaryData | null;
  executionState: ExecutionStateData | null;
  liveSnapshot: RankedSnapshotData | null;
  isWsConnected: boolean;
  lastWsUpdatedMs: number;
  wsReconnectCount: number;
  isKiteLoading: boolean;
  isProfileLoading: boolean;
  isUniverseLoading: boolean;
  isScanning: boolean;
  isExecutionUpdating: boolean;

  // Actions
  fetchKiteStatus: () => Promise<void>;
  fetchKiteProfile: () => Promise<void>;
  fetchUniverse: () => Promise<void>;
  fetchExecutionState: () => Promise<void>;
  triggerScan: () => Promise<void>;
  setKiteStatus: (status: KiteStatusData | null) => void;
  setExecutionState: (state: ExecutionStateData | null) => void;
  setLiveSnapshot: (snapshot: RankedSnapshotData) => void;
  setWsConnectionState: (connected: boolean, reconnectDelta?: number) => void;
  updateSettings: (payload: {
    mode?: "PAPER" | "LIVE";
    auto_execute?: boolean;
    paper_capital?: number;
    risk_per_trade_pct?: number;
    max_open_positions?: number;
  }) => Promise<ExecutionStateData>;
  setPaperCapital: (paperCapital: number) => Promise<ExecutionStateData>;
  flattenPosition: (positionId?: string, all?: boolean) => Promise<number>;
}

export const useTradingStore = create<TradingState>((set, get) => ({
  kiteStatus: null,
  kiteProfile: null,
  universe: null,
  executionState: null,
  liveSnapshot: null,
  isWsConnected: false,
  lastWsUpdatedMs: 0,
  wsReconnectCount: 0,
  isKiteLoading: false,
  isProfileLoading: false,
  isUniverseLoading: false,
  isScanning: false,
  isExecutionUpdating: false,

  setKiteStatus: (status) => set({ kiteStatus: status }),
  setExecutionState: (executionState) => set({ executionState }),
  setLiveSnapshot: (snapshot) =>
    set((prev) => ({
      liveSnapshot: snapshot,
      lastWsUpdatedMs: Date.now(),
      executionState: snapshot.execution ?? prev.executionState,
    })),
  setWsConnectionState: (connected, reconnectDelta = 0) =>
    set((prev) => ({
      isWsConnected: connected,
      wsReconnectCount: prev.wsReconnectCount + reconnectDelta,
    })),

  fetchKiteStatus: async () => {
    if (!get().kiteStatus) {
      set({ isKiteLoading: true });
    }
    try {
      const status = await getKiteStatus();
      set({ kiteStatus: status });
    } catch (err) {
      console.warn("[TradingStore] Kite status check:", err);
    } finally {
      set({ isKiteLoading: false });
    }
  },

  fetchKiteProfile: async () => {
    if (!get().kiteProfile) {
      set({ isProfileLoading: true });
    }
    try {
      const profile = await getKiteUserProfile();
      set({ kiteProfile: profile });
    } catch (err) {
      console.warn("[TradingStore] Kite profile check:", err);
    } finally {
      set({ isProfileLoading: false });
    }
  },

  fetchUniverse: async () => {
    if (get().universe) return;

    set({ isUniverseLoading: true });
    try {
      const universe = await getStockUniverse();
      set({ universe });
    } catch (err) {
      console.warn("[TradingStore] Stock universe check:", err);
    } finally {
      set({ isUniverseLoading: false });
    }
  },

  fetchExecutionState: async () => {
    try {
      const state = await getExecutionState();
      set({ executionState: state });
    } catch (err) {
      console.warn("[TradingStore] Execution state check:", err);
    }
  },

  triggerScan: async () => {
    set({ isScanning: true });
    try {
      const universe = await triggerUniverseScan();
      set({ universe });
    } finally {
      set({ isScanning: false });
    }
  },

  updateSettings: async (payload) => {
    set({ isExecutionUpdating: true });
    try {
      const updated = await updateExecutionSettings(payload);
      set({ executionState: updated });
      return updated;
    } finally {
      set({ isExecutionUpdating: false });
    }
  },

  setPaperCapital: async (paperCapital) => {
    set({ isExecutionUpdating: true });
    try {
      const updated = await updatePaperCapital(paperCapital);
      set({ executionState: updated });
      return updated;
    } finally {
      set({ isExecutionUpdating: false });
    }
  },

  flattenPosition: async (positionId, all = false) => {
    set({ isExecutionUpdating: true });
    try {
      const res = await flattenExecutionPosition({
        position_id: positionId,
        all,
      });
      set({ executionState: res.state });
      return res.flattened_count;
    } finally {
      set({ isExecutionUpdating: false });
    }
  },
}));
