"use client";

import React, { useState } from "react";
import { toast } from "sonner";

interface PaperFundsModalProps {
  isOpen: boolean;
  currentCapital: number;
  onClose: () => void;
  onSave: (newCapital: number) => Promise<void>;
}

const PRESET_CAPITALS = [
  { label: "₹1,000 (Micro)", value: 1000 },
  { label: "₹10,000", value: 10000 },
  { label: "₹25,000", value: 25000 },
  { label: "₹1,00,000 (1L)", value: 100000 },
  { label: "₹5,00,000 (5L)", value: 500000 },
  { label: "₹25,00,000 (25L)", value: 2500000 },
  { label: "₹1,00,00,000 (1Cr)", value: 10000000 },
];

export function PaperFundsModal({
  isOpen,
  currentCapital,
  onClose,
  onSave,
}: PaperFundsModalProps) {
  const [amount, setAmount] = useState<string>(String(currentCapital || 100000));
  const [isSubmitting, setIsSubmitting] = useState(false);

  if (!isOpen) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const parsed = Number(amount);
    if (!Number.isFinite(parsed) || parsed < 500 || parsed > 1000000000) {
      toast.error("Paper capital must be between ₹500 and ₹100 Crore");
      return;
    }

    setIsSubmitting(true);
    try {
      await onSave(parsed);
      toast.success(`Paper trading capital updated to ₹${parsed.toLocaleString("en-IN")}`);
      onClose();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to update paper capital";
      toast.error(msg);
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 bg-slate-900/50 backdrop-blur-xs flex items-center justify-center p-4">
      <div className="modal-surface bg-white border border-slate-200 rounded-3xl max-w-md w-full p-6 shadow-xl font-mono">
        <div className="flex items-center justify-between pb-3 border-b border-slate-100">
          <div>
            <h3 className="text-sm font-black text-slate-900 uppercase tracking-wider">
              Configure Paper Capital
            </h3>
            <p className="text-[11px] text-slate-500 mt-0.5">
              Dynamic 4-Constraint Sizer scales from ₹500 to ₹10 Crore+
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1.5 rounded-xl text-slate-400 hover:text-slate-700 hover:bg-slate-100 cursor-pointer"
          >
            ✕
          </button>
        </div>

        <form onSubmit={handleSubmit} className="mt-4 space-y-4">
          <div>
            <label className="block text-[11px] font-bold text-slate-600 uppercase mb-1.5">
              Virtual Account Equity (₹)
            </label>
            <div className="relative">
              <span className="absolute left-3.5 top-1/2 -translate-y-1/2 text-slate-400 font-bold">
                ₹
              </span>
              <input
                type="number"
                inputMode="numeric"
                enterKeyHint="done"
                autoComplete="off"
                min={500}
                max={1000000000}
                step="100"
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
                className="w-full pl-8 pr-4 py-2.5 bg-slate-50 border border-slate-200 rounded-xl text-sm font-black text-slate-900 focus:bg-white focus:outline-none focus:ring-2 focus:ring-emerald-500/20 focus:border-emerald-500"
              />
            </div>
          </div>

          <div>
            <span className="block text-[10px] font-bold text-slate-400 uppercase mb-2">
              Quick Presets
            </span>
            <div className="flex flex-wrap gap-1.5">
              {PRESET_CAPITALS.map((preset) => (
                <button
                  key={preset.value}
                  type="button"
                  onClick={() => setAmount(String(preset.value))}
                  className="text-[11px] font-bold px-2.5 py-1 rounded-xl border border-slate-200 bg-slate-50 hover:bg-slate-900 hover:text-white hover:border-slate-900 transition-colors cursor-pointer"
                >
                  {preset.label}
                </button>
              ))}
            </div>
          </div>

          <div className="pt-3 border-t border-slate-100 flex items-center justify-end gap-2">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 rounded-xl border border-slate-200 text-xs font-bold text-slate-600 hover:bg-slate-100 cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={isSubmitting}
              className="px-4 py-2 rounded-xl bg-emerald-600 hover:bg-emerald-700 disabled:opacity-50 text-xs font-bold text-white cursor-pointer shadow-xs"
            >
              {isSubmitting ? "Saving..." : "Apply Capital"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
