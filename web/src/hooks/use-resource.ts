"use client";

import { useCallback, useEffect, useState } from "react";

interface Resource<T> {
  data: T | null;
  error: string | null;
}

const cache = new Map<string, unknown>();
const inflight = new Map<string, Promise<unknown>>();
const watchers = new Map<string, Set<(data: unknown) => void>>();
let leaving = false;

// signing in again here would undo a sign-out made in another tab
export function signedOut() {
  if (leaving) {
    return;
  }
  leaving = true;
  location.assign("/");
}

function request<T>(url: string): Promise<T> {
  const running = inflight.get(url);
  if (running) {
    return running as Promise<T>;
  }
  const next = fetch(url)
    .then(async (response) => {
      if (response.status === 401) {
        signedOut();
      }
      if (!response.ok) {
        throw new Error(
          `The server answered ${response.status}. Try again in a moment.`
        );
      }
      const data = (await response.json()) as T;
      cache.set(url, data);
      for (const watch of watchers.get(url) ?? []) {
        watch(data);
      }
      return data;
    })
    .finally(() => inflight.delete(url));
  inflight.set(url, next);
  return next;
}

export function prefetch(...urls: string[]) {
  for (const url of urls) {
    request(url).catch(() => undefined);
  }
}

// Cached data shows at once and refreshes in the background, so revisiting a page never flashes a skeleton.
export function useResource<T>(url: string) {
  const [state, setState] = useState<Resource<T>>(() => ({
    data: (cache.get(url) as T | undefined) ?? null,
    error: null,
  }));

  useEffect(() => {
    const set = watchers.get(url) ?? new Set();
    watchers.set(url, set);
    const watch = (data: unknown) => setState({ data: data as T, error: null });
    set.add(watch);
    return () => {
      set.delete(watch);
    };
  }, [url]);

  const load = useCallback(() => {
    let live = true;
    setState((current) => ({ ...current, error: null }));
    request<T>(url).catch((error: unknown) => {
      if (!live) {
        return;
      }
      const message =
        error instanceof Error ? error.message : "Couldn't reach the server.";
      setState((current) => ({
        data: current.data,
        error: current.data ? null : message,
      }));
    });
    return () => {
      live = false;
    };
  }, [url]);

  useEffect(() => load(), [load]);

  // No polling: coming back to the tab is when the data may have moved on.
  useEffect(() => {
    const onShow = () => {
      if (document.visibilityState === "visible") {
        load();
      }
    };
    document.addEventListener("visibilitychange", onShow);
    return () => document.removeEventListener("visibilitychange", onShow);
  }, [load]);

  return {
    ...state,
    loading: state.data === null && state.error === null,
    retry: load,
  };
}
