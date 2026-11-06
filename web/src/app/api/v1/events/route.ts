import { authDevice } from "@/lib/device-auth";
import { apiError } from "@/lib/errors";
import { listEvents, record } from "@/lib/events";
import { eventsBody, readBody } from "@/lib/schemas";

export const runtime = "nodejs";

export async function POST(req: Request): Promise<Response> {
  const who = await authDevice(req);
  if (who instanceof Response) {
    return who;
  }
  const body = await readBody(req, eventsBody);
  if (body instanceof Response) {
    return body;
  }
  await record(
    who.userId,
    body.entries.map(({ id, ...e }) => ({
      ...e,
      clientId: id,
      at: new Date(e.at),
      deviceId: who.deviceId,
    }))
  );
  return new Response(null, { status: 204 });
}

export async function GET(req: Request): Promise<Response> {
  const who = await authDevice(req);
  if (who instanceof Response) {
    return who;
  }
  const url = new URL(req.url);
  const raw = url.searchParams.get("before");
  const before = raw ? new Date(raw) : null;
  if (before && Number.isNaN(before.getTime())) {
    return apiError(400, "bad_request", "before must be a date");
  }
  const limit = Math.min(
    Math.max(Number(url.searchParams.get("limit")) || 50, 1),
    100
  );
  return Response.json(
    { items: await listEvents(who.userId, before, limit) },
    { headers: { "cache-control": "no-store" } }
  );
}
