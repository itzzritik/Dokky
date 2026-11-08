import { z } from "zod";
import { clientIp } from "@/lib/audit";
import {
  removeDrive,
  renameDrive,
  sessionUser,
  unauthorized,
  validId,
} from "@/lib/dash";
import { apiError } from "@/lib/errors";
import { readBody } from "@/lib/schemas";

export const runtime = "nodejs";

const missing = () => apiError(404, "not_found", "no such drive");

export async function PATCH(
  req: Request,
  { params }: RouteContext<"/api/dash/drives/[id]">
): Promise<Response> {
  const userId = await sessionUser();
  if (!userId) {
    return unauthorized();
  }
  const { id } = await params;
  if (!validId(id)) {
    return missing();
  }
  const body = await readBody(req, z.object({ name: z.unknown() }));
  if (body instanceof Response) {
    return body;
  }
  const result = await renameDrive(userId, id, body.name, clientIp(req));
  if (result === "not_found") {
    return missing();
  }
  if (result === "name_taken") {
    return apiError(409, "name_taken", "another drive uses this name");
  }
  if (result === "bad_name") {
    return apiError(
      400,
      "bad_name",
      "names can't have / or : and are at most 40 characters"
    );
  }
  return new Response(null, { status: 204 });
}

export async function DELETE(
  req: Request,
  { params }: RouteContext<"/api/dash/drives/[id]">
): Promise<Response> {
  const userId = await sessionUser();
  if (!userId) {
    return unauthorized();
  }
  const { id } = await params;
  if (!(validId(id) && (await removeDrive(userId, id, clientIp(req))))) {
    return missing();
  }
  return new Response(null, { status: 204 });
}
