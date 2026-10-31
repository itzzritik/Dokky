import { readBody, serviceBody } from "@/lib/schemas";
import {
  caller,
  changed,
  type IdContext,
  mounts,
  notFound,
  save,
  seal,
  services,
  serviceWire,
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
  const body = await readBody(req, serviceBody);
  if (body instanceof Response) {
    return body;
  }
  const { baseVersion, secrets, ...fields } = body;
  const doc = await save(
    services(),
    c,
    "service",
    baseVersion,
    { ...fields, secrets: seal(c, secrets) },
    serviceWire
  );
  if (doc instanceof Response) {
    return doc;
  }
  await changed(req, c, "service.put");
  return Response.json(serviceWire(doc));
}

export async function DELETE(
  req: Request,
  { params }: IdContext
): Promise<Response> {
  const c = await caller(req, params);
  if (c instanceof Response) {
    return c;
  }
  const removed = await mounts().deleteMany({
    userId: c.userId,
    serviceId: c.id,
  });
  const { deletedCount } = await services().deleteOne({
    _id: c.id,
    userId: c.userId,
  });
  if (!(deletedCount || removed.deletedCount)) {
    return notFound("service");
  }
  await changed(req, c, "service.delete");
  return new Response(null, { status: 204 });
}
