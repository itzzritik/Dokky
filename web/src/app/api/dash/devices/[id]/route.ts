import { clientIp } from "@/lib/audit";
import { fresh, sessionUser, unauthorized } from "@/lib/dash";
import { removeDevice } from "@/lib/device-auth";
import { apiError } from "@/lib/errors";
import { freshSignIn } from "@/lib/fresh";

export const runtime = "nodejs";

export async function DELETE(
  req: Request,
  { params }: RouteContext<"/api/dash/devices/[id]">
): Promise<Response> {
  const userId = await sessionUser();
  if (!userId) {
    return unauthorized();
  }
  const { id } = await params;
  const mode =
    new URL(req.url).searchParams.get("mode") === "erase" ? "erase" : "remove";
  if (mode === "erase" && !(await freshSignIn(userId))) {
    return fresh();
  }
  const ok = await removeDevice(userId, id, mode, "", clientIp(req));
  return ok
    ? new Response(null, { status: 204 })
    : apiError(404, "not_found", "device not found");
}
