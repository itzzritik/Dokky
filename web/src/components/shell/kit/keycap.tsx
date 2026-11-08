"use client";

import { useSyncExternalStore } from "react";
import { cn } from "@/lib/utils";

const APPLE = /Mac|iPhone|iPad/u;
const GLYPHS: Record<string, string> = {
  arrowdown: "↓",
  arrowup: "↑",
  enter: "↵",
  esc: "esc",
};
const subscribe = () => () => undefined;
const isApple = () => APPLE.test(navigator.userAgent);

export function Keycap({
  className,
  combo,
}: {
  className?: string;
  combo: string;
}) {
  const apple = useSyncExternalStore(subscribe, isApple, () => true);
  const mod = combo.startsWith("mod+");
  const key = mod ? combo.slice(4) : combo;
  const label = GLYPHS[key] ?? (mod ? key.toUpperCase() : key);
  return (
    <kbd className={cn("keycap", className)} data-keycap={combo}>
      {mod ? `${apple ? "⌘" : "Ctrl "}${label}` : label}
    </kbd>
  );
}
