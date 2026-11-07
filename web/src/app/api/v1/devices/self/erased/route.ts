import { authDevice, markErased } from "@/lib/device-auth";
import { apiError } from "@/lib/errors";

export const runtime = "nodejs";

export async function POST(req: Request): Promise<Response> {
  const who = await authDevice(req, new Date(), { removed: "report" });
  if (who instanceof Response) {
    return who;
  }
  if (!who.removed) {
    return apiError(409, "not_removed", "this device was not removed");
  }
  await markErased(who.userId, who.deviceId);
  return new Response(null, { status: 204 });
}
