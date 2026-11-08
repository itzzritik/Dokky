"use client";

import { useSyncExternalStore } from "react";

type Tone = "good" | "warn" | "danger";

export interface Message {
  action?: { key: string; label: string; run: () => void };
  id: number;
  text: string;
  tone: Tone;
}

const HOLD_MS = 2600;
const listeners = new Set<() => void>();
let current: Message | null = null;
let pinned: Message | null = null;
let seq = 0;
let timer: ReturnType<typeof setTimeout> | undefined;

const emit = () => {
  for (const listener of listeners) {
    listener();
  }
};

export function say(text: string, tone: Tone = "good") {
  clearTimeout(timer);
  const id = ++seq;
  current = { id, text, tone };
  emit();
  timer = setTimeout(() => {
    if (current?.id !== id) {
      return;
    }
    current = null;
    emit();
  }, HOLD_MS);
}

export function pin(text: string, action: Message["action"]) {
  pinned = { action, id: ++seq, text, tone: "danger" };
  emit();
}

export function unpin() {
  if (!pinned) {
    return;
  }
  pinned = null;
  emit();
}

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => listeners.delete(listener);
};
const snapshot = () => current ?? pinned;

export function useMessage() {
  return useSyncExternalStore(subscribe, snapshot, () => null);
}
