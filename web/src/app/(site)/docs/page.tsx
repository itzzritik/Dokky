import type { Metadata } from "next";
import type { ReactNode } from "react";
import { ScrollReveal } from "@/components/landing/client";
import { DocsTOC, type DocsTOCGroup } from "@/components/landing/docs-toc";
import { Footer } from "@/components/landing/footer";

export const metadata: Metadata = {
  title: "Docs | dokky",
  description:
    "Install dokky, add your drives and keep them running on every computer.",
};

function Code({ children }: { children: string }) {
  return (
    <code className="inline-flex -translate-y-px items-center border border-[#27272a] bg-black px-1.5 py-0.5 align-middle font-mono text-[13px] leading-none text-[#22b8cc] shadow-[4px_4px_0px_rgba(39,39,42,1)]">
      {children}
    </code>
  );
}

function CodeBlock({ title, children }: { title?: string; children: string }) {
  const lines = children.split("\n");
  return (
    <div className="group relative my-8 flex flex-col overflow-hidden border border-[#27272a] bg-black">
      <div className="pointer-events-none absolute inset-0 bg-gradient-to-br from-[#22b8cc]/5 to-transparent opacity-0 transition-opacity duration-500 group-hover:opacity-100" />
      {title ? (
        <div className="flex h-12 items-center justify-between border-b border-[#27272a] bg-[#09090b] px-5">
          <div className="flex items-center gap-3">
            <span className="h-1.5 w-1.5 bg-[#22b8cc]" />
            <span className="font-mono text-[10px] tracking-[0.2em] text-[#a1a1aa] uppercase">
              {title}
            </span>
          </div>
          <span className="font-mono text-[10px] tracking-widest text-[#3f3f46] uppercase select-none">
            Terminal
          </span>
        </div>
      ) : null}
      <div className="relative flex">
        <div className="pointer-events-none hidden w-12 shrink-0 flex-col items-end border-r border-[#27272a] bg-[#09090b] py-5 pr-4 font-mono text-[11px] text-[#3f3f46] select-none sm:flex">
          {Array.from({ length: lines.length }, (_, i) => i + 1).map((n) => (
            <span className="leading-7" key={n}>
              {n}
            </span>
          ))}
        </div>
        <pre className="flex-1 overflow-x-auto p-5 font-mono text-[13px] leading-7 text-white sm:pl-6">
          <code>{children}</code>
        </pre>
      </div>
    </div>
  );
}

function Note({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="relative my-10 overflow-hidden border border-[#27272a] bg-black p-6 shadow-[4px_4px_0px_#22b8cc]">
      <div className="mb-4 flex items-center gap-3 border-b border-[#27272a] pb-4">
        <span className="h-1.5 w-1.5 bg-[#22b8cc]" />
        <span className="font-mono text-[11px] font-bold tracking-[0.2em] text-[#22b8cc] uppercase">
          {title}
        </span>
      </div>
      <div className="m-0 text-sm leading-relaxed text-white/90 md:text-base">
        {children}
      </div>
    </div>
  );
}

function Section({
  id,
  title,
  children,
}: {
  id: string;
  title: string;
  children: ReactNode;
}) {
  return (
    <section
      className="mb-32 scroll-mt-32 border-t border-[#27272a] pt-12 first:border-0 first:pt-0"
      id={id}
    >
      <div className="mb-8 flex items-center gap-2.5">
        <span className="h-3 w-1.5 bg-[#22b8cc]" />
        <h2 className="m-0 text-3xl font-bold tracking-tight text-white">
          {title}
        </h2>
      </div>
      <div className="space-y-6 text-lg leading-relaxed text-[#a1a1aa]">
        {children}
      </div>
    </section>
  );
}

function Steps({ items }: { items: [string, ReactNode][] }) {
  return (
    <ol className="my-8 border-l border-[#27272a]">
      {items.map(([name, body], i) => (
        <li className="relative pb-6 pl-8 last:pb-0" key={name}>
          <span className="absolute top-1 -left-[13px] flex h-6 w-6 items-center justify-center border border-[#27272a] bg-black font-mono text-[11px] text-[#22b8cc]">
            {i + 1}
          </span>
          <span className="block font-semibold text-white">{name}</span>
          <span className="block text-base">{body}</span>
        </li>
      ))}
    </ol>
  );
}

const COMMANDS: [string, string][] = [
  ["dokky", "Open the app, where every change happens"],
  ["dokky status", "Show every drive and the background service"],
  [
    "dokky doctor",
    "Check this computer and show the exact fix for anything wrong",
  ],
  [
    "dokky completion",
    "Print shell completion for bash, zsh, fish or PowerShell",
  ],
];

const tocGroups: DocsTOCGroup[] = [
  {
    title: "[ 01 ] // Getting started",
    items: [
      { href: "#install", label: "Install" },
      { href: "#sign-in", label: "Sign in" },
      { href: "#add-drive", label: "Add a drive" },
    ],
  },
  {
    title: "[ 02 ] // Everyday use",
    items: [
      { href: "#mount", label: "Mount and unmount" },
      { href: "#devices", label: "Your devices" },
      { href: "#security", label: "Security" },
      { href: "#cache", label: "Cache and uploads" },
    ],
  },
  {
    title: "[ 03 ] // Upkeep",
    items: [
      { href: "#health", label: "Health and fixes" },
      { href: "#updates", label: "Updates" },
      { href: "#commands", label: "Commands" },
      { href: "#uninstall", label: "Uninstall" },
    ],
  },
];

export default function DocsPage() {
  return (
    <div
      className="relative min-h-screen overflow-clip bg-black text-[#a1a1aa]"
      data-landing
    >
      <div
        className="pointer-events-none fixed inset-0 opacity-[0.03]"
        style={{
          background:
            "repeating-linear-gradient(135deg,transparent,transparent 4px,rgba(255,255,255,0.5) 4px,rgba(255,255,255,0.5) 5px)",
        }}
      />
      <div className="pointer-events-none fixed inset-y-0 left-0 z-10 w-8 bg-gradient-to-r from-black to-transparent" />

      <div className="relative z-10 mx-auto flex w-full max-w-[1400px] flex-col gap-20 px-6 pt-32 pb-32 lg:flex-row lg:px-16">
        <DocsTOC groups={tocGroups} />

        <main className="max-w-4xl min-w-0 flex-[1.5] pt-2 pb-8">
          <ScrollReveal className="mb-12">
            <div className="mb-6 flex items-center gap-2.5">
              <span className="font-mono text-[10px] tracking-[0.2em] text-[#a1a1aa] uppercase">
                Guide
              </span>
            </div>
            <h1 className="mb-8 text-[clamp(3.5rem,6vw,7rem)] leading-[0.9] font-bold tracking-tighter text-pretty text-white">
              Documentation.
            </h1>
            <p className="max-w-2xl text-lg leading-relaxed text-[#a1a1aa] lg:text-xl">
              Everything you need to install dokky, add your drives and keep
              them running on every computer.
            </p>
          </ScrollReveal>

          <Section id="install" title="Install">
            <p>
              dokky installs with npm on macOS, Linux and Windows. The first
              time you run it, it downloads the app for your computer.
            </p>
            <CodeBlock title="macOS, Linux, Windows">npm i -g dokky</CodeBlock>
            <Note title="What each system needs">
              <p className="mb-3">
                <span className="text-white">macOS:</span> macOS 13 or newer,
                nothing else. Drives mount through a local network share, with
                no kernel extension.
              </p>
              <p className="mb-3">
                <span className="text-white">Windows:</span> WinFsp. dokky
                offers to install it the first time, and Windows asks for
                permission.
              </p>
              <p className="m-0">
                <span className="text-white">Linux:</span> FUSE 3, from your
                package manager (below).
              </p>
            </Note>
            <CodeBlock title="Linux, FUSE 3">
              {
                "sudo apt install fuse3     # Debian, Ubuntu\nsudo dnf install fuse3     # Fedora\nsudo pacman -S fuse3       # Arch\nsudo zypper install fuse3  # openSUSE"
              }
            </CodeBlock>
          </Section>

          <Section id="sign-in" title="Sign in">
            <p>
              Run <Code>dokky</Code> to open the app. The first time, it asks
              how you want to use it.
            </p>
            <CodeBlock title="Open the app">dokky</CodeBlock>
            <Steps
              items={[
                [
                  "Sign in with browser",
                  "Your drives sync to every computer you sign in on.",
                ],
                [
                  "Use on this computer only",
                  "Everything stays on this computer. You can sign in later from Settings.",
                ],
              ]}
            />
            <p>
              Either way, dokky then asks you to create a master password and
              shows your recovery key. See{" "}
              <a className="text-white underline" href="#security">
                Security
              </a>
              .
            </p>
          </Section>

          <Section id="add-drive" title="Add a drive">
            <p>
              In the app, open Drives (press <Code>2</Code>) and press{" "}
              <Code>a</Code>.
            </p>
            <Steps
              items={[
                [
                  "Service",
                  "Pick where your files live: Google Drive, OneDrive, Dropbox, S3, a NAS over SFTP or WebDAV, and 50+ more.",
                ],
                [
                  "Sign in",
                  "Sign in to that account, in your browser or with its details.",
                ],
                [
                  "Details",
                  "Name the drive, choose which part of the account it shows (leave it empty for everything), and whether it mounts at login.",
                ],
                ["Mount", "dokky mounts the drive right away."],
              ]}
            />
            <Note title="Where drives appear">
              <p className="mb-3">
                <span className="text-white">macOS and Linux:</span>{" "}
                <Code>~/dokky/&lt;Name&gt;</Code>
              </p>
              <p className="m-0">
                <span className="text-white">Windows:</span> the next free drive
                letter.
              </p>
            </Note>
            <p>
              One account can have many drives, for example one for work and one
              for photos.
            </p>
          </Section>

          <Section id="mount" title="Mount and unmount">
            <p>
              In Drives, select a drive and press <Code>m</Code> to mount or
              unmount it. Press <Code>enter</Code> for its details and settings.
            </p>
            <p>
              A drive never unmounts while it still has files uploading.
              Ejecting it in Finder or Explorer waits for them too.
            </p>
          </Section>

          <Section id="devices" title="Your devices">
            <p>
              When you are signed in, every drive you add shows up on all your
              computers, ready to mount. Mounting is per computer, so each one
              only mounts what it needs.
            </p>
            <p>
              See your computers in Settings, Your devices. Select one and press{" "}
              <Code>d</Code> to remove it. It leaves the list at once, finishes
              its uploads, then erases its dokky data the next time it is
              online.
            </p>
            <p>
              To remove this computer, choose Remove this device in Settings. It
              signs out and erases its dokky data once its uploads finish.
            </p>
            <Note title="Lost or stolen">
              Remove the computer from the dashboard and tick Lost or stolen. It
              erases itself the moment it comes online, without waiting for
              uploads.
            </Note>
          </Section>

          <Section id="security" title="Security">
            <p>
              Your saved sign-ins are end-to-end encrypted. dokky seals them on
              your computer with XChaCha20-Poly1305 before they sync. The key
              that opens them is locked by your master password and, separately,
              by your recovery key. dokky's server stores only the sealed copies
              and can't open them.
            </p>
            <Steps
              items={[
                [
                  "Master password",
                  "Created the first time you open dokky. It is stretched with Argon2id on your computer and never sent anywhere. Every computer on your account uses the same one.",
                ],
                [
                  "Recovery key",
                  "Shown once, right after the master password. Keep it in a password manager or on paper. If you forget the master password, it is the only way back in. We can't reset either of them.",
                ],
                [
                  "Unlock",
                  "Your drives open by themselves when your computer starts and keep working whether the app is open, closed or locked. Changes need the app unlocked with Touch ID, Windows Hello, your system password or the master password.",
                ],
              ]}
            />
            <p>
              One unlock lasts 4 hours. It ends sooner when your screen locks,
              the computer sleeps, you press <Code>l</Code>, the app sits idle
              for 4 minutes or the master password changes. So you don't forget
              the master password, dokky asks for it instead of Touch ID or
              Windows Hello when you haven't unlocked for 7, 15 or 30 days (7
              by default), at least every 90 days, and once after it changes.
            </p>
            <p>
              In Settings, Security you can choose that interval, change the
              master password and make a new recovery key.
            </p>
            <Note title="What the server can see">
              The names of your accounts and drives, which kind of storage each
              one is, folders, cache settings and your computers, so the
              dashboard can show them. Never your sign-ins, passwords, tokens or
              files.
            </Note>
            <Note title="Coming from an older dokky">
              The next time you open dokky, it asks you to create a master
              password, or to enter it if another of your computers already did.
              It then seals your saved sign-ins on your computer and replaces
              the server's old copies. Older versions of dokky stop syncing once
              your account has a master password, until they update. dokky
              updates itself every hour unless you turned that off.
            </Note>
          </Section>

          <Section id="cache" title="Cache and uploads">
            <p>
              dokky keeps recently used files in a local cache so they open
              instantly, and streams large files instead of downloading them
              first.
            </p>
            <p>
              Each drive has its own cache settings in its details:{" "}
              <span className="text-white">Size limit</span> and{" "}
              <span className="text-white">Keep files for</span>. They apply to
              this computer only.
            </p>
            <p>
              Files you save upload in the background. Watch them in Transfers
              (press <Code>3</Code>). Unmounting, removing a drive, and signing
              a computer out all wait for uploads to finish first, and an upload
              that keeps failing is shown so you can retry it. Erasing right
              away (Lost or stolen, uninstall) drops what has not uploaded yet.
            </p>
          </Section>

          <Section id="health" title="Health and fixes">
            <p>
              Health (press <Code>5</Code>) checks dokky, its background service
              and every drive. Press <Code>f</Code> to fix everything it can.
            </p>
            <CodeBlock title="From the terminal">dokky doctor</CodeBlock>
            <p>
              <Code>doctor</Code> shows what is wrong and the exact fix.
            </p>
          </Section>

          <Section id="updates" title="Updates">
            <p>
              dokky updates itself in the background and switches once nothing
              is uploading. Turn this off in Settings, Automatic updates.
            </p>
            <p>
              With them off, dokky says when a new version is out. Press{" "}
              <Code>u</Code> in the app to update right away.
            </p>
          </Section>

          <Section id="commands" title="Commands">
            <div className="my-8 border border-[#27272a]">
              {COMMANDS.map(([cmd, what]) => (
                <div
                  className="flex flex-col gap-1 border-b border-[#27272a] px-5 py-4 last:border-0 sm:flex-row sm:items-baseline sm:gap-6"
                  key={cmd}
                >
                  <code className="shrink-0 font-mono text-sm text-[#22b8cc] sm:w-64">
                    {cmd}
                  </code>
                  <span className="text-base">{what}</span>
                </div>
              ))}
            </div>
            <p>
              Add <Code>--help</Code> to any command for its options.
            </p>
          </Section>

          <Section id="uninstall" title="Uninstall">
            <p>
              In the app, open Settings and choose Uninstall dokky. It unmounts
              your drives, removes this computer from your account, and cleans
              up after itself in about ten minutes.
            </p>
            <CodeBlock title="Or with npm">npm rm -g dokky</CodeBlock>
          </Section>
        </main>
      </div>

      <Footer />
    </div>
  );
}
