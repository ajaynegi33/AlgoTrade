"use client";

interface PipelineStage {
  step: string;
  timeWindow: string;
  title: string;
  plainEnglishDescription: string;
  metricBadges: { label: string; value: string }[];
}

const STAGES: PipelineStage[] = [
  {
    step: "Step 1",
    timeWindow: "8:45 AM – 9:15 AM",
    title: "Morning Liquidity Scanner",
    plainEnglishDescription:
      "The system screens 500 major NSE companies before the market opens, searching for stocks with at least 2.5 times higher buyer interest than normal. We only trade high-liquidity companies where institutional money is actively entering.",
    metricBadges: [
      { label: "Volume Threshold", value: "2.5x Normal" },
      { label: "Universe", value: "Top 500 Liquid Equities" },
      { label: "Penny Stock Filter", value: "Strictly Blocked" },
    ],
  },
  {
    step: "Step 2",
    timeWindow: "9:15 AM – 9:45 AM",
    title: "Breakout Wave Confirmation",
    plainEnglishDescription:
      "The algorithm waits for price to cross cleanly above yesterday's highest price point. If the crossing happens with expanding volume and remains firmly above the average session price, an upward breakout wave is mathematically confirmed.",
    metricBadges: [
      { label: "Price Trigger", value: "Above Yesterday's High" },
      { label: "Momentum", value: "Fast Trend Line Cross" },
      { label: "Support", value: "Above Session Average" },
    ],
  },
  {
    step: "Step 3",
    timeWindow: "Microsecond Execution",
    title: "Order Book Buyer Depth Check",
    plainEnglishDescription:
      "Milliseconds before sending the order, the system examines the deepest 5 layers of buyer and seller queues. We only execute if buyers make up over 60% of total queue depth, guaranteeing your trade fills immediately without price slippage.",
    metricBadges: [
      { label: "Buyer Dominance", value: "> 60% Order Queue" },
      { label: "Whale Wall Check", value: "Heavy Bid Defense" },
      { label: "Execution Speed", value: "Sub-15ms Direct" },
    ],
  },
];

export default function EngineStrategyPipeline() {
  return (
    <section
      id="pipeline-section"
      className="bg-white border border-slate-200/90 rounded-3xl p-6 sm:p-8 shadow-xs flex flex-col gap-6 scroll-mt-20"
    >
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-slate-100 pb-5">
        <div>
          <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-slate-100 border border-slate-200 text-slate-700 text-xs font-mono font-semibold mb-2">
            <span>Stage 1 to 3 Execution Pipeline</span>
          </div>
          <h2 className="text-2xl sm:text-3xl font-black text-slate-900 tracking-tight">
            How the Engine Chooses Which Stock to Trade
          </h2>
          <p className="text-sm text-slate-600 mt-1 max-w-2xl leading-relaxed">
            Every trade follows three strict rules. If any rule fails, the trade is discarded. Zero guesswork, zero emotional impulse.
          </p>
        </div>
      </div>

      {/* 3-Step Card Pipeline */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-5">
        {STAGES.map((stage) => (
          <div
            key={stage.step}
            className="rounded-2xl border border-slate-200/80 bg-slate-50/60 p-5 sm:p-6 flex flex-col justify-between hover:border-slate-300 transition-colors"
          >
            <div>
              {/* Step & Time badge */}
              <div className="flex items-center justify-between mb-3 text-xs font-mono">
                <span className="font-bold text-slate-900 px-2 py-0.5 rounded-md bg-white border border-slate-200">
                  {stage.step}
                </span>
                <span className="text-slate-500 font-semibold">{stage.timeWindow}</span>
              </div>

              {/* Title */}
              <h3 className="text-base font-bold text-slate-900 tracking-tight mb-2">
                {stage.title}
              </h3>

              {/* Description */}
              <p className="text-xs text-slate-600 leading-relaxed">
                {stage.plainEnglishDescription}
              </p>
            </div>

            {/* Metric Badges */}
            <div className="mt-5 pt-4 border-t border-slate-200/70 space-y-2">
              {stage.metricBadges.map((badge) => (
                <div
                  key={badge.label}
                  className="flex items-center justify-between text-[11px] font-mono"
                >
                  <span className="text-slate-500">{badge.label}:</span>
                  <span className="font-bold text-slate-900 bg-white px-2 py-0.5 rounded border border-slate-200/80">
                    {badge.value}
                  </span>
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}
