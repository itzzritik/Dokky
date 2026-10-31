import { authDevice, listDevices } from "@/lib/device-auth";

export const runtime = "nodejs";

export async function GET(req: Request): Promise<Response> {
  const who = await authDevice(req);
  if (who instanceof Response) {
    return who;
  }
  const rows = await listDevices(who.userId);
  return Response.json(
    rows.map((d) => ({
      id: String(d._id),
      name: d.name,
      os: d.os,
      arch: d.arch,
      version: d.version,
      lastSeenAt: d.lastSeenAt,
      createdAt: d.createdAt,
      current: String(d._id) === who.deviceId,
    }))
  );
}
