import { clientIp } from "@/lib/audit";
import { authDevice, removeDevice, renameDevice } from "@/lib/device-auth";
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
  const mode = new URL(req.url).searchParams.get("mode") ?? "remove";
  if (mode !== "remove" && mode !== "erase") {
    return apiError(400, "bad_request", "mode must be remove or erase");
  }
  // a stolen device must not be able to erase the others
  if (mode === "erase" && target !== who.deviceId) {
    return apiError(
      403,
      "forbidden",
      "a lost or stolen device is removed from the dashboard"
    );
  }
  const ok = await removeDevice(
    who.userId,
    target,
    mode,
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
