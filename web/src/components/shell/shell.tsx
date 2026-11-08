"use client";

import { ThemeProvider } from "next-themes";
import { type ReactNode, useEffect } from "react";
import { Frame } from "./frame/frame";

export function Shell({ children }: { children: ReactNode }) {
  // the landing and docs are dark only, so the dashboard's theme leaves with it
  useEffect(
    () => () => {
      document.documentElement.classList.remove("light", "dark");
      document.documentElement.style.colorScheme = "";
    },
    []
  );
  return (
    <ThemeProvider attribute="class" defaultTheme="system" enableSystem>
      <div className="bg-background text-foreground font-sans" data-shell="">
        <Frame>{children}</Frame>
      </div>
    </ThemeProvider>
  );
}
