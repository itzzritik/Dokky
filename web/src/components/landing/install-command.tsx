"use client";

import { useCallback, useState } from "react";

export function InstallCommand({
  command = "npm i -g dokky",
  className = "",
}: {
  command?: string;
  className?: string;
}) {
  const [copied, setCopied] = useState(false);
  const copy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    } catch {
      setCopied(false);
    }
  }, [command]);
  return (
    <div
      className={`group flex h-12 items-center border border-[#27272a] bg-[#09090b] transition-colors hover:border-[#a1a1aa]/30 ${className}`}
    >
      <span className="mx-3 font-mono text-lg text-[#27272a] select-none">
        $
      </span>
      <code className="pr-4 font-mono text-sm tracking-wide text-white">
        {command}
      </code>
      <button
        aria-label={copied ? "Copied" : "Copy command"}
        className="flex h-full w-12 cursor-pointer items-center justify-center border-l border-[#27272a] text-[#a1a1aa] transition-colors hover:bg-white/5 hover:text-white"
        onClick={copy}
        type="button"
      >
        {copied ? (
          <svg
            aria-hidden
            fill="none"
            height="14"
            stroke="#6ed69a"
            strokeWidth="2"
            viewBox="0 0 24 24"
            width="14"
          >
            <polyline points="20 6 9 17 4 12" />
          </svg>
        ) : (
          <svg
            aria-hidden
            fill="none"
            height="14"
            stroke="currentColor"
            strokeWidth="2"
            viewBox="0 0 24 24"
            width="14"
          >
            <rect height="13" width="13" x="9" y="9" />
            <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
          </svg>
        )}
      </button>
    </div>
  );
}
