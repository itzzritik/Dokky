"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";
import { auth } from "@/auth";
import { revokeDevice } from "@/lib/device-auth";

export async function revoke(form: FormData): Promise<void> {
  const userId = (await auth())?.user?.id;
  if (!userId) {
    redirect("/");
  }
  const id = form.get("id");
  if (typeof id === "string") {
    await revokeDevice(userId, id, "", "");
  }
  revalidatePath("/devices");
}
