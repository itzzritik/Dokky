"use client";

import { useEffect } from "react";

// Focus styles show only after Tab, so a clicked button never lights up while arrow keys move a table.
export function Tabbing() {
  useEffect(() => {
    const root = document.documentElement;
    const on = (event: KeyboardEvent) => {
      if (event.key === "Tab") {
        root.dataset.tabbing = "";
      }
    };
    const off = () => {
      delete root.dataset.tabbing;
    };
    window.addEventListener("keydown", on, true);
    window.addEventListener("pointerdown", off, true);
    return () => {
      window.removeEventListener("keydown", on, true);
      window.removeEventListener("pointerdown", off, true);
    };
  }, []);
  return null;
}
