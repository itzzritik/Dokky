"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { signOutOfDokky } from "@/app/actions";

export function UserMenu({
  name,
  image,
}: {
  name: string;
  image: string | null;
}) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const toggle = useCallback(() => setOpen((v) => !v), []);

  useEffect(() => {
    if (!open) {
      return;
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setOpen(false);
        trigger.current?.focus();
      }
    };
    const onClick = (e: MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onClick);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onClick);
    };
  }, [open]);

  return (
    <div className="relative" ref={root}>
      <button
        aria-controls="user-menu"
        aria-expanded={open}
        aria-label={`${name}, account menu`}
        className="group flex items-center gap-3"
        onClick={toggle}
        ref={trigger}
        type="button"
      >
        <span className="hidden text-[12px] tracking-wide text-[#a1a1aa] transition-colors group-hover:text-white sm:inline">
          {name}
        </span>
        {image ? (
          // biome-ignore lint/performance/noImgElement: tiny remote avatar, not worth next/image config
          <img
            alt=""
            className="h-7 w-7 border border-[#27272a] object-cover transition-colors group-hover:border-[#22b8cc]"
            height={28}
            src={image}
            width={28}
          />
        ) : (
          <span className="flex h-7 w-7 items-center justify-center bg-[#22b8cc] font-mono text-[11px] font-bold text-[#03161c]">
            {name.slice(0, 1).toUpperCase()}
          </span>
        )}
      </button>
      {open ? (
        <div
          className="absolute top-11 right-0 w-44 border border-[#27272a] bg-[#050505] py-1 shadow-2xl"
          id="user-menu"
        >
          <Link
            className="block px-4 py-2.5 font-mono text-[11px] tracking-widest text-[#a1a1aa] uppercase transition-colors hover:bg-white/5 hover:text-white"
            href="/devices"
          >
            Devices
          </Link>
          <form action={signOutOfDokky}>
            <button
              className="block w-full px-4 py-2.5 text-left font-mono text-[11px] tracking-widest text-[#a1a1aa] uppercase transition-colors hover:bg-white/5 hover:text-[#f2665a]"
              type="submit"
            >
              Sign out
            </button>
          </form>
        </div>
      ) : null}
    </div>
  );
}
