import { ObjectId } from "mongodb";
import { authDevice } from "@/lib/device-auth";
import { mounts, mountWire, services, serviceWire, users } from "@/lib/sync";

export const runtime = "nodejs";

const WEAK = /^W\//u;

export async function GET(req: Request): Promise<Response> {
  const who = await authDevice(req);
  if (who instanceof Response) {
    return who;
  }
  const user = await users().findOne(
    { _id: new ObjectId(who.userId) },
    { projection: { rev: 1 } }
  );
  const rev = user?.rev ?? 0;
  const headers = { etag: `"${rev}"`, "cache-control": "no-store" };
  if (req.headers.get("if-none-match")?.replace(WEAK, "") === headers.etag) {
    return new Response(null, { status: 304, headers });
  }
  const [serviceDocs, mountDocs] = await Promise.all([
    services().find({ userId: who.userId }).toArray(),
    mounts().find({ userId: who.userId }).toArray(),
  ]);
  return Response.json(
    {
      rev,
      services: serviceDocs.map(serviceWire),
      mounts: mountDocs.map(mountWire),
    },
    { headers }
  );
}
