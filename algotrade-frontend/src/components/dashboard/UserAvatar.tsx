"use client";

import React, { useState } from "react";

export interface UserAvatarProps {
  avatarUrl?: string | null;
  name?: string | null;
  size?: "sm" | "md" | "lg";
  rounded?: "full" | "2xl";
  className?: string;
  showOnlineDot?: boolean;
}

export default function UserAvatar({
  avatarUrl,
  name,
  size = "sm",
  rounded = "full",
  className = "",
  showOnlineDot = false,
}: UserAvatarProps) {
  const initial = (name || "Ajju").charAt(0).toUpperCase();

  // State tracking: if Zerodha avatar URL fails, fall back to /peep-17.svg. If peep-17 fails, fall back to initial.
  const [failedUrl, setFailedUrl] = useState<string | null>(null);
  const [isPeepFailed, setIsPeepFailed] = useState<boolean>(false);

  const hasZerodha = Boolean(avatarUrl?.trim());
  const useZerodha = hasZerodha && failedUrl !== avatarUrl;
  const currentSrc = useZerodha ? (avatarUrl as string) : "/peep-17.svg";
  const hasError = isPeepFailed && !useZerodha;

  const handleError = () => {
    if (useZerodha && avatarUrl) {
      setFailedUrl(avatarUrl);
    } else {
      setIsPeepFailed(true);
    }
  };

  const sizeClasses = {
    sm: "w-9 h-9 text-xs",
    md: "w-12 h-12 text-base",
    lg: "w-16 h-16 text-2xl",
  }[size];

  const dotSizes = {
    sm: "w-2.5 h-2.5",
    md: "w-3 h-3",
    lg: "w-3.5 h-3.5",
  }[size];

  const roundedClass = rounded === "2xl" ? "rounded-2xl" : "rounded-full";
  const isPeep = currentSrc.includes("peep-");

  return (
    <div className={`relative shrink-0 ${sizeClasses} ${className}`}>
      <div
        className={`w-full h-full ${roundedClass} overflow-hidden bg-slate-100 border border-slate-200/80 shadow-2xs flex items-center justify-center`}
      >
        {!hasError ? (
          // Standard img used to support arbitrary external Zerodha avatar URLs without domain locking
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={currentSrc}
            alt={name || "User Avatar"}
            onError={handleError}
            className={`w-full h-full select-none ${
              isPeep
                ? "object-cover object-top scale-135 origin-top -translate-y-1.5"
                : "object-cover object-center"
            }`}
          />
        ) : (
          <div
            className={`w-full h-full ${roundedClass} bg-gradient-to-tr from-emerald-600 to-teal-500 text-white font-bold flex items-center justify-center shadow-2xs`}
          >
            {initial}
          </div>
        )}
      </div>

      {showOnlineDot && (
        <span
          className={`absolute bottom-0 right-0 ${dotSizes} bg-emerald-500 rounded-full ring-1 ring-white`}
        />
      )}
    </div>
  );
}
