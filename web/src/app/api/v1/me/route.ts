import { ObjectId } from "mongodb";
import { authDevice } from "@/lib/device-auth";
import { users } from "@/lib/sync";

export const runtime = "nodejs";

export async function GET(req: Request): Promise<Response> {
  const who = await authDevice(req);
  if (who instanceof Response) {
    return who;
  }
  const user = await users().findOne(
    { _id: new ObjectId(who.userId) },
    { projection: { name: 1, email: 1 } }
  );
  return Response.json({ name: user?.name ?? "", email: user?.email ?? "" });
}
