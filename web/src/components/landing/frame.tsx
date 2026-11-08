import {
  ArrowLeftIcon,
  type LucideIcon,
  SquareTerminalIcon,
} from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";
import { DokkyMark } from "./brand";
import { AnimatedTerminalGrid, TERMINAL_CARDS } from "./client";
import { Caustics } from "./water";

const TEAL = "#22b8cc";
export const GREEN = "#6ed69a";
export const CORAL = "#f2665a";

export function Frame({
  children,
  color = TEAL,
  icon: Icon = SquareTerminalIcon,
  text,
  title,
}: {
  children?: ReactNode;
  color?: string;
  icon?: LucideIcon;
  text: string;
  title: string;
}) {
  return (
    <div
      className="relative flex min-h-svh flex-col items-center justify-center overflow-hidden bg-black px-6 py-24"
      data-landing
    >
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 overflow-hidden select-none"
      >
        <div className="absolute inset-0 opacity-45">
          <AnimatedTerminalGrid cards={TERMINAL_CARDS} />
        </div>
        <Caustics />
        <div className="absolute inset-0 bg-black/40" />
        <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_center,transparent_0%,black_100%)]" />
        <div className="absolute inset-x-0 top-0 h-32 bg-linear-to-b from-black to-transparent" />
        <div className="absolute inset-x-0 bottom-0 h-32 bg-linear-to-t from-black to-transparent" />
      </div>

      <nav className="fixed top-0 right-0 left-0 z-50 border-b border-[#27272a] bg-black/80 backdrop-blur-xl">
        <div className="flex h-14 items-center justify-between px-6 lg:px-16">
          <Link aria-label="dokky home" href="/">
            <DokkyMark water width={74} />
          </Link>
          <Link
            className="flex items-center gap-2 font-mono text-[12px] tracking-wider text-[#a1a1aa] uppercase transition-colors hover:text-white"
            href="/"
          >
            <ArrowLeftIcon className="size-3" />
            Back
          </Link>
        </div>
      </nav>

      <main className="animate-slide-up relative z-10 w-full max-w-[440px]">
        <div className="mb-10 flex flex-col items-center text-center">
          <div className="relative">
            <div
              className="absolute inset-0 scale-150 blur-[24px]"
              style={{ background: `${color}26` }}
            />
            <div className="relative flex size-14 items-center justify-center border border-[#27272a] bg-black">
              <Icon color={color} size={24} strokeWidth={1.5} />
            </div>
          </div>
          <h1 className="mt-8 text-3xl font-bold tracking-tighter text-white sm:text-4xl">
            {title}
          </h1>
          <p className="mt-3 text-sm tracking-wide text-[#a1a1aa]">{text}</p>
        </div>
        {children}
      </main>
    </div>
  );
}

export function Card({
  aside,
  children,
  label,
}: {
  aside: string;
  children: ReactNode;
  label: string;
}) {
  return (
    <div className="overflow-hidden border border-[#27272a] bg-[#050505]">
      <div className="flex h-10 items-center justify-between border-b border-[#27272a] bg-[#030303] px-6">
        <div className="flex items-center gap-3">
          <span className="size-1.5 animate-pulse rounded-full bg-[#22b8cc] shadow-[0_0_8px_#22b8cc]" />
          <span className="font-mono text-[10px] tracking-widest text-[#a1a1aa] uppercase">
            {label}
          </span>
        </div>
        <span className="font-mono text-[9px] tracking-widest text-[#52525b] uppercase">
          {aside}
        </span>
      </div>
      <div className="p-6 sm:p-8">{children}</div>
    </div>
  );
}
