import { readBody, sealedServiceBody, serviceBody } from "@/lib/schemas";
import {
  type Caller,
  caller,
  changed,
  type IdContext,
  legacyGate,
  mounts,
  notFound,
  save,
  seal,
  sealedWire,
  services,
  serviceWire,
  vaultAware,
} from "@/lib/sync";
import { findVaultKey, keyMismatch, noVault } from "@/lib/vault";

export const runtime = "nodejs";

async function putLegacy(req: Request, c: Caller): Promise<Response> {
  const blocked = await legacyGate(req, c.userId);
  if (blocked) {
    return blocked;
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

// sealed on the device with the account key; the server only checks it is that key
async function putSealed(req: Request, c: Caller): Promise<Response> {
  const body = await readBody(req, sealedServiceBody);
  if (body instanceof Response) {
    return body;
  }
  const vault = await findVaultKey(c.userId);
  if (!vault) {
    return noVault(409);
  }
  if (vault.keyId !== body.keyId) {
    return keyMismatch();
  }
  const { baseVersion, tokenExpiry, ...fields } = body;
  const doc = await save(
    services(),
    c,
    "service",
    baseVersion,
    tokenExpiry ? { ...fields, tokenExpiry: { token: tokenExpiry } } : fields,
    sealedWire,
    tokenExpiry ? ["options", "secrets"] : ["options", "secrets", "tokenExpiry"]
  );
  if (doc instanceof Response) {
    return doc;
  }
  await changed(req, c, "service.put");
  return Response.json(sealedWire(doc));
}

export async function PUT(
  req: Request,
  { params }: IdContext
): Promise<Response> {
  const c = await caller(req, params);
  if (c instanceof Response) {
    return c;
  }
  return vaultAware(req) ? await putSealed(req, c) : await putLegacy(req, c);
}

export async function DELETE(
  req: Request,
  { params }: IdContext
): Promise<Response> {
  const c = await caller(req, params);
  if (c instanceof Response) {
    return c;
  }
  const blocked = await legacyGate(req, c.userId);
  if (blocked) {
    return blocked;
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
