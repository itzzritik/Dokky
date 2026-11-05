import { clientIp } from "@/lib/audit";
import { authDevice, renameDevice, revokeDevice } from "@/lib/device-auth";
import { apiError } from "@/lib/errors";
import { deviceRenameBody, readBody } from "@/lib/schemas";
import type { IdContext } from "@/lib/sync";

export const runtime = "nodejs";

export async function DELETE(
  req: Request,
  { params }: IdContext
): Promise<Response> {
  const who = await authDevice(req);
  if (who instanceof Response) {
    return who;
  }
  const { id } = await params;
  const target = id === "self" ? who.deviceId : id;
  const ok = await revokeDevice(
    who.userId,
    target,
    who.deviceId,
    clientIp(req)
  );
  return ok
    ? new Response(null, { status: 204 })
    : apiError(404, "not_found", "device not found");
}

export async function PATCH(
  req: Request,
  { params }: IdContext
): Promise<Response> {
  const who = await authDevice(req);
  if (who instanceof Response) {
    return who;
  }
  const body = await readBody(req, deviceRenameBody);
  if (body instanceof Response) {
    return body;
  }
  const { id } = await params;
  const ok = await renameDevice(
    who.userId,
    id === "self" ? who.deviceId : id,
    body.name,
    who.deviceId,
    clientIp(req)
  );
  return ok
    ? new Response(null, { status: 204 })
    : apiError(404, "not_found", "device not found");
}
