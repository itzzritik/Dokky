import { json, sessionUser, unauthorized } from "@/lib/dash";
import { apiError } from "@/lib/errors";
import { listEvents } from "@/lib/events";

export const runtime = "nodejs";

const PAGE = 50;

export async function GET(req: Request): Promise<Response> {
  const userId = await sessionUser();
  if (!userId) {
    return unauthorized();
  }
  const raw = new URL(req.url).searchParams.get("before");
  const before = raw ? new Date(raw) : null;
  if (before && Number.isNaN(before.getTime())) {
    return apiError(400, "bad_request", "before must be a date");
  }
  const items = await listEvents(userId, before, PAGE);
  return json({ items, more: items.length === PAGE });
}
