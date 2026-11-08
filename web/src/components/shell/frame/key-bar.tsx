"use client";

import { CommandIcon } from "lucide-react";
import { type CSSProperties, useEffect, useState } from "react";
import { PHONE, useMedia } from "@/hooks/use-media";
import { SHORTCUT_EVENT, trigger, useShortcuts } from "@/hooks/use-shortcuts";
import { Action } from "../kit/action";
import { Keycap } from "../kit/keycap";
import { useMessage } from "../message";
import { replay } from "../motion";

export interface Hint {
  combo: string;
  label: string;
  mobile?: boolean;
  primary?: boolean;
}

interface KeyBarProps {
  hints: Hint[];
  onPalette: () => void;
}

const ICONS = { danger: "!", good: "✓", warn: "!" } as const;

export function KeyBar({ hints, onPalette }: KeyBarProps) {
  const message = useMessage();
  const phone = useMedia(PHONE);
  // Keeps the last message rendered while the line fades out.
  const [shown, setShown] = useState(message);
  if (message && message !== shown) {
    setShown(message);
  }
  const action = message?.action;

  useShortcuts(action ? { [action.key]: action.run } : {});

  useEffect(() => {
    const press = (event: Event) => {
      const combo = (event as CustomEvent<string>).detail;
      for (const cap of document.querySelectorAll(
        `.key-bar [data-keycap="${CSS.escape(combo)}"]`
      )) {
        replay(cap, "press");
      }
    };
    window.addEventListener(SHORTCUT_EVENT, press);
    return () => window.removeEventListener(SHORTCUT_EVENT, press);
  }, []);

  return (
    <footer className="key-bar" data-say={message ? "" : undefined}>
      <div className="key-hints">
        {phone ? (
          <>
            {hints.map(
              (hint) =>
                hint.mobile && (
                  <Action
                    className="h-10 flex-1"
                    key={hint.combo}
                    onClick={() => trigger(hint.combo)}
                    tone={hint.primary ? "primary" : "outline"}
                  >
                    {hint.label}
                  </Action>
                )
            )}
            <Action
              aria-label="Open commands"
              className="h-10 w-11 px-0"
              onClick={onPalette}
              tone="outline"
            >
              <CommandIcon />
            </Action>
          </>
        ) : (
          <>
            {hints.map((hint, index) => (
              <button
                key={hint.combo}
                onClick={() => trigger(hint.combo)}
                style={{ "--i": index } as CSSProperties}
                type="button"
              >
                <Keycap combo={hint.combo} />
                {hint.label}
              </button>
            ))}
            <button
              className="ml-auto"
              onClick={onPalette}
              style={{ "--i": hints.length } as CSSProperties}
              type="button"
            >
              <Keycap combo="mod+k" />
              Commands
            </button>
          </>
        )}
      </div>
      <div
        aria-live="polite"
        className="key-message"
        data-tone={shown?.tone}
        role="status"
      >
        {shown && (
          <>
            <span aria-hidden className="key-message-icon">
              {ICONS[shown.tone]}
            </span>
            <span className="min-w-0 truncate" key={shown.id}>
              {shown.text}
            </span>
            {shown.action && (
              <button
                className="text-foreground ml-auto inline-flex items-center gap-2"
                onClick={shown.action.run}
                type="button"
              >
                <Keycap combo={shown.action.key} />
                {shown.action.label}
              </button>
            )}
          </>
        )}
      </div>
    </footer>
  );
}
