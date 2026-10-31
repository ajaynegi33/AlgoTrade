"use client";

import { useEffect } from "react";
import { useParams, useRouter } from "next/navigation";

export default function SearchSymbolRedirectPage() {
  const params = useParams();
  const router = useRouter();
  const symbol = typeof params?.symbol === "string" ? params.symbol : "";

  useEffect(() => {
    if (symbol) {
      router.replace(`/stock/${encodeURIComponent(symbol)}`);
    } else {
      router.replace("/leaderboard");
    }
  }, [symbol, router]);

  return (
    <div className="min-h-[300px] flex items-center justify-center font-mono text-xs text-slate-400 animate-pulse">
      Navigating to /stock/{symbol}...
    </div>
  );
}
