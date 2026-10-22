import { create } from "zustand";
import {
  KiteStatusData,
  KiteUserProfileData,
  UniverseSummaryData,
} from "@/lib/types";
import {
  getKiteStatus,
  getKiteUserProfile,
  getStockUniverse,
  triggerUniverseScan,
} from "@/lib/api";

interface TradingState {
  // State
  kiteStatus: KiteStatusData | null;
  kiteProfile: KiteUserProfileData | null;
  universe: UniverseSummaryData | null;
  isKiteLoading: boolean;
  isProfileLoading: boolean;
  isUniverseLoading: boolean;
  isScanning: boolean;

  // Actions
  fetchKiteStatus: () => Promise<void>;
  fetchKiteProfile: () => Promise<void>;
  fetchUniverse: () => Promise<void>;
  triggerScan: () => Promise<void>;
  setKiteStatus: (status: KiteStatusData | null) => void;
}

export const useTradingStore = create<TradingState>((set, get) => ({
  kiteStatus: null,
  kiteProfile: null,
  universe: null,
  isKiteLoading: false,
  isProfileLoading: false,
  isUniverseLoading: false,
  isScanning: false,

  setKiteStatus: (status) => set({ kiteStatus: status }),

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
    // If universe is already loaded in store, don't re-fetch unnecessarily
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

  triggerScan: async () => {
    set({ isScanning: true });
    try {
      const universe = await triggerUniverseScan();
      set({ universe });
    } finally {
      set({ isScanning: false });
    }
  },
}));
