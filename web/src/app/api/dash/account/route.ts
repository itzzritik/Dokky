import { clientIp } from "@/lib/audit";
import { deleteAccount, fresh, sessionUser, unauthorized } from "@/lib/dash";
import { freshSignIn } from "@/lib/fresh";

export const runtime = "nodejs";

export async function DELETE(req: Request): Promise<Response> {
  const userId = await sessionUser();
  if (!userId) {
    return unauthorized();
  }
  if (!(await freshSignIn(userId))) {
    return fresh();
  }
  await deleteAccount(userId, clientIp(req));
  return new Response(null, { status: 204 });
}
