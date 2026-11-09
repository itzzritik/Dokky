import { ObjectId } from "mongodb";
import { authDevice } from "@/lib/device-auth";
import {
  mounts,
  mountWire,
  sealedWire,
  services,
  serviceWire,
  updateRequired,
  users,
  vaultAware,
} from "@/lib/sync";
import { findVaultKey } from "@/lib/vault";

export const runtime = "nodejs";

const WEAK = /^W\//u;

export async function GET(req: Request): Promise<Response> {
  const who = await authDevice(req);
  if (who instanceof Response) {
    return who;
  }
  const aware = vaultAware(req);
  const [user, vault] = await Promise.all([
    users().findOne(
      { _id: new ObjectId(who.userId) },
      { projection: { rev: 1 } }
    ),
    findVaultKey(who.userId),
  ]);
  if (vault && !aware) {
    return updateRequired();
  }
  const rev = user?.rev ?? 0;
  // the sealed shape is another representation, so an ETag cached before an update never matches it
  const headers = {
    etag: aware ? `"v${rev}"` : `"${rev}"`,
    "cache-control": "no-store",
  };
  if (req.headers.get("if-none-match")?.replace(WEAK, "") === headers.etag) {
    return new Response(null, { status: 304, headers });
  }
  const [serviceDocs, mountDocs] = await Promise.all([
    services().find({ userId: who.userId }).toArray(),
    mounts().find({ userId: who.userId }).toArray(),
  ]);
  if (!aware) {
    return Response.json(
      {
        rev,
        services: serviceDocs.map(serviceWire),
        mounts: mountDocs.map(mountWire),
      },
      { headers }
    );
  }
  return Response.json(
    {
      rev,
      vault: vault ? { keyId: vault.keyId, version: vault.version } : null,
      services: serviceDocs.map(sealedWire),
      mounts: mountDocs.map(mountWire),
    },
    { headers }
  );
}
