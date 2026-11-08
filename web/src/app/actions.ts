"use server";

import { signIn, signOut } from "@/auth";

export async function signInWithGitHub(): Promise<void> {
  await signIn("github", { redirectTo: "/dashboard" });
}

export async function signInAgain(returnTo: string): Promise<void> {
  await signIn("github", {
    redirectTo: returnTo.startsWith("/dashboard") ? returnTo : "/dashboard",
  });
}

export async function signOutOfDokky(): Promise<void> {
  await signOut({ redirectTo: "/" });
}
