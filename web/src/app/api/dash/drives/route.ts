import { json, listDrives, sessionUser, unauthorized } from "@/lib/dash";

export const runtime = "nodejs";

export async function GET(): Promise<Response> {
  const userId = await sessionUser();
  return userId ? json(await listDrives(userId)) : unauthorized();
}
