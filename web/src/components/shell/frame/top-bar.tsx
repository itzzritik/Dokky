"use client";

import { SunMoonIcon } from "lucide-react";
import Link from "next/link";
import { useLayoutEffect, useRef, useState } from "react";
import { DokkyMark } from "@/components/landing/brand";
import { useBusy } from "../busy";
import { PAGES } from "../nav";

export function TopBar({
  index,
  onTheme,
}: {
  index: number;
  onTheme: () => void;
}) {
  const syncing = useBusy();
  const nav = useRef<HTMLElement>(null);
  const tabs = useRef<(HTMLAnchorElement | null)[]>([]);
  const [line, setLine] = useState({ left: 0, width: 0 });

  useLayoutEffect(() => {
    const measure = () => {
      const tab = tabs.current[index];
      if (tab) {
        setLine({ left: tab.offsetLeft - 6, width: tab.offsetWidth + 12 });
      }
    };
    measure();
    const observer = new ResizeObserver(measure);
    if (nav.current) {
      observer.observe(nav.current);
    }
    return () => observer.disconnect();
  }, [index]);

  return (
    <header className="top-bar">
      <Link
        aria-label="dokky home"
        className="text-foreground"
        href="/dashboard"
      >
        <DokkyMark width={52} />
      </Link>
      <nav aria-label="Pages" className="tabs" ref={nav}>
        {PAGES.map((page, i) => (
          <Link
            aria-current={i === index ? "page" : undefined}
            className="tab"
            href={page.href}
            key={page.href}
            ref={(el) => {
              tabs.current[i] = el;
            }}
          >
            {page.label}
          </Link>
        ))}
        <i
          aria-hidden
          className="tab-line"
          style={{ transform: `translateX(${line.left}px)`, width: line.width }}
        />
      </nav>
      <button
        aria-label="Switch theme"
        className="text-muted-foreground hover:bg-raise hover:text-foreground ml-auto grid size-7 place-items-center rounded-md transition-colors"
        data-theme-toggle=""
        onClick={onTheme}
        type="button"
      >
        <SunMoonIcon className="size-4" />
      </button>
      <i
        aria-hidden
        className="sweep sync-sweep"
        data-on={syncing ? "" : undefined}
      />
    </header>
  );
}
