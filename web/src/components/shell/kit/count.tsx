"use client";

import { useEffect, useRef } from "react";
import { plural } from "@/lib/format";
import { reducedMotion } from "../motion";

const format = (value: number, word?: string) =>
  word ? plural(value, word) : value.toLocaleString();

export function Count({ value, word }: { value: number; word?: string }) {
  const ref = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el?.closest("[data-boot]") || reducedMotion()) {
      return;
    }
    const start = performance.now();
    let raf = 0;
    const step = (now: number) => {
      const p = Math.min(1, (now - start) / 650);
      el.textContent = format(Math.round(value * (1 - (1 - p) ** 3)), word);
      if (p < 1) {
        raf = requestAnimationFrame(step);
      }
    };
    raf = requestAnimationFrame(step);
    return () => {
      cancelAnimationFrame(raf);
      el.textContent = format(value, word);
    };
  }, [value, word]);
  return <span ref={ref}>{format(value, word)}</span>;
}
