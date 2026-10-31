"use server";

import { redirect } from "next/navigation";
import { auth, signIn } from "@/auth";
import { decideLogin } from "@/lib/device-auth";
import { USER_CODE } from "@/lib/schemas";

async function decide(form: FormData, status: "approved" | "denied") {
  const userId = (await auth())?.user?.id;
  if (!userId) {
    redirect("/");
  }
  const ok = await decideLogin(userId, form.get("code"), status, new Date());
  redirect(`/cli/approve?done=${ok ? status : "invalid"}`);
}

export async function approve(form: FormData): Promise<void> {
  await decide(form, "approved");
}

export async function deny(form: FormData): Promise<void> {
  await decide(form, "denied");
}

export async function signInToApprove(form: FormData): Promise<void> {
  const code = form.get("code");
  await signIn("github", {
    redirectTo:
      typeof code === "string" && USER_CODE.test(code)
        ? `/cli/approve?code=${code}`
        : "/",
  });
}
