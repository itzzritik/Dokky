import { readBody, sealedTokenBody, tokenBody } from "@/lib/schemas";
import { aadFor, encrypt } from "@/lib/secrets";
import {
  type Caller,
  caller,
  changed,
  type IdContext,
  legacyGate,
  notFound,
  services,
  vaultAware,
} from "@/lib/sync";
import { findVaultKey, keyMismatch, noVault } from "@/lib/vault";

export const runtime = "nodejs";

// the token with the later expiry wins, whichever device refreshed it
async function keepNewest(
  req: Request,
  c: Caller,
  field: string,
  expiry: Date,
  set: Record<string, unknown>,
  unset: Record<string, ""> = {}
): Promise<Response> {
  const own = { _id: c.id, userId: c.userId };
  const expiryPath = `tokenExpiry.${field}`;
  const { matchedCount } = await services().updateOne(
    {
      ...own,
      $or: [
        { [expiryPath]: { $exists: false } },
        { [expiryPath]: { $lt: expiry } },
      ],
    },
    {
      $set: {
        ...set,
        [expiryPath]: expiry,
        updatedAt: new Date(),
        updatedBy: c.deviceId,
      },
      ...(Object.keys(unset).length > 0 ? { $unset: unset } : {}),
      $inc: { version: 1 },
    }
  );
  if (matchedCount) {
    await changed(req, c, "token.writeback");
    return Response.json({ kept: "new" });
  }
  if (await services().countDocuments(own, { limit: 1 })) {
    return Response.json({ kept: "existing" });
  }
  return notFound("service");
}

async function writeLegacy(req: Request, c: Caller): Promise<Response> {
  const blocked = await legacyGate(req, c.userId);
  if (blocked) {
    return blocked;
  }
  const body = await readBody(req, tokenBody);
  if (body instanceof Response) {
    return body;
  }
  return await keepNewest(req, c, body.field, body.expiry, {
    [`secrets.${body.field}`]: encrypt(
      body.value,
      aadFor(c.userId, c.id, body.field)
    ),
  });
}

// a sealed token replaces any server-encrypted one, so only one copy is ever current
async function writeSealed(req: Request, c: Caller): Promise<Response> {
  const body = await readBody(req, sealedTokenBody);
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
  return await keepNewest(
    req,
    c,
    "token",
    body.expiry,
    { keyId: body.keyId, sealedToken: body.sealedToken },
    { "secrets.token": "" }
  );
}

export async function POST(
  req: Request,
  { params }: IdContext
): Promise<Response> {
  // a removed device still writes back refreshed tokens while it finishes its uploads
  const c = await caller(req, params, { removed: "writeback" });
  if (c instanceof Response) {
    return c;
  }
  return vaultAware(req)
    ? await writeSealed(req, c)
    : await writeLegacy(req, c);
}
