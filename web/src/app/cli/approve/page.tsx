import {
  ArrowLeftIcon,
  CircleCheckIcon,
  CircleXIcon,
  type LucideIcon,
  SquareTerminalIcon,
} from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";
import { auth } from "@/auth";
import { DokkyMark } from "@/components/landing/brand";
import {
  AnimatedTerminalGrid,
  GlitchButton,
  TERMINAL_CARDS,
} from "@/components/landing/client";
import { Caustics } from "@/components/landing/water";
import { system } from "@/components/shell/devices/labels";
import { pendingLogin } from "@/lib/device-auth";
import { USER_CODE } from "@/lib/schemas";
import { approve, deny, signInToApprove } from "./actions";

const TEAL = "#22b8cc";
const GREEN = "#6ed69a";
const CORAL = "#f2665a";

interface Result {
  color: string;
  icon: LucideIcon;
  text: string;
  title: string;
}

const results: Record<string, Result> = {
  approved: {
    color: GREEN,
    icon: CircleCheckIcon,
    text: "You can go back to your terminal.",
    title: "Device approved",
  },
  denied: {
    color: CORAL,
    icon: CircleXIcon,
    text: "Nothing was added to your account.",
    title: "Login denied",
  },
  invalid: {
    color: CORAL,
    icon: CircleXIcon,
    text: "It's invalid, expired or already used. Run dokky login again.",
    title: "Code not valid",
  },
};

function Frame({
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

function Card({
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

const CodeBox = ({ code }: { code: string }) => (
  <div className="border border-[#27272a] bg-black">
    <div className="flex items-center justify-between border-b border-[#18181b] px-4 py-2">
      <span className="font-mono text-[9px] tracking-widest text-[#52525b] uppercase">
        Login code
      </span>
      <span className="size-1.5 animate-pulse rounded-full bg-[#6ed69a] shadow-[0_0_6px_#6ed69a]" />
    </div>
    <div className="flex flex-col items-center gap-4 px-4 py-6 text-center">
      <p className="font-mono text-[10px] tracking-widest text-[#a1a1aa] uppercase">
        Check it matches your terminal
      </p>
      <p className="font-mono text-2xl leading-none font-bold tracking-[0.2em] text-[#22b8cc]">
        {code}
      </p>
    </div>
  </div>
);

const Decide = ({
  action,
  code,
  label,
  variant,
}: {
  action: (form: FormData) => Promise<void>;
  code: string;
  label: string;
  variant: "primary" | "secondary";
}) => (
  <form action={action}>
    <input name="code" type="hidden" value={code} />
    <GlitchButton
      className="h-12 w-full text-[12px] tracking-widest"
      type="submit"
      variant={variant}
    >
      {label}
    </GlitchButton>
  </form>
);

export default async function ApprovePage({
  searchParams,
}: PageProps<"/cli/approve">) {
  const { code, done } = await searchParams;
  if (typeof done === "string" && Object.hasOwn(results, done)) {
    return <Frame {...results[done]} />;
  }
  const userCode = typeof code === "string" ? code.trim().toUpperCase() : "";
  if (!USER_CODE.test(userCode)) {
    return <Frame {...results.invalid} />;
  }

  const session = await auth();
  if (!session?.user) {
    return (
      <Frame text="Sign in to approve this device." title="Approve login">
        <Card aside="GitHub" label="Session // Sign in">
          <CodeBox code={userCode} />
          <div className="mt-6">
            <Decide
              action={signInToApprove}
              code={userCode}
              label="Continue with GitHub"
              variant="primary"
            />
          </div>
        </Card>
      </Frame>
    );
  }

  const login = await pendingLogin(userCode, new Date());
  if (!login) {
    return <Frame {...results.invalid} />;
  }

  return (
    <Frame text="A device wants to join your account." title="Approve login">
      <Card aside="dokky login" label="Device // Login">
        <CodeBox code={login.userCode} />
        <dl className="mt-6 grid grid-cols-[auto_1fr] items-baseline gap-x-6 gap-y-2 font-mono text-[13px]">
          <dt className="text-[10px] tracking-widest text-[#52525b] uppercase">
            Device
          </dt>
          <dd className="text-white">{login.deviceName}</dd>
          <dt className="text-[10px] tracking-widest text-[#52525b] uppercase">
            System
          </dt>
          <dd className="text-white">{system(login)}</dd>
        </dl>
        <div className="mt-8 grid grid-cols-2 gap-3">
          <Decide
            action={approve}
            code={userCode}
            label="Approve"
            variant="primary"
          />
          <Decide
            action={deny}
            code={userCode}
            label="Deny"
            variant="secondary"
          />
        </div>
        <p className="mt-6 text-center font-mono text-[11px] text-[#71717a]">
          Didn't just run dokky login? Deny.
        </p>
      </Card>
    </Frame>
  );
}
