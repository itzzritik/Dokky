import { CircleXIcon } from "lucide-react";
import { cookies } from "next/headers";
import { signInAgain } from "@/app/actions";
import { GlitchButton } from "@/components/landing/client";
import { Card, CORAL, Frame } from "@/components/landing/frame";

const REASONS: Record<string, string> = {
  AccessDenied: "GitHub didn't allow the sign-in.",
  Configuration: "Sign-in isn't working on our side. Try again soon.",
  OAuthAccountNotLinked: "That email is already linked to another sign-in.",
};

// Auth.js keeps where the sign-in was headed in this cookie
async function returnTo(): Promise<string> {
  const jar = await cookies();
  const raw = (
    jar.get("__Secure-authjs.callback-url") ?? jar.get("authjs.callback-url")
  )?.value;
  if (!raw) {
    return "/dashboard";
  }
  const url = URL.parse(raw, "http://local");
  return url ? url.pathname + url.search : "/dashboard";
}

export default async function SignInPage({
  searchParams,
}: PageProps<"/signin">) {
  const { error } = await searchParams;
  const failed = typeof error === "string" && error !== "";
  const action = signInAgain.bind(null, await returnTo());
  return (
    <Frame
      {...(failed ? { color: CORAL, icon: CircleXIcon } : {})}
      text={
        failed
          ? (REASONS[error] ?? "GitHub sign-in didn't finish. Try again.")
          : "Use your GitHub account to continue."
      }
      title={failed ? "Sign-in failed" : "Sign in"}
    >
      <Card aside="GitHub" label="Session // Sign in">
        <form action={action}>
          <GlitchButton
            className="h-12 w-full text-[12px] tracking-widest"
            type="submit"
          >
            {failed ? "Try again" : "Continue with GitHub"}
          </GlitchButton>
        </form>
      </Card>
    </Frame>
  );
}
