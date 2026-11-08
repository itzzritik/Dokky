import { json, listDashDevices, sessionUser, unauthorized } from "@/lib/dash";

export const runtime = "nodejs";

export async function GET(): Promise<Response> {
  const userId = await sessionUser();
  return userId ? json(await listDashDevices(userId)) : unauthorized();
}
