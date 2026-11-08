import { auth } from "@/auth";
import { counts, json, unauthorized } from "@/lib/dash";

export const runtime = "nodejs";

export async function GET(): Promise<Response> {
  const user = (await auth())?.user;
  if (!user?.id) {
    return unauthorized();
  }
  return json({
    name: user.name ?? "",
    email: user.email ?? "",
    image: user.image ?? null,
    ...(await counts(user.id)),
  });
}
