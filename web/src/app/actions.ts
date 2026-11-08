"use server";

import { signIn, signOut } from "@/auth";

export async function signInWithGitHub(): Promise<void> {
  await signIn("github");
}

export async function signOutOfDokky(): Promise<void> {
  await signOut({ redirectTo: "/" });
}
