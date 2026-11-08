"use server";

import { signIn, signOut } from "@/auth";

export async function signInWithGitHub(): Promise<void> {
  await signIn("github", { redirectTo: "/dashboard" });
}

export async function signInAgain(returnTo: string): Promise<void> {
  const safe =
    returnTo.startsWith("/dashboard") || returnTo.startsWith("/cli/approve");
  await signIn("github", { redirectTo: safe ? returnTo : "/dashboard" });
}

export async function signOutOfDokky(): Promise<void> {
  await signOut({ redirectTo: "/" });
}
