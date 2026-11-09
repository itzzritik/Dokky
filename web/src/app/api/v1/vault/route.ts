import { authDevice } from "@/lib/device-auth";
import { rateLimit, tooMany } from "@/lib/rate-limit";
import { readBody, vaultBody, vaultUpdateBody } from "@/lib/schemas";
import { changed } from "@/lib/sync";
import {
  createVault,
  noVault,
  updateVault,
  vaults,
  vaultWire,
} from "@/lib/vault";

export const runtime = "nodejs";

// each read hands out wraps a thief would guess offline, so a stolen token gets them slowly
const reads = rateLimit(20);

export async function GET(req: Request): Promise<Response> {
  const who = await authDevice(req);
  if (who instanceof Response) {
    return who;
  }
  const wait = reads(who.deviceId);
  if (wait > 0) {
    return tooMany(wait);
  }
  const doc = await vaults().findOne({ _id: who.userId });
  if (!doc) {
    return noVault(404);
  }
  return Response.json(
    { vault: vaultWire(doc) },
    { headers: { "cache-control": "no-store" } }
  );
}

export async function POST(req: Request): Promise<Response> {
  const who = await authDevice(req);
  if (who instanceof Response) {
    return who;
  }
  const body = await readBody(req, vaultBody);
  if (body instanceof Response) {
    return body;
  }
  const doc = await createVault(who.userId, who.deviceId, body);
  if (doc instanceof Response) {
    return doc;
  }
  // a new rev tells every device's next sync that the account now has a vault
  await changed(req, { ...who, id: doc.keyId }, "vault.create");
  return Response.json({ vault: vaultWire(doc) }, { status: 201 });
}

export async function PUT(req: Request): Promise<Response> {
  const who = await authDevice(req);
  if (who instanceof Response) {
    return who;
  }
  const body = await readBody(req, vaultUpdateBody);
  if (body instanceof Response) {
    return body;
  }
  const doc = await updateVault(who.userId, who.deviceId, body);
  if (doc instanceof Response) {
    return doc;
  }
  await changed(req, { ...who, id: doc.keyId }, "vault.rekey");
  return Response.json({ vault: vaultWire(doc) });
}
