"use client";

import {
  AnimatePresence,
  motion,
  useInView,
  useReducedMotion,
} from "framer-motion";
import Link from "next/link";
import {
  type AnchorHTMLAttributes,
  type ReactNode,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";

const EASE = [0.16, 1, 0.3, 1] as const;

const prefersStill = () =>
  window.matchMedia("(prefers-reduced-motion: reduce)").matches;

export function ScrollReveal({
  children,
  delay = 0,
  className = "",
}: {
  children: ReactNode;
  delay?: number;
  className?: string;
}) {
  const ref = useRef(null);
  const inView = useInView(ref, { once: true, margin: "-80px" });
  const still = useReducedMotion();

  return (
    <motion.div
      animate={inView ? { opacity: 1, y: 0 } : { opacity: 0, y: 30 }}
      className={className}
      initial={{ opacity: 0, y: 30 }}
      ref={ref}
      transition={
        still ? { duration: 0 } : { duration: 0.7, delay, ease: EASE }
      }
    >
      {children}
    </motion.div>
  );
}

export interface TerminalCardDef {
  brightness: number;
  lines: string[];
  pace: "fast" | "normal" | "slow";
  status: "ok" | "warn" | "error";
  title: string;
}

const PACE_CONFIG = {
  fast: { lineInterval: 160, holdTime: 2000 },
  normal: { lineInterval: 460, holdTime: 4000 },
  slow: { lineInterval: 640, holdTime: 5600 },
} as const;

const DOT_COLORS = {
  ok: { idle: "bg-[#6ed69a]/80", active: "bg-[#6ed69a] animate-pulse" },
  warn: { idle: "bg-[#f2b544]/80", active: "bg-[#f2b544] animate-pulse" },
  error: { idle: "bg-[#f2665a]/80", active: "bg-[#f2665a] animate-pulse" },
} as const;

function lineColor(line: string) {
  if (line.startsWith(">")) {
    return "#999";
  }
  if (line.includes("[WARN]")) {
    return "rgb(242 181 68 / 0.8)";
  }
  if (line.includes("[ERR]")) {
    return "rgb(242 102 90 / 0.8)";
  }
  return "#666";
}

export const TERMINAL_CARDS: TerminalCardDef[] = [
  {
    title: "SETUP // FIRST RUN",
    status: "ok",
    brightness: 1.0,
    pace: "normal",
    lines: [
      "> dokky",
      "[TUI] Welcome to dokky",
      "[AUTH] Sign in with browser",
      "[AUTH] Code BCDF-GHJK approved",
      "[SYNC] Pulling your drives",
      "[SYNC] 4 drives ready to mount",
      "[DAEMON] Running at login",
    ],
  },
  {
    title: "DRIVE // ADD",
    status: "ok",
    brightness: 0.75,
    pace: "slow",
    lines: [
      "> dokky",
      "[TUI] Drives > Add",
      "[AUTH] Google Drive signed in",
      "[MOUNT] Google Drive",
      "[OK] Mounted in 1.2s",
    ],
  },
  {
    title: "UPLOAD // QUEUE",
    status: "ok",
    brightness: 1.1,
    pace: "fast",
    lines: [
      "[VFS] report.pdf saved",
      "[UPLOAD] report.pdf 4.2 MB",
      "[UPLOAD] 38% at 18 MB/s",
      "[UPLOAD] 81% at 19 MB/s",
      "[UPLOAD] report.pdf done",
      "[CACHE] 1.1 GB of 10 GB",
    ],
  },
  {
    title: "SYNC // DEVICES",
    status: "ok",
    brightness: 1.1,
    pace: "fast",
    lines: [
      "> dokky",
      "[SYNC] Checking for changes",
      "[SYNC] Home NAS added on mac-mini",
      "[SYNC] 3 devices up to date",
      "[OK] Ready to mount",
    ],
  },
  {
    title: "DEVICE // REMOVE",
    status: "ok",
    brightness: 0.9,
    pace: "normal",
    lines: [
      "> dokky",
      "[TUI] Settings > Your devices > office-pc",
      "[SERVER] office-pc removed",
      "[OFFICE-PC] Finishing 2 uploads",
      "[OFFICE-PC] Erased",
    ],
  },
  {
    title: "HEALTH // CHECK",
    status: "ok",
    brightness: 1.0,
    pace: "normal",
    lines: [
      "> dokky doctor",
      "[OK] Service running",
      "[OK] File system driver installed",
      "[OK] 4 drives mounted",
      "[OK] Cache within limit",
    ],
  },
  {
    title: "MOUNT // RECOVER",
    status: "warn",
    brightness: 1.0,
    pace: "normal",
    lines: [
      "[NET] Wi-Fi changed",
      "[WARN] Work OneDrive reconnecting",
      "[MOUNT] Back after 3s",
      "[OK] Nothing lost",
    ],
  },
  {
    title: "CACHE // LIMIT",
    status: "ok",
    brightness: 0.8,
    pace: "slow",
    lines: [
      "[CACHE] Limit 10 GB, max age 30d",
      "[CACHE] Trimmed 412 MB",
      "[CACHE] Kept files still open",
    ],
  },
  {
    title: "ACTIVITY // LOG",
    status: "ok",
    brightness: 0.85,
    pace: "slow",
    lines: [
      "[LOG] Mounted Photos S3",
      "[LOG] Renamed Home NAS",
      "[LOG] Removed device office-pc",
      "[LOG] Signed in again to OneDrive",
    ],
  },
  {
    title: "DRIVE // SPACE",
    status: "ok",
    brightness: 1.0,
    pace: "normal",
    lines: [
      "> dokky",
      "[SPACE] Google Drive 1.4 of 2 TB",
      "[SPACE] Home NAS 904 GB free",
      "[SPACE] Photos S3 unlimited",
    ],
  },
  {
    title: "UPDATE // SELF",
    status: "ok",
    brightness: 1.1,
    pace: "fast",
    lines: [
      "> dokky",
      "[UPDATE] New version available",
      "[UPDATE] Waiting for uploads",
      "[UPDATE] Installed, restarting",
      "[OK] Drives back in 2s",
    ],
  },
  {
    title: "EJECT // SAFE",
    status: "ok",
    brightness: 0.9,
    pace: "normal",
    lines: [
      "[FINDER] Home NAS ejected",
      "[UPLOAD] Waiting for 1 file",
      "[UPLOAD] notes.md done",
      "[OK] Unmounted safely",
    ],
  },
  {
    title: "SLEEP // WAKE",
    status: "ok",
    brightness: 0.75,
    pace: "slow",
    lines: [
      "[SLEEP] Mac woke up",
      "[SYNC] Checking for changes",
      "[MOUNT] 4 drives back",
      "[SYNC] Nothing new",
    ],
  },
  {
    title: "UPLOAD // RETRY",
    status: "warn",
    brightness: 1.0,
    pace: "normal",
    lines: [
      "[UPLOAD] notes.md",
      "[WARN] Offline, retrying in 1 min",
      "[NET] Back online",
      "[UPLOAD] notes.md done",
    ],
  },
  {
    title: "DOCTOR // FIX",
    status: "ok",
    brightness: 1.1,
    pace: "fast",
    lines: [
      "> dokky fix",
      "[FIX] Finder names repaired",
      "[FIX] Service restarted",
      "[OK] All good",
    ],
  },
  {
    title: "VAULT // UNLOCK",
    status: "ok",
    brightness: 0.9,
    pace: "normal",
    lines: [
      "> dokky",
      "[VAULT] Master password: ********",
      "[ARGON2] Deriving key",
      "[VAULT] 5 sign-ins unlocked",
      "[OK] Vault ready",
    ],
  },
  {
    title: "DRIVE // MOUNT",
    status: "ok",
    brightness: 1.0,
    pace: "normal",
    lines: ['> dokky mount "Photos S3"', "[MOUNT] Photos S3", "[OK] Mounted"],
  },
  {
    title: "SIGN-IN // EXPIRED",
    status: "error",
    brightness: 0.9,
    pace: "slow",
    lines: [
      "[AUTH] Dropbox",
      "[ERR] Sign-in expired",
      "[TUI] Drives > Dropbox > Sign in",
      "[OK] Signed in again",
    ],
  },
  {
    title: "TRANSFER // STREAM",
    status: "ok",
    brightness: 1.1,
    pace: "fast",
    lines: [
      "[VFS] trip.mov opened",
      "[READ] Streaming 2.1 GB",
      "[READ] 64 MB ahead",
      "[OK] Playing",
    ],
  },
  {
    title: "DEVICE // ADD",
    status: "ok",
    brightness: 0.8,
    pace: "slow",
    lines: [
      "> dokky login",
      "[AUTH] Approve in your browser",
      "[AUTH] mac-mini approved",
      "[SYNC] 4 drives ready",
    ],
  },
];

export function AnimatedTerminalGrid({ cards }: { cards: TerminalCardDef[] }) {
  const gridRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const grid = gridRef.current;
    if (!grid) {
      return;
    }

    const terminals = grid.querySelectorAll<HTMLElement>("[data-terminal]");
    if (prefersStill()) {
      for (const el of grid.querySelectorAll<HTMLElement>("[data-line]")) {
        el.style.visibility = "visible";
      }
      return;
    }
    const visible = new Int32Array(cards.length);
    const BLANK = 300;
    let rafId: number | null = null;
    let active = true;
    const start = performance.now();

    // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: ported frame loop from forged, one pass over every card
    function tick(now: number) {
      for (let t = 0; t < cards.length; t += 1) {
        const card: TerminalCardDef = cards[t];
        const { lineInterval, holdTime } = PACE_CONFIG[card.pace];
        const delay = t * 120;
        const elapsed = now - start - delay;
        if (elapsed < 0) {
          continue;
        }

        const revealDuration = card.lines.length * lineInterval;
        const cycle = BLANK + revealDuration + holdTime;
        const phase = elapsed % cycle;

        let count: number;
        if (phase < BLANK) {
          count = 0;
        } else if (phase < BLANK + revealDuration) {
          count = Math.floor((phase - BLANK) / lineInterval) + 1;
        } else {
          count = card.lines.length;
        }

        if (count === visible[t]) {
          continue;
        }
        visible[t] = count;

        const el = terminals[t];
        if (!el) {
          continue;
        }

        const lineEls = el.querySelectorAll<HTMLElement>("[data-line]");
        for (let i = 0; i < lineEls.length; i += 1) {
          lineEls[i].style.visibility = i < count ? "visible" : "hidden";
        }

        const dot = el.querySelector<HTMLElement>("[data-dot]");
        if (dot) {
          const streaming = count > 0 && count < card.lines.length;
          const colors = DOT_COLORS[card.status];
          dot.className = `w-1.5 h-1.5 rounded-full ${streaming ? colors.active : colors.idle}`;
        }
      }

      if (active && document.visibilityState === "visible") {
        rafId = requestAnimationFrame(tick);
      }
    }

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting && !rafId) {
          rafId = requestAnimationFrame(tick);
        } else if (!entry.isIntersecting && rafId) {
          cancelAnimationFrame(rafId);
          rafId = null;
        }
      },
      { threshold: 0.1 }
    );
    observer.observe(grid);

    const onVis = () => {
      if (document.visibilityState === "visible" && !rafId) {
        rafId = requestAnimationFrame(tick);
      } else if (document.visibilityState === "hidden" && rafId) {
        cancelAnimationFrame(rafId);
        rafId = null;
      }
    };
    document.addEventListener("visibilitychange", onVis);

    return () => {
      active = false;
      if (rafId) {
        cancelAnimationFrame(rafId);
      }
      observer.disconnect();
      document.removeEventListener("visibilitychange", onVis);
    };
  }, [cards]);

  return (
    <div
      className="grid h-full grid-cols-2 grid-rows-5 gap-px bg-[#1a1a1a] md:grid-cols-3 lg:grid-cols-4"
      ref={gridRef}
    >
      {cards.map((card: TerminalCardDef) => (
        <div
          className="flex flex-col overflow-hidden border border-[#1a1a1a] bg-black"
          data-terminal
          key={card.title}
          style={{ filter: `brightness(${card.brightness})` }}
        >
          <div className="flex h-7 shrink-0 items-center justify-between border-b border-[#1a1a1a] px-3">
            <span className="font-mono text-[10px] tracking-[0.5px] text-[#555]">
              {card.title}
            </span>
            <span
              className={`h-1.5 w-1.5 rounded-full ${DOT_COLORS[card.status].idle}`}
              data-dot
            />
          </div>
          <div className="flex-1 overflow-hidden p-3">
            {card.lines.map((line: string) => (
              <div
                className="truncate font-mono text-[11px] leading-[1.7] whitespace-pre"
                data-line
                key={line}
                style={{ visibility: "hidden", color: lineColor(line) }}
              >
                {line}
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

const GLITCH_CHARS = "!@#$%^&*0123456789";
const GLITCH_DURATION = 400;

function useGlitchText(text: string) {
  const [display, setDisplay] = useState(text);
  const animating = useRef<boolean>(false);

  useEffect(() => {
    setDisplay(text);
  }, [text]);

  const scramble = useCallback(() => {
    if (animating.current) {
      return;
    }
    const still =
      window.matchMedia("(prefers-reduced-motion: reduce)").matches ||
      window.matchMedia("(max-width: 768px)").matches;
    if (still) {
      return;
    }
    animating.current = true;

    const chars = text.split("");
    const rand = () =>
      GLITCH_CHARS[Math.floor(Math.random() * GLITCH_CHARS.length)];
    setDisplay(chars.map((c) => (c === " " ? " " : rand())).join(""));

    const start = performance.now();
    const tick = (now: number) => {
      const progress = Math.min(1, (now - start) / GLITCH_DURATION);
      const resolved = Math.floor(progress * chars.length);
      setDisplay(
        chars
          .map((c, i) => {
            if (c === " ") {
              return " ";
            }
            if (i < resolved) {
              return c;
            }
            return rand();
          })
          .join("")
      );
      if (progress < 1) {
        requestAnimationFrame(tick);
      } else {
        setDisplay(text);
        animating.current = false;
      }
    };
    requestAnimationFrame(tick);
  }, [text]);

  return { display, scramble };
}

type GlitchButtonProps = {
  children: string;
  variant?: "primary" | "secondary";
  href?: string;
  external?: boolean;
  type?: "button" | "submit";
} & Omit<AnchorHTMLAttributes<HTMLAnchorElement>, "children" | "href" | "type">;

export function GlitchButton({
  children,
  variant = "primary",
  href,
  external,
  className = "",
  type = "button",
  ...rest
}: GlitchButtonProps) {
  const { display, scramble } = useGlitchText(children);

  const isPrimary = variant === "primary";
  const base =
    "group relative inline-flex items-center justify-center gap-2 font-mono text-sm font-bold tracking-wider uppercase overflow-hidden transition-colors active:scale-[0.97]";
  const style = isPrimary
    ? "bg-white text-black hover:bg-zinc-200"
    : "bg-transparent text-white border border-[#27272a] hover:border-[#22b8cc] hover:text-[#22b8cc]";
  const overlayOpacity = isPrimary
    ? "group-hover:opacity-[0.12]"
    : "group-hover:opacity-[0.30]";

  const inner = (
    <>
      <span className="relative z-[2]">{display}</span>
      <div
        aria-hidden
        className={`pointer-events-none absolute inset-0 z-[1] opacity-0 ${overlayOpacity} transition-opacity duration-200`}
        style={{
          background: [
            "repeating-linear-gradient(135deg, transparent, transparent 4px, currentColor 4px, currentColor 5px)",
            "repeating-linear-gradient(45deg, transparent, transparent 4px, currentColor 4px, currentColor 5px)",
          ].join(", "),
          backgroundSize: "8px 8px",
        }}
      />
    </>
  );

  const classes = `${base} ${style} ${className}`;

  if (external && href) {
    return (
      <a className={classes} href={href} onMouseEnter={scramble} {...rest}>
        {inner}
      </a>
    );
  }

  if (href) {
    return (
      <Link className={classes} href={href} onMouseEnter={scramble} {...rest}>
        {inner}
      </Link>
    );
  }

  return (
    <button
      className={classes}
      onMouseEnter={scramble}
      {...(rest as React.ButtonHTMLAttributes<HTMLButtonElement>)}
      type={type}
    >
      {inner}
    </button>
  );
}

export interface TerminalStep {
  command: string;
  output: string[];
  pauseAfter?: number;
}

const HEADER_RE = /^NAME\s+STATE/u;
const MAX_LINES = 60;

interface TermLine {
  id: string;
  text: string;
  ts: string;
  type: "cmd" | "out";
}

function TerminalOutputLine({ text }: { text: string }) {
  if (HEADER_RE.test(text) || text.startsWith("dokky daemon")) {
    return <span className="text-[#52525b]">{text}</span>;
  }
  if (text.includes(" is mounted at ")) {
    return <span className="text-[#6ed69a]">{text}</span>;
  }
  if (text.startsWith("Free space")) {
    return <span className="text-[#52525b]">{text}</span>;
  }
  return <span className="text-[#a1a1aa]">{text}</span>;
}

export function AnimatedBigTerminal({ steps }: { steps: TerminalStep[] }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const onScreen = useRef(false);
  const still = useReducedMotion() === true;

  useEffect(() => {
    const el = containerRef.current;
    if (!el) {
      return;
    }
    const io = new IntersectionObserver(([entry]) => {
      onScreen.current = entry.isIntersecting;
    });
    io.observe(el);
    return () => io.disconnect();
  }, []);
  const [lines, setLines] = useState<TermLine[]>([]);
  const [typing, setTyping] = useState("");
  const [cursorOn, setCursorOn] = useState(true);
  const tsRef = useRef({ base: Date.now(), offset: 0 });

  useEffect(() => {
    if (prefersStill()) {
      return;
    }
    const id = setInterval(() => setCursorOn((v) => !v), 530);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    let alive = true;
    const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

    const ts = () => {
      const t = tsRef.current;
      const d = new Date(t.base + t.offset);
      t.offset += 20 + Math.floor(Math.random() * 180);
      const hh = d.getHours().toString().padStart(2, "0");
      const mm = d.getMinutes().toString().padStart(2, "0");
      const ss = d.getSeconds().toString().padStart(2, "0");
      const ms = d.getMilliseconds().toString().padStart(3, "0");
      return `${hh}:${mm}:${ss}.${ms}`;
    };

    const charDelay = (ch: string, prev: string): number => {
      if (ch === " ") {
        return 15 + Math.random() * 12;
      }
      if ("\"'*@".includes(ch)) {
        return 60 + Math.random() * 30;
      }
      if (".-/~".includes(ch)) {
        return 28 + Math.random() * 14;
      }
      if (prev === " " && Math.random() < 0.12) {
        return 130 + Math.random() * 90;
      }
      return 25 + Math.random() * 28;
    };

    const add = (entry: TermLine) =>
      setLines((p) => [...p, entry].slice(-MAX_LINES));
    const shown = () => !document.hidden && onScreen.current;
    const whileHidden = async () => {
      while (alive && !shown()) {
        // biome-ignore lint/performance/noAwaitInLoops: polls until the terminal is visible again
        await sleep(800);
      }
    };

    async function typeCommand(command: string) {
      let typed = "";
      let prev = "";
      for (const ch of command) {
        if (!alive) {
          return false;
        }
        typed += ch;
        setTyping(typed);
        // biome-ignore lint/performance/noAwaitInLoops: each key waits for the one before it
        await sleep(charDelay(ch, prev));
        prev = ch;
      }
      return alive;
    }

    async function printOutput(output: string[], at: number) {
      for (const [i, text] of output.entries()) {
        if (!alive) {
          return false;
        }
        add({ id: `o${at}-${i}-${Date.now()}`, text, type: "out", ts: ts() });
        const pause = i === 0 && output.length > 3 ? 20 : 45;
        // biome-ignore lint/performance/noAwaitInLoops: lines print one after another
        await sleep(pause + Math.random() * 25);
      }
      return alive;
    }

    async function play(at: number) {
      await whileHidden();
      const s = steps[at];
      if (!(await typeCommand(s.command))) {
        return;
      }
      add({
        id: `c${at}-${Date.now()}`,
        text: s.command,
        type: "cmd",
        ts: ts(),
      });
      setTyping("");
      await sleep(160 + Math.random() * 120);
      if (!(await printOutput(s.output, at))) {
        return;
      }
      add({ id: `g${at}-${Date.now()}`, text: "\u00A0", type: "out", ts: "" });
      await sleep(s.pauseAfter ?? 2500);
      if (alive) {
        play((at + 1) % steps.length);
      }
    }

    async function animate() {
      if (prefersStill()) {
        setLines(
          steps.flatMap((st, at) => [
            { id: `c${at}`, text: st.command, type: "cmd" as const, ts: "" },
            ...st.output.map((text, i) => ({
              id: `o${at}-${i}`,
              text,
              type: "out" as const,
              ts: "",
            })),
            { id: `g${at}`, text: "\u00A0", type: "out" as const, ts: "" },
          ])
        );
        return;
      }
      await sleep(700);
      play(0);
    }

    animate();
    return () => {
      alive = false;
    };
  }, [steps]);

  // biome-ignore lint/correctness/useExhaustiveDependencies: containerRef is a stable ref, intentionally omitted
  useEffect(() => {
    containerRef.current?.scrollTo({
      top: containerRef.current.scrollHeight,
      behavior: "smooth",
    });
  }, [lines, typing]);

  return (
    <div className="relative flex h-full w-full flex-col overflow-hidden">
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_at_top,_rgba(34,184,204,0.04)_0%,_transparent_55%)]" />

      <div
        className="relative z-10 flex-1 overflow-x-auto overflow-y-hidden p-5 font-mono text-[11px] leading-[1.9] md:px-6 md:py-5 md:text-[13px] [&::-webkit-scrollbar]:hidden"
        ref={containerRef}
        style={{ tabSize: 8 }}
      >
        <AnimatePresence initial={false}>
          {lines.map((line) => (
            <motion.div
              animate={{ opacity: 1, y: 0 }}
              className="flex w-max min-w-full whitespace-pre"
              initial={{ opacity: 0, y: 5 }}
              key={line.id}
              transition={
                still ? { duration: 0 } : { duration: 0.12, ease: "easeOut" }
              }
            >
              {line.ts ? (
                <span className="mr-4 hidden shrink-0 text-[#1e1e21] select-none lg:inline">
                  {line.ts}
                </span>
              ) : null}
              <span className="flex-1">
                {line.type === "cmd" ? (
                  <>
                    <span className="text-[#22b8cc] select-none">$ </span>
                    <span className="text-white">{line.text}</span>
                  </>
                ) : (
                  <TerminalOutputLine text={line.text} />
                )}
              </span>
            </motion.div>
          ))}
        </AnimatePresence>

        <div className="flex w-max min-w-full whitespace-pre">
          <span className="mr-4 hidden shrink-0 text-[#1e1e21] select-none lg:inline">
            &nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;
          </span>
          <span className="flex-1">
            <span className="text-[#22b8cc] select-none">$ </span>
            <span className="text-white">{typing}</span>
            <span
              className="ml-px inline-block h-[14px] w-[7px] translate-y-[2px] transition-opacity duration-75"
              style={{
                backgroundColor: cursorOn ? "#22b8cc" : "transparent",
                boxShadow: cursorOn ? "0 0 8px rgba(34,184,204,0.5)" : "none",
              }}
            />
          </span>
        </div>
      </div>
    </div>
  );
}

const SYNC_BARS = Array.from({ length: 24 }, (_, i) => `sync-${i}`);

export function TopologyVisualizer() {
  const still = useReducedMotion() === true;
  return (
    <div className="relative mt-16 w-full md:mt-24">
      <div
        className="pointer-events-none absolute inset-0 opacity-[0.03]"
        style={{
          backgroundImage: "radial-gradient(#22b8cc 1px, transparent 1px)",
          backgroundSize: "32px 32px",
        }}
      />

      <div className="relative z-10 mb-8 grid w-full grid-cols-1 gap-8 md:mb-12 md:grid-cols-12">
        <motion.div
          className="group relative flex min-h-[280px] w-full flex-col border border-[#27272a] bg-[#050505] p-6 shadow-2xl transition-colors hover:border-[#6ed69a]/30 md:col-span-5"
          initial={{ opacity: 0, y: 20 }}
          viewport={{ once: true }}
          whileInView={{ opacity: 1, y: 0 }}
        >
          <div className="mb-6 flex items-center justify-between border-b border-[#27272a] pb-4">
            <div className="flex items-center gap-3">
              <span className="font-mono text-[10px] tracking-[0.2em] text-[#a1a1aa]">
                01 {"// "}
              </span>
              <span className="font-mono text-[11px] font-bold tracking-widest text-white uppercase">
                Native Drives
              </span>
            </div>
            <div className="h-1.5 w-1.5 animate-pulse rounded-full bg-[#6ed69a] shadow-[0_0_8px_#6ed69a]" />
          </div>
          <p className="mb-6 flex-1 text-sm leading-relaxed text-[#a1a1aa]">
            Every drive mounts the way your system expects: a local network
            share on macOS with no kernel extension, WinFsp on Windows and FUSE
            on Linux.
          </p>
          <div className="mt-auto flex h-16 w-full shrink-0 items-center justify-between overflow-hidden border border-[#18181b] bg-black px-4">
            <span className="flex shrink-0 items-center gap-2 truncate font-mono text-[10px] tracking-widest text-[#6ed69a]">
              <span className="h-1.5 w-1.5 shrink-0 animate-pulse rounded-full bg-[#6ed69a] shadow-[0_0_8px_#6ed69a]" />
              <span className="hidden font-bold sm:inline">MOUNTED</span>
            </span>
            <span className="ml-4 shrink-0 truncate font-mono text-[10px] tracking-widest text-[#a1a1aa] opacity-60">
              ~/dokky/Home NAS
            </span>
          </div>
        </motion.div>

        <motion.div
          className="group relative flex min-h-[280px] w-full flex-col border border-[#27272a] bg-[#050505] p-6 shadow-2xl transition-colors hover:border-[#22b8cc]/30 md:col-span-7"
          initial={{ opacity: 0, y: 20 }}
          transition={{ delay: 0.1 }}
          viewport={{ once: true }}
          whileInView={{ opacity: 1, y: 0 }}
        >
          <div className="mb-6 flex items-center justify-between border-b border-[#27272a] pb-4">
            <div className="flex items-center gap-3">
              <span className="font-mono text-[10px] tracking-[0.2em] text-[#a1a1aa]">
                02 {"// "}
              </span>
              <span className="font-mono text-[11px] font-bold tracking-widest text-white uppercase">
                Sync Service
              </span>
            </div>
            <div className="h-1.5 w-1.5 animate-pulse rounded-full bg-[#22b8cc] shadow-[0_0_8px_#22b8cc]" />
          </div>
          <p className="mb-6 flex-1 text-sm leading-relaxed text-[#a1a1aa]">
            Keeps your drive list and sign-ins the same on every device.
            Sign-ins are encrypted on your computer first, so the server only
            passes along blobs it can't read. It never sees your files.
          </p>
          <div className="mt-auto flex h-16 w-full shrink-0 items-center justify-between overflow-hidden border border-[#18181b] bg-black px-4">
            <span className="z-10 shrink-0 truncate font-mono text-[10px] tracking-widest text-[#a1a1aa]">
              dokky.ritik.me
            </span>

            <div className="mx-4 flex flex-1 justify-center gap-1 md:mx-6 lg:gap-1.5">
              {SYNC_BARS.map((bar, i) => (
                <motion.div
                  animate={
                    still ? { opacity: 0.6 } : { opacity: [0.15, 1, 0.15] }
                  }
                  className="h-[2px] flex-1 rounded-full bg-[#22b8cc] shadow-[0_0_5px_rgba(34,184,204,0.5)]"
                  key={bar}
                  transition={{
                    repeat: Number.POSITIVE_INFINITY,
                    duration: 1.5,
                    delay: i * 0.05,
                    ease: "linear",
                  }}
                />
              ))}
            </div>

            <span className="z-10 shrink-0 font-mono text-[10px] font-bold tracking-widest text-[#22b8cc] uppercase">
              2 KB/S
            </span>
          </div>
        </motion.div>
        <motion.div
          className="group relative flex min-h-[280px] w-full flex-col border border-[#27272a] bg-[#050505] p-6 shadow-2xl transition-colors hover:border-[#6ed69a]/30 md:col-span-7"
          initial={{ opacity: 0, y: 20 }}
          transition={{ delay: 0.2 }}
          viewport={{ once: true }}
          whileInView={{ opacity: 1, y: 0 }}
        >
          <div className="mb-6 flex items-center justify-between border-b border-[#27272a] pb-4">
            <div className="flex items-center gap-3">
              <span className="font-mono text-[10px] tracking-[0.2em] text-[#a1a1aa]">
                03 {"// "}
              </span>
              <span className="font-mono text-[11px] font-bold tracking-widest text-white uppercase">
                Transfer Engine
              </span>
            </div>
            <div className="h-1.5 w-1.5 animate-pulse rounded-full bg-[#6ed69a] shadow-[0_0_8px_#6ed69a]" />
          </div>
          <p className="mb-6 flex-1 text-sm leading-relaxed text-[#a1a1aa]">
            A smart cache opens recent files instantly and streams big ones.
            Uploads go straight from your computer to your storage and always
            finish before a drive goes away.
          </p>

          <div className="mt-auto flex h-16 w-full shrink-0 items-center justify-between overflow-hidden border border-[#18181b] bg-black px-4">
            <span className="max-w-[80px] shrink-0 truncate font-mono text-[10px] tracking-widest text-[#a1a1aa] sm:max-w-none">
              trip.mov
            </span>

            <div className="mx-4 flex min-w-0 flex-1 items-center">
              <div className="relative isolate h-[2px] flex-1 overflow-hidden rounded-l-full bg-[#27272a]">
                <motion.div
                  animate={still ? { left: "-100%" } : { left: "200%" }}
                  className="absolute inset-y-0 w-12 bg-gradient-to-r from-transparent via-[#6ed69a] to-transparent"
                  initial={{ left: "-100%" }}
                  transition={{
                    repeat: Number.POSITIVE_INFINITY,
                    duration: 1.5,
                    ease: "linear",
                  }}
                />
              </div>

              <div className="mx-2 flex shrink-0 items-center gap-1.5 bg-[#6ed69a]/10 px-2 py-0.5 shadow-[0_0_10px_rgba(110,214,154,0.1)]">
                <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-[#6ed69a]" />
                <span className="hidden font-mono text-[9px] font-bold tracking-widest text-[#6ed69a] sm:block">
                  DIRECT
                </span>
              </div>

              <div className="relative isolate h-[2px] flex-1 overflow-hidden rounded-r-full bg-[#27272a]">
                <motion.div
                  animate={still ? { left: "-100%" } : { left: "200%" }}
                  className="absolute inset-y-0 w-12 bg-gradient-to-r from-transparent via-[#22b8cc] to-transparent"
                  initial={{ left: "-100%" }}
                  transition={{
                    repeat: Number.POSITIVE_INFINITY,
                    duration: 1.5,
                    ease: "linear",
                    delay: 0.75,
                  }}
                />
              </div>
            </div>

            <span className="max-w-[80px] shrink-0 truncate text-right font-mono text-[10px] font-bold tracking-widest text-[#22b8cc] sm:max-w-none">
              Photos S3
            </span>
          </div>
        </motion.div>

        <motion.div
          className="group relative flex min-h-[280px] w-full flex-col border border-[#27272a] bg-[#050505] p-6 shadow-2xl transition-colors hover:border-[#22b8cc]/30 md:col-span-5"
          initial={{ opacity: 0, y: 20 }}
          transition={{ delay: 0.3 }}
          viewport={{ once: true }}
          whileInView={{ opacity: 1, y: 0 }}
        >
          <div className="mb-6 flex items-center justify-between border-b border-[#27272a] pb-4">
            <div className="flex items-center gap-3">
              <span className="font-mono text-[10px] tracking-[0.2em] text-[#a1a1aa]">
                04 {"// "}
              </span>
              <span className="font-mono text-[11px] font-bold tracking-widest text-white uppercase">
                Sign-in Vault
              </span>
            </div>
            <div className="h-1.5 w-1.5 animate-pulse rounded-full bg-[#22b8cc] shadow-[0_0_8px_#22b8cc]" />
          </div>

          <p className="mb-6 flex-1 text-sm leading-relaxed text-[#a1a1aa]">
            Saved sign-ins are sealed on your devices with a key only your
            master password or recovery key can open. Unlock with Touch ID,
            Windows Hello or your system password.
          </p>

          <div className="mt-auto flex h-16 w-full shrink-0 items-center justify-between overflow-hidden border border-[#18181b] bg-black px-4">
            <span className="flex shrink-0 items-center gap-2 truncate font-mono text-[10px] tracking-widest text-[#22b8cc]">
              <span className="h-1.5 w-1.5 shrink-0 animate-pulse rounded-full bg-[#22b8cc] shadow-[0_0_8px_#22b8cc]" />
              <span className="hidden font-bold sm:inline">SEALED</span>
            </span>

            <div
              className="isolate ml-4 flex h-full flex-1 flex-col justify-center overflow-hidden pl-4 text-right"
              style={{
                WebkitMaskImage:
                  "linear-gradient(to right, transparent 0%, black 20%)",
                maskImage:
                  "linear-gradient(to right, transparent 0%, black 20%)",
              }}
            >
              <motion.div
                animate={still ? { x: "0%" } : { x: ["0%", "-50%"] }}
                className="inline-flex gap-4 font-mono text-[10px] tracking-widest whitespace-nowrap text-[#3f3f46]"
                transition={{
                  repeat: Number.POSITIVE_INFINITY,
                  duration: 25,
                  ease: "linear",
                }}
              >
                <span>
                  9f2c41d07ab3e85c6d19f04b72ae3c5810dd6b94e27f0c3a51b8e6f2047d9ac3
                </span>
                <span>
                  9f2c41d07ab3e85c6d19f04b72ae3c5810dd6b94e27f0c3a51b8e6f2047d9ac3
                </span>
                <span>
                  9f2c41d07ab3e85c6d19f04b72ae3c5810dd6b94e27f0c3a51b8e6f2047d9ac3
                </span>
                <span>
                  9f2c41d07ab3e85c6d19f04b72ae3c5810dd6b94e27f0c3a51b8e6f2047d9ac3
                </span>
              </motion.div>
            </div>
          </div>
        </motion.div>
      </div>
    </div>
  );
}
