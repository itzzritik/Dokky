"use client";

import { useEffect, useRef } from "react";

type Shortcuts = Record<string, () => void>;

interface Entry {
  map: Shortcuts;
  modal: boolean;
}

export const SHORTCUT_EVENT = "shell:shortcut";

const SEQUENCE_MS = 900;
const TYPING =
  "input, textarea, select, [contenteditable='true'], [role='menu'], [role='listbox']";
const PRESSABLE = "button, a[href], [role='checkbox'], summary";
const OPEN_DIALOG =
  "[role='dialog'][data-open], [role='alertdialog'][data-open]";
// Focus stays in a dialog's field until its exit animation ends; that field no longer counts as typing.
const CLOSING = "[data-ending-style], [data-closed]";
// Holding a key may only repeat movement; a held d must not open and confirm a delete.
const REPEATABLE = new Set(["j", "k", "arrowdown", "arrowup"]);

const registry = new Set<{ current: Entry }>();
let prefix = "";
let prefixTimer: ReturnType<typeof setTimeout> | undefined;

function comboOf(event: KeyboardEvent) {
  const key = event.key === "Escape" ? "esc" : event.key.toLowerCase();
  return event.metaKey || event.ctrlKey ? `mod+${key}` : key;
}

function blocked(event: KeyboardEvent, combo: string) {
  if (combo.startsWith("mod+")) {
    return false;
  }
  const target = event.target instanceof Element ? event.target : null;
  if (target?.closest(TYPING) && !target.closest(CLOSING)) {
    return true;
  }
  return (
    (combo === "enter" || combo === " ") && Boolean(target?.closest(PRESSABLE))
  );
}

// While a dialog is open only modal maps answer, so page shortcuts pause; the newest map wins.
function maps(modal: boolean) {
  return [...registry]
    .map((entry) => entry.current)
    .filter((entry) => entry.modal === modal)
    .reverse();
}

export function trigger(combo: string) {
  maps(false)
    .find((entry) => entry.map[combo])
    ?.map[combo]();
}

function onKeyDown(event: KeyboardEvent) {
  if (event.defaultPrevented || event.isComposing || event.altKey) {
    return;
  }
  const key = comboOf(event);
  if (event.repeat && !REPEATABLE.has(key)) {
    return;
  }
  if (blocked(event, key)) {
    return;
  }
  const combo = prefix ? `${prefix} ${key}` : key;
  const continuing = prefix !== "";
  prefix = "";
  clearTimeout(prefixTimer);
  const active = maps(
    !key.startsWith("mod+") && Boolean(document.querySelector(OPEN_DIALOG))
  );
  const handler = active.find((entry) => entry.map[combo])?.map[combo];
  if (handler) {
    event.preventDefault();
    handler();
    window.dispatchEvent(new CustomEvent(SHORTCUT_EVENT, { detail: combo }));
    return;
  }
  if (
    !continuing &&
    active.some((entry) =>
      Object.keys(entry.map).some((name) => name.startsWith(`${key} `))
    )
  ) {
    prefix = key;
    prefixTimer = setTimeout(() => {
      prefix = "";
    }, SEQUENCE_MS);
  }
}

export function useShortcuts(
  shortcuts: Shortcuts,
  enabled = true,
  modal = false
) {
  const ref = useRef<Entry>({ map: shortcuts, modal });
  useEffect(() => {
    ref.current = { map: shortcuts, modal };
  });
  useEffect(() => {
    if (!enabled) {
      return;
    }
    if (registry.size === 0) {
      window.addEventListener("keydown", onKeyDown);
    }
    registry.add(ref);
    return () => {
      registry.delete(ref);
      if (registry.size === 0) {
        window.removeEventListener("keydown", onKeyDown);
      }
    };
  }, [enabled]);
}
