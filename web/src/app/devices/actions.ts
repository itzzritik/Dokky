"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { auth, signIn } from "@/auth";
import { removeDevice } from "@/lib/device-auth";
import { freshSignIn } from "@/lib/fresh";

export async function remove(form: FormData): Promise<void> {
  const userId = (await auth())?.user?.id;
  if (!userId) {
    redirect("/");
  }
  const id = form.get("id");
  if (typeof id !== "string") {
    return;
  }
  const mode = form.get("mode") === "erase" ? "erase" : "remove";
  if (mode === "erase" && !(await freshSignIn(userId))) {
    await signIn("github", {
      redirectTo: `/devices?erase=${encodeURIComponent(id)}`,
    });
    return;
  }
  await removeDevice(userId, id, mode, "", "");
  revalidatePath("/devices");
}
