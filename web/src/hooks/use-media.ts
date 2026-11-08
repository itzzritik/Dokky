"use client";

import { useCallback, useSyncExternalStore } from "react";

export const WIDE = "(min-width: 1100px)";
export const PHONE = "(max-width: 767px)";

export function useMedia(query: string, server = false) {
  const subscribe = useCallback(
    (notify: () => void) => {
      const list = window.matchMedia(query);
      list.addEventListener("change", notify);
      return () => list.removeEventListener("change", notify);
    },
    [query]
  );
  return useSyncExternalStore(
    subscribe,
    () => window.matchMedia(query).matches,
    () => server
  );
}
