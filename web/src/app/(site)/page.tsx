import Link from "next/link";
import type { TerminalStep } from "@/components/landing/client";
import {
  AnimatedBigTerminal,
  AnimatedTerminalGrid,
  GlitchButton,
  ScrollReveal,
  TERMINAL_CARDS,
  TopologyVisualizer,
} from "@/components/landing/client";
import { Footer } from "@/components/landing/footer";
import { InstallCommand } from "@/components/landing/install-command";
import { Caustics, Waterline } from "@/components/landing/water";

const STATUS_DEMO: TerminalStep[] = [
  {
    command: "dokky status",
    output: [
      "dokky daemon 0.1.24",
      "",
      "NAME           STATE      MOUNT POINT                       CACHE    UPLOADS",
      "Google Drive   mounted    /Users/ritik/dokky/Google Drive   640 MiB  0 queued, 0 uploading",
      "Home NAS       mounted    /Users/ritik/dokky/Home NAS       312 MiB  0 queued, 0 uploading",
      "Work OneDrive  mounted    /Users/ritik/dokky/Work OneDrive  128 MiB  1 queued, 1 uploading",
      "Photos S3      unmounted  /Users/ritik/dokky/Photos S3      0 B      0 queued, 0 uploading",
      "",
      "Free space for the cache: 212 GiB",
    ],
    pauseAfter: 2600,
  },
  {
    command: "ls ~/dokky",
    output: ["Google Drive    Home NAS    Photos S3    Work OneDrive"],
    pauseAfter: 1800,
  },
  {
    command: "cp ~/Movies/trip.mov ~/dokky/Work\\ OneDrive/",
    output: [],
    pauseAfter: 1400,
  },
  {
    command: 'dokky mount "Photos S3"',
    output: ["Photos S3 is mounted at /Users/ritik/dokky/Photos S3"],
    pauseAfter: 3200,
  },
];

function Hero() {
  return (
    <section className="relative flex h-dvh flex-col border-b border-[#27272a]">
      <section className="relative flex-1 overflow-hidden">
        <div className="pointer-events-none absolute inset-0 overflow-hidden select-none">
          <div className="h-full">
            <div className="relative h-full">
              <AnimatedTerminalGrid cards={TERMINAL_CARDS} />
              <Caustics />
              <div className="pointer-events-none absolute right-0 bottom-0 left-0 h-[40%] bg-gradient-to-b from-transparent to-black" />
              <div className="pointer-events-none absolute inset-0 bg-gradient-to-r from-black via-black/80 via-40% to-transparent" />
              <div className="pointer-events-none absolute top-0 right-0 left-0 h-20 bg-gradient-to-b from-black to-transparent" />
            </div>
          </div>
        </div>

        <div className="relative z-10 flex h-full flex-col justify-center px-6 lg:px-16">
          <div className="max-w-4xl">
            <div className="animate-slide-up">
              <div className="inline-flex items-center gap-2 bg-[#22b8cc] px-3 py-1.5 font-mono text-[11px] font-bold tracking-[0.15em] text-black uppercase">
                Cloud is a mess
              </div>
            </div>

            <h1 className="animate-slide-up anim-delay-100 mt-8 text-[clamp(48px,8vw,96px)] leading-[0.9] font-bold tracking-[-0.035em] text-white">
              Your Drives, <br />
              One Dock, <br />
              Every Machine.
            </h1>

            <p className="animate-slide-up anim-delay-200 mt-8 mb-12 max-w-2xl text-lg leading-relaxed text-[#a1a1aa] md:text-xl">
              Mount Google Drive, OneDrive, S3, your NAS and 50+ more as native
              drives, the same on every computer. One command to install. One
              app to run. You never juggle sync clients again.
            </p>

            <div className="animate-slide-up anim-delay-300 flex flex-col items-start gap-4 sm:flex-row sm:items-center">
              <GlitchButton className="h-12 px-8" href="/docs">
                Get started
              </GlitchButton>
              <InstallCommand />
            </div>
          </div>
        </div>
      </section>
    </section>
  );
}

function GridFeatures() {
  const features = [
    {
      icon: "M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z M3 11h18",
      title: "Native Drives",
      subtitle: "Finder, Explorer and your terminal",
      desc: "Each drive shows up next to your disks. Open, save and drag files with the apps you already use, nothing new to learn.",
      bullets: [
        "macOS without kernel extensions",
        "Windows through WinFsp",
        "Linux through FUSE 3",
      ],
      cta: "How mounting works",
      href: "/docs#mount",
    },
    {
      icon: "M7 18a4 4 0 0 1-.6-7.96A6 6 0 0 1 18 9a4.5 4.5 0 0 1-.5 9z",
      title: "50+ Services",
      subtitle: "Sign in once per account",
      desc: "Google Drive, OneDrive, Dropbox, iCloud Drive, S3, MEGA, your NAS over SFTP or WebDAV, and many more.",
      bullets: [
        "Many drives from one account",
        "Part of an account as its own drive",
        "Sign-in problems shown, never silent",
      ],
      cta: "Add a drive",
      href: "/docs#add-drive",
    },
    {
      icon: "M4 5h12v9H4z M2 18h16 M18 9h4v10h-4z",
      title: "Every Device",
      subtitle: "Add once, ready everywhere",
      desc: "Add a drive on one computer and it is waiting on all of them. Mount it only where you want it.",
      bullets: [
        "Drive list synced to every device",
        "Mount and unmount per device",
        "Remove once, gone everywhere",
      ],
      cta: "Your devices",
      href: "/docs#devices",
    },
    {
      icon: "M13 2 4 14h7l-1 8 9-12h-7z",
      title: "Smart Cache",
      subtitle: "Files open instantly",
      desc: "Recent files open from a local cache, and big videos stream without a full download first.",
      bullets: [
        "Cache size and age per device",
        "Streams large files on demand",
        "Clears what you stop using",
      ],
      cta: "Cache settings",
      href: "/docs#cache",
    },
    {
      icon: "M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z M9 12l2 2 4-4",
      title: "Safe Uploads",
      subtitle: "Nothing lost on eject",
      desc: "Every upload finishes before a drive is ejected, removed or its computer signs out, even when the network drops.",
      bullets: [
        "Waits for uploads, never forever",
        "Retries when the network drops",
        "Stuck uploads shown, then let go",
      ],
      cta: "How uploads work",
      href: "/docs#cache",
    },
    {
      icon: "M3 12h4l3-8 4 16 3-8h4",
      title: "Health & Activity",
      subtitle: "Know what happened, everywhere",
      desc: "One log for every change on every device, with clear status for each drive and one-key repairs.",
      bullets: [
        "Activity across all devices",
        "Health checks with Fix all",
        "Drives come back after sleep",
      ],
      cta: "Health and fixes",
      href: "/docs#health",
    },
  ];

  return (
    <section className="relative overflow-hidden border-t border-white/10 bg-black px-6 py-36 lg:px-16">
      <div
        className="pointer-events-none absolute inset-0 opacity-[0.04]"
        style={{
          background:
            "repeating-linear-gradient(135deg,transparent,transparent 4px,rgba(255,255,255,0.5) 4px,rgba(255,255,255,0.5) 5px)",
        }}
      />

      <div className="relative z-10 w-full">
        <ScrollReveal className="mb-4">
          <div className="flex items-center gap-2.5">
            <span className="h-2 w-2 bg-[#22b8cc]" />
            <span className="font-mono text-[10px] tracking-[0.2em] text-[#a1a1aa] uppercase">
              The storage layer
            </span>
          </div>
        </ScrollReveal>

        <ScrollReveal>
          <h2 className="text-4xl leading-[0.95] font-bold tracking-tighter text-pretty text-white sm:text-5xl lg:text-7xl xl:text-8xl">
            Every cloud you use, as a drive you already know.
          </h2>
        </ScrollReveal>

        <ScrollReveal className="mt-6">
          <p className="max-w-2xl text-base leading-relaxed text-[#a1a1aa] lg:text-lg">
            One app, six worries gone. Mount, cache, sync, upload, remove and
            repair your drives from a single background app, while you get on
            with your work.
          </p>
        </ScrollReveal>

        <div className="relative z-10 mt-16 grid grid-cols-1 border-t border-l border-white/10 md:grid-cols-2 lg:grid-cols-3">
          {features.map((f) => (
            <article
              className="group relative flex cursor-pointer flex-col border-r border-b border-white/10 bg-white/[0.03] transition-colors duration-300 hover:border-[#22b8cc]/20"
              key={f.title}
            >
              <div className="pointer-events-none absolute inset-0 bg-[#22b8cc]/[0.07] opacity-0 transition-opacity duration-300 group-hover:opacity-100" />

              <div className="relative flex items-center gap-3.5 px-6 pt-6 pb-3">
                <div className="flex h-9 w-9 items-center justify-center border border-white/10 bg-black text-white/70 transition-colors duration-300 group-hover:border-[#22b8cc]/40 group-hover:text-[#22b8cc]">
                  <svg
                    aria-label={f.title}
                    fill="none"
                    height="20"
                    role="img"
                    stroke="currentColor"
                    strokeWidth="1.5"
                    viewBox="0 0 24 24"
                    width="20"
                  >
                    <path d={f.icon} />
                  </svg>
                </div>
                <div>
                  <h3 className="text-sm font-semibold tracking-tight text-white transition-colors duration-300 group-hover:text-[#22b8cc]">
                    {f.title}
                  </h3>
                  <p className="text-xs text-[#a1a1aa] transition-colors duration-300 group-hover:text-[#22b8cc]/60">
                    {f.subtitle}
                  </p>
                </div>
              </div>

              <p className="relative px-6 text-sm leading-relaxed text-white transition-colors duration-300 group-hover:text-[#7fd6e2]">
                {f.desc}
              </p>

              <ul className="relative flex-1 space-y-2 px-6 pt-4 pb-2">
                {f.bullets.map((b) => (
                  <li
                    className="flex items-start gap-2.5 text-xs text-white transition-colors duration-300 group-hover:text-[#7fd6e2]"
                    key={b}
                  >
                    <span className="mt-1.5 h-1 w-1 shrink-0 bg-[#22b8cc]/80" />
                    {b}
                  </li>
                ))}
              </ul>

              <div className="relative mt-auto px-6 pt-3 pb-6">
                <Link
                  className="inline-flex items-center gap-2 bg-[length:0%_1px] bg-[position:0%_100%] bg-[linear-gradient(to_right,rgba(34,184,204,0.4),rgba(34,184,204,0.4))] bg-no-repeat text-xs font-medium text-white transition-all duration-500 ease-in-out group-hover:bg-[length:100%_1px] group-hover:text-[#22b8cc]"
                  href={f.href}
                >
                  {f.cta}
                  <svg
                    aria-label="Arrow right"
                    className="opacity-0 transition-opacity group-hover:opacity-100"
                    fill="none"
                    height="14"
                    role="img"
                    stroke="currentColor"
                    strokeWidth="2"
                    viewBox="0 0 24 24"
                    width="14"
                  >
                    <line x1="5" x2="19" y1="12" y2="12" />
                    <polyline points="12 5 19 12 12 19" />
                  </svg>
                </Link>
              </div>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}

function TerminalSection() {
  return (
    <section className="relative overflow-hidden border-t border-white/10 bg-black py-24 lg:py-36">
      <div className="relative z-10 flex w-full flex-col px-6 lg:px-16">
        <ScrollReveal className="mb-12 flex w-full max-w-3xl flex-col items-start text-left">
          <div className="mb-4 flex items-center gap-2.5">
            <span className="h-2 w-2 bg-[#22b8cc]" />
            <span className="font-mono text-[10px] tracking-[0.2em] text-[#a1a1aa] uppercase">
              One command
            </span>
          </div>
          <h2 className="mb-6 text-5xl leading-[0.95] font-bold tracking-tighter text-white sm:text-6xl lg:text-7xl">
            Your clouds, from the terminal.
          </h2>
          <p className="mb-10 max-w-2xl text-lg leading-relaxed text-[#a1a1aa] lg:text-xl">
            Run dokky for a full-screen app, or script it. Drives mount at login
            and come back on their own after sleep, Wi-Fi changes and updates.
          </p>
          <div className="flex flex-wrap items-center gap-4">
            <GlitchButton className="h-12 px-8 text-sm" href="/docs">
              Get started
            </GlitchButton>
            <GlitchButton
              className="h-12 px-8 text-sm"
              external
              href="https://github.com/itzzritik/Dokky"
              variant="secondary"
            >
              View on GitHub
            </GlitchButton>
          </div>
        </ScrollReveal>

        <ScrollReveal className="w-full" delay={0.2}>
          <div className="group relative flex h-[500px] flex-col overflow-hidden border border-[#27272a] bg-[#050505] p-2 shadow-2xl md:h-[600px]">
            <div className="relative flex h-full w-full flex-col border border-[#18181b] bg-black">
              <div className="z-20 flex h-12 shrink-0 items-center justify-between border-b border-[#18181b] bg-[#030303] px-4">
                <div className="flex items-center gap-4">
                  <div className="flex gap-2">
                    <div className="h-3 w-3 rounded-full bg-white/10 transition-colors group-hover:bg-[#f2665a]/80" />
                    <div className="h-3 w-3 rounded-full bg-white/10 transition-colors group-hover:bg-[#f2b544]/80" />
                    <div className="h-3 w-3 rounded-full bg-white/10 transition-colors group-hover:bg-[#6ed69a]/80" />
                  </div>

                  <div className="mx-2 h-4 w-px bg-[#27272a]" />
                  <span className="mt-0.5 font-mono text-[11px] tracking-widest text-[#a1a1aa] uppercase">
                    ritik@macbook: ~
                  </span>
                </div>

                <div className="flex items-center gap-4">
                  <div className="flex items-center gap-2 border border-[#6ed69a]/30 bg-[#6ed69a]/10 px-2 py-1">
                    <span className="h-1.5 w-1.5 animate-pulse bg-[#6ed69a]" />
                    <span className="font-mono text-[9px] tracking-widest text-[#6ed69a] uppercase">
                      MOUNTED
                    </span>
                  </div>
                </div>
              </div>

              <div className="relative flex-1 overflow-hidden bg-black">
                <AnimatedBigTerminal steps={STATUS_DEMO} />
              </div>

              <div className="z-20 flex h-8 shrink-0 items-center justify-between border-t border-[#18181b] bg-[#050505] px-4">
                <div className="flex items-center gap-3">
                  <span className="font-mono text-[9px] tracking-widest text-[#a1a1aa] uppercase">
                    CACHE: 1.1 GB
                  </span>
                  <span className="hidden font-mono text-[9px] tracking-widest text-[#a1a1aa] uppercase sm:inline">
                    | UP: 18 MB/S
                  </span>
                </div>
                <div className="flex items-center gap-1">
                  <span className="h-3 w-1.5 bg-[#22b8cc]" />
                  <span className="h-3 w-1.5 bg-[#22b8cc]" />
                  <span className="h-3 w-1.5 bg-[#22b8cc]" />
                  <span className="h-3 w-1.5 bg-[#22b8cc]/30" />
                  <span className="h-3 w-1.5 bg-[#22b8cc]/30" />
                </div>
              </div>
            </div>
          </div>
        </ScrollReveal>
      </div>
    </section>
  );
}

function Architecture() {
  return (
    <section className="relative overflow-hidden border-t border-white/10 bg-black py-36">
      <div
        className="pointer-events-none absolute inset-0 opacity-[0.04]"
        style={{
          background:
            "repeating-linear-gradient(135deg,transparent,transparent 4px,rgba(255,255,255,0.5) 4px,rgba(255,255,255,0.5) 5px)",
        }}
      />

      <div className="relative z-10 w-full px-6 lg:px-16">
        <ScrollReveal className="mb-4">
          <div className="flex items-center gap-2.5">
            <span className="h-2 w-2 bg-[#22b8cc]" />
            <span className="font-mono text-[10px] tracking-[0.2em] text-[#a1a1aa] uppercase">
              Architecture
            </span>
          </div>
        </ScrollReveal>

        <ScrollReveal>
          <h2 className="mb-6 text-4xl leading-[0.95] font-bold tracking-tighter text-pretty text-white sm:text-5xl lg:text-7xl xl:text-8xl">
            Architecture
          </h2>
          <p className="mb-16 max-w-2xl text-base leading-relaxed text-[#a1a1aa] lg:text-lg">
            No Electron. No browser extension. No second copy of your files. A
            small Go app on each computer, talking straight to your storage.
          </p>
        </ScrollReveal>

        <ScrollReveal>
          <TopologyVisualizer />
        </ScrollReveal>
      </div>
    </section>
  );
}

function ZeroKnowledge() {
  const specs = [
    {
      id: "01",
      title: "Encryption",
      value: "XChaCha20",
      desc: "Saved sign-ins and connection details are sealed with XChaCha20-Poly1305 on your device. The server only stores ciphertext.",
    },
    {
      id: "02",
      title: "Derivation",
      value: "Argon2id",
      desc: "Your master password is stretched with Argon2id on your computer. It is never sent to us, so we can't see it or reset it.",
    },
    {
      id: "03",
      title: "Unlock",
      value: "Touch ID",
      desc: "Unlock with Touch ID, Windows Hello or your system password. By default dokky unlocks by itself, so drives still mount at login.",
    },
    {
      id: "04",
      title: "Recovery",
      value: "Recovery key",
      desc: "Forgot your master password? Your recovery key opens your sign-ins too. Only you have it, so keep it somewhere safe.",
    },
    {
      id: "05",
      title: "Files",
      value: "Direct",
      desc: "Uploads and downloads go straight between your computer and your storage. Nothing is copied to dokky.",
    },
    {
      id: "06",
      title: "Auditability",
      value: "Open Source",
      desc: "The app, the mounting layer and the server are public on GitHub. Nothing to take on trust.",
    },
  ];

  return (
    <section className="relative overflow-hidden border-t border-[#27272a] bg-black py-36">
      <div
        className="absolute inset-0 opacity-[0.02]"
        style={{
          backgroundImage:
            "linear-gradient(#fff 1px, transparent 1px), linear-gradient(90deg, #fff 1px, transparent 1px)",
          backgroundSize: "64px 64px",
        }}
      />

      <div className="relative z-10 mx-auto w-full max-w-[1400px] px-6 lg:px-16">
        <ScrollReveal className="mb-4">
          <div className="flex items-center gap-2.5">
            <span className="h-2 w-2 bg-[#22b8cc]" />
            <span className="font-mono text-[10px] tracking-[0.2em] text-[#a1a1aa] uppercase">
              Zero knowledge
            </span>
          </div>
        </ScrollReveal>

        <ScrollReveal>
          <h2 className="mb-6 text-4xl leading-[0.95] font-bold tracking-tighter text-pretty text-white sm:text-5xl lg:text-7xl xl:text-8xl">
            Zero Knowledge.
          </h2>
          <p className="mb-16 max-w-2xl text-base leading-relaxed text-[#a1a1aa] lg:text-lg">
            Your sign-ins are sealed on your own computer before they leave it,
            with a key that only your master password or recovery key can open.
            dokky's server stores ciphertext it can't read, and your files never
            pass through it at all.
          </p>
        </ScrollReveal>

        <div className="grid grid-cols-1 gap-px bg-[#27272a] md:grid-cols-2 lg:grid-cols-3">
          {specs.map((spec, i) => (
            <ScrollReveal
              className="group relative flex flex-col overflow-hidden bg-black p-8"
              delay={i * 0.1}
              key={spec.id}
            >
              <div className="pointer-events-none absolute inset-0 bg-gradient-to-br from-[#22b8cc]/5 to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100" />

              <div className="mb-8 flex items-center justify-between">
                <span className="font-mono text-xs tracking-widest text-[#a1a1aa] uppercase">
                  Spec {"// "}
                  {spec.id}
                </span>
                <svg
                  aria-label="Arrow right"
                  className="text-[#3f3f46] transition-colors duration-500 group-hover:text-[#22b8cc]"
                  fill="none"
                  height="14"
                  role="img"
                  stroke="currentColor"
                  strokeWidth="1.5"
                  viewBox="0 0 24 24"
                  width="14"
                >
                  <path d="M5 12h14" />
                  <path d="M12 5l7 7-7 7" />
                </svg>
              </div>
              <span className="mb-1 font-mono text-sm tracking-widest text-[#22b8cc] uppercase opacity-80">
                {spec.title}
              </span>
              <span className="mb-6 text-3xl font-bold tracking-tight text-white sm:text-4xl">
                {spec.value}
              </span>
              <p className="relative z-10 mt-auto text-sm leading-relaxed text-[#a1a1aa]">
                {spec.desc}
              </p>
            </ScrollReveal>
          ))}
        </div>

        <ScrollReveal
          className="relative mt-8 flex flex-col items-center justify-between gap-8 overflow-hidden border border-[#27272a] bg-[#020202] p-8 md:flex-row md:p-12"
          delay={0.2}
        >
          <div className="animate-scan pointer-events-none absolute inset-0 h-[1px] w-full bg-gradient-to-r from-transparent via-[#22b8cc]/20 to-transparent" />

          <div className="relative z-10 flex-1">
            <h3 className="mb-3 text-2xl font-bold tracking-tight text-white md:text-3xl">
              Private by design. Fully auditable.
            </h3>
            <p className="max-w-2xl text-sm text-[#a1a1aa] md:text-base">
              Read exactly how your sign-ins are encrypted and how your drives
              are mounted, straight from the source.
            </p>
          </div>

          <div className="relative z-10 flex w-full flex-col gap-4 sm:w-auto sm:flex-row">
            <GlitchButton
              className="h-14 px-8 text-sm"
              external
              href="https://github.com/itzzritik/Dokky"
            >
              View source
            </GlitchButton>
          </div>
        </ScrollReveal>
      </div>
    </section>
  );
}

function CTA() {
  return (
    <section className="relative flex flex-col items-center justify-center overflow-hidden border-t border-white/10 bg-black py-36 text-center">
      <Waterline />
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_at_center,_rgba(34,184,204,0.06)_0%,_transparent_60%)]" />
      <div
        className="pointer-events-none absolute inset-0 opacity-[0.04]"
        style={{
          background:
            "repeating-linear-gradient(135deg,transparent,transparent 4px,rgba(255,255,255,0.5) 4px,rgba(255,255,255,0.5) 5px)",
        }}
      />

      <ScrollReveal className="relative z-10 flex w-full max-w-4xl flex-col items-center px-6 lg:px-16">
        <div className="mb-6 flex items-center justify-center gap-2.5">
          <span className="h-2 w-2 animate-pulse bg-[#22b8cc]" />
          <span className="font-mono text-[10px] tracking-[0.2em] text-[#a1a1aa] uppercase">
            Get started
          </span>
        </div>
        <h2 className="mb-8 text-5xl leading-[0.9] font-bold tracking-tighter text-pretty text-white sm:text-7xl lg:text-8xl xl:text-[100px]">
          All your drives.
          <br />
          Always within reach.
        </h2>
        <p className="mb-12 max-w-2xl text-base leading-relaxed text-[#a1a1aa] lg:text-xl">
          Install dokky. Never think about multi-cloud management again.
        </p>
        <div className="flex flex-col flex-wrap items-center justify-center gap-6 sm:flex-row">
          <GlitchButton className="h-14 max-w-full px-12 text-sm" href="/docs">
            Get started
          </GlitchButton>
          <GlitchButton
            className="h-14 max-w-full px-12 text-sm"
            external
            href="https://github.com/itzzritik/Dokky"
            variant="secondary"
          >
            View on GitHub
          </GlitchButton>
        </div>
      </ScrollReveal>
    </section>
  );
}

export default function Home() {
  return (
    <div className="bg-black" data-landing>
      <Hero />
      <GridFeatures />
      <TerminalSection />
      <Architecture />
      <ZeroKnowledge />
      <CTA />
      <Footer />
    </div>
  );
}
