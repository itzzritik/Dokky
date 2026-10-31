import { apiError } from "@/lib/errors";
import { mountBody, readBody } from "@/lib/schemas";
import {
  caller,
  changed,
  type IdContext,
  mounts,
  mountWire,
  notFound,
  save,
  services,
} from "@/lib/sync";

export const runtime = "nodejs";

export async function PUT(
  req: Request,
  { params }: IdContext
): Promise<Response> {
  const c = await caller(req, params);
  if (c instanceof Response) {
    return c;
  }
  const body = await readBody(req, mountBody);
  if (body instanceof Response) {
    return body;
  }
  const { baseVersion, ...fields } = body;
  const owned = await services().countDocuments(
    { _id: fields.serviceId, userId: c.userId },
    { limit: 1 }
  );
  if (!owned) {
    return apiError(400, "bad_request", "serviceId: no such service");
  }
  const doc = await save(mounts(), c, "mount", baseVersion, fields, mountWire);
  if (doc instanceof Response) {
    return doc;
  }
  await changed(req, c, "mount.put");
  return Response.json(mountWire(doc));
}

export async function DELETE(
  req: Request,
  { params }: IdContext
): Promise<Response> {
  const c = await caller(req, params);
  if (c instanceof Response) {
    return c;
  }
  const { deletedCount } = await mounts().deleteOne({
    _id: c.id,
    userId: c.userId,
  });
  if (!deletedCount) {
    return notFound("mount");
  }
  await changed(req, c, "mount.delete");
  return new Response(null, { status: 204 });
}
