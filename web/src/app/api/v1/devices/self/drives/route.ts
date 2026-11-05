import { authDevice, setDeviceDrives } from "@/lib/device-auth";
import { deviceDrivesBody, readBody } from "@/lib/schemas";

export const runtime = "nodejs";

export async function PUT(req: Request): Promise<Response> {
  const who = await authDevice(req);
  if (who instanceof Response) {
    return who;
  }
  const body = await readBody(req, deviceDrivesBody);
  if (body instanceof Response) {
    return body;
  }
  await setDeviceDrives(who.userId, who.deviceId, body.mountIds);
  return new Response(null, { status: 204 });
}
