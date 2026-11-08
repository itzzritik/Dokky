import { CircleCheckIcon, CircleXIcon, type LucideIcon } from "lucide-react";
import { auth } from "@/auth";
import { GlitchButton } from "@/components/landing/client";
import { Card, CORAL, Frame, GREEN } from "@/components/landing/frame";
import { system } from "@/components/shell/devices/labels";
import { pendingLogin } from "@/lib/device-auth";
import { USER_CODE } from "@/lib/schemas";
import { approve, deny, signInToApprove } from "./actions";

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
