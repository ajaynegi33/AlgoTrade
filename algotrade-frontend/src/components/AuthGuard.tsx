"use client";

import { useAuth } from "../context/AuthContext";
import { useRouter } from "next/navigation";
import { useEffect, ReactNode, useSyncExternalStore } from "react";
import Image from "next/image";

const emptySubscribe = () => () => {};

function useIsClient() {
  return useSyncExternalStore(
    emptySubscribe,
    () => true,
    () => false
  );
}

export default function AuthGuard({ children }: { children: ReactNode }) {
  const { isAuthenticated, isLoading } = useAuth();
  const router = useRouter();
  const isClient = useIsClient();

  useEffect(() => {
    if (!isLoading && !isAuthenticated) {
      router.replace("/login");
    }
  }, [isLoading, isAuthenticated, router]);

  // While verifying session on client mount, show clean light theme loader
  if (isClient && isLoading) {
    return (
      <div className="min-h-screen bg-slate-50 flex flex-col items-center justify-center p-6 text-slate-900 selection:bg-emerald-500/20">
        <div className="flex flex-col items-center gap-4 animate-in fade-in duration-300">
          <div className="w-14 h-14 rounded-2xl bg-white border border-slate-200/80 p-2 flex items-center justify-center shadow-sm">
            <Image
              src="/AlgoTrade.png"
              alt="AlgoTrade Logo"
              width={40}
              height={40}
              className="w-full h-full object-contain"
              priority
            />
          </div>
          <div className="flex items-center gap-2.5 text-xs font-mono font-medium text-slate-500">
            <div className="w-3.5 h-3.5 border-2 border-emerald-600/30 border-t-emerald-600 rounded-full animate-spin" />
            <span>Verifying secure session...</span>
          </div>
        </div>
      </div>
    );
  }

  // On client after auth check completes, block unauthenticated view before redirect
  if (isClient && !isLoading && !isAuthenticated) {
    return null;
  }

  return <>{children}</>;
}
