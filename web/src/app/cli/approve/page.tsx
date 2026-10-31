import type { ReactNode } from "react";
import { auth } from "@/auth";
import { Button } from "@/components/ui/button";
import { pendingLogin } from "@/lib/device-auth";
import { USER_CODE } from "@/lib/schemas";
import { approve, deny, signInToApprove } from "./actions";

const results: Record<string, string> = {
  approved: "Device approved. You can go back to your terminal.",
  denied: "Login denied. Nothing was added to your account.",
  invalid:
    "This code is invalid, expired or already used. Run dokky login again.",
};

function Shell({ children }: { children: ReactNode }) {
  return (
    <main className="flex min-h-svh flex-col items-center justify-center gap-6 p-6 text-center">
      <h1 className="text-2xl font-semibold">Approve dokky login</h1>
      {children}
    </main>
  );
}

const Message = ({ text }: { text?: string }) => (
  <Shell>
    <p className="text-muted-foreground">{text}</p>
  </Shell>
);

export default async function ApprovePage({
  searchParams,
}: PageProps<"/cli/approve">) {
  const { code, done } = await searchParams;
  if (typeof done === "string" && Object.hasOwn(results, done)) {
    return <Message text={results[done]} />;
  }
  const userCode = typeof code === "string" ? code.trim().toUpperCase() : "";
  if (!USER_CODE.test(userCode)) {
    return <Message text={results.invalid} />;
  }

  const session = await auth();
  if (!session?.user) {
    return (
      <Shell>
        <p className="text-muted-foreground">
          Sign in to approve the login for code{" "}
          <span className="font-mono">{userCode}</span>.
        </p>
        <form action={signInToApprove}>
          <input name="code" type="hidden" value={userCode} />
          <Button size="lg" type="submit">
            Sign in with GitHub
          </Button>
        </form>
      </Shell>
    );
  }

  const login = await pendingLogin(userCode, new Date());
  if (!login) {
    return <Message text={results.invalid} />;
  }

  return (
    <Shell>
      <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-left">
        <dt className="text-muted-foreground">Device</dt>
        <dd>{login.deviceName}</dd>
        <dt className="text-muted-foreground">OS</dt>
        <dd>
          {login.os} / {login.arch}
        </dd>
        <dt className="text-muted-foreground">Code</dt>
        <dd className="font-mono text-lg tracking-widest">{login.userCode}</dd>
      </dl>
      <p className="text-muted-foreground">
        Approve only if this code matches the one in your terminal.
      </p>
      <div className="flex gap-3">
        <form action={approve}>
          <input name="code" type="hidden" value={userCode} />
          <Button type="submit">Approve</Button>
        </form>
        <form action={deny}>
          <input name="code" type="hidden" value={userCode} />
          <Button type="submit" variant="outline">
            Deny
          </Button>
        </form>
      </div>
    </Shell>
  );
}
