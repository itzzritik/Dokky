"use client";

import { usePathname, useRouter } from "next/navigation";
import {
  type CSSProperties,
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import { prefetch } from "@/hooks/use-resource";
import { useShortcuts } from "@/hooks/use-shortcuts";
import { PAGES, pageIndex } from "../nav";
import { logout } from "../session";
import { CommandPalette } from "./command-palette";
import { type Hint, KeyBar } from "./key-bar";
import { useThemeToggle } from "./theme";
import { TopBar } from "./top-bar";

const BOOT_MS = 1900;
const HintsContext = createContext<(hints: Hint[]) => void>(() => undefined);
const signature = (hints: Hint[]) =>
  hints
    .map(
      (hint) =>
        `${hint.combo}:${hint.label}:${hint.primary ? 1 : 0}:${hint.mobile ? 1 : 0}`
    )
    .join("|");

// Pages pass a fresh array each render; only a changed signature re-renders the bar.
export function useHints(hints: Hint[]) {
  const set = useContext(HintsContext);
  useEffect(() => set(hints));
  useEffect(() => () => set([]), [set]);
}

export function Frame({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const toggleTheme = useThemeToggle();
  const [boot, setBoot] = useState(true);
  const [palette, setPalette] = useState(false);
  const [hints, setHintsState] = useState<Hint[]>([]);
  const shown = useRef("");
  const index = pageIndex(pathname);
  const view = useRef({ dir: 0, index });
  if (view.current.index !== index) {
    view.current = { dir: Math.sign(index - view.current.index), index };
  }

  const setHints = useCallback((next: Hint[]) => {
    const key = signature(next);
    if (key === shown.current) {
      return;
    }
    shown.current = key;
    setHintsState(next);
  }, []);

  useEffect(() => {
    prefetch(
      "/api/dash/drives",
      "/api/dash/devices",
      "/api/dash/activity",
      "/api/dash/me"
    );
  }, []);

  useEffect(() => {
    if (!boot) {
      return;
    }
    const timer = setTimeout(() => setBoot(false), BOOT_MS);
    return () => clearTimeout(timer);
  }, [boot]);

  useShortcuts({
    ...Object.fromEntries(
      PAGES.map((page) => [page.key, () => router.push(page.href)])
    ),
    s: () => void logout(),
    "mod+k": () => setPalette((open) => !open),
    t: toggleTheme,
  });

  return (
    <HintsContext.Provider value={setHints}>
      <div className="frame" data-boot={boot ? "" : undefined}>
        <TopBar index={index} onTheme={toggleTheme} />
        <main
          className="frame-view"
          data-turn={view.current.dir ? "" : undefined}
          key={pathname}
          style={{ "--dir": view.current.dir } as CSSProperties}
        >
          {children}
        </main>
        <KeyBar hints={hints} onPalette={() => setPalette(true)} />
      </div>
      <CommandPalette
        onOpenChange={setPalette}
        onTheme={toggleTheme}
        open={palette}
      />
    </HintsContext.Provider>
  );
}
