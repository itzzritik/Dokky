"use client";

import { useTheme } from "next-themes";
import { useCallback } from "react";
import { EASE, reducedMotion } from "../motion";

export function useThemeToggle() {
  const { resolvedTheme, setTheme } = useTheme();
  return useCallback(() => {
    const next = resolvedTheme === "dark" ? "light" : "dark";
    // The class flips inside the transition so the snapshot shows the new theme; next-themes then persists it.
    const apply = () => {
      document.documentElement.classList.toggle("dark", next === "dark");
      setTheme(next);
    };
    if (!document.startViewTransition || reducedMotion()) {
      apply();
      return;
    }
    const box = document
      .querySelector("[data-theme-toggle]")
      ?.getBoundingClientRect();
    const x = box ? box.left + box.width / 2 : innerWidth - 32;
    const y = box ? box.top + box.height / 2 : 24;
    const radius = Math.hypot(
      Math.max(x, innerWidth - x),
      Math.max(y, innerHeight - y)
    );
    document
      .startViewTransition(apply)
      .ready.then(() =>
        document.documentElement.animate(
          {
            clipPath: [
              `circle(0 at ${x}px ${y}px)`,
              `circle(${radius}px at ${x}px ${y}px)`,
            ],
          },
          {
            duration: 560,
            easing: EASE,
            pseudoElement: "::view-transition-new(root)",
          }
        )
      )
      .catch(() => undefined);
  }, [resolvedTheme, setTheme]);
}
