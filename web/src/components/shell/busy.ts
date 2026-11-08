"use client";

import { useSyncExternalStore } from "react";

const listeners = new Set<() => void>();
let running = 0;

const emit = () => {
  for (const listener of listeners) {
    listener();
  }
};

// The top bar sweeps while any change is on its way to the server.
export function busy<T>(work: Promise<T>): Promise<T> {
  running++;
  emit();
  return work.finally(() => {
    running--;
    emit();
  });
}

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => listeners.delete(listener);
};

export function useBusy() {
  return useSyncExternalStore(
    subscribe,
    () => running > 0,
    () => false
  );
}
