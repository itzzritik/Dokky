import { readBody, tokenBody } from "@/lib/schemas";
import { aadFor, encrypt } from "@/lib/secrets";
import {
  caller,
  changed,
  type IdContext,
  notFound,
  services,
} from "@/lib/sync";

export const runtime = "nodejs";

export async function POST(
  req: Request,
  { params }: IdContext
): Promise<Response> {
  const c = await caller(req, params);
  if (c instanceof Response) {
    return c;
  }
  const body = await readBody(req, tokenBody);
  if (body instanceof Response) {
    return body;
  }
  const own = { _id: c.id, userId: c.userId };
  const expiryPath = `tokenExpiry.${body.field}`;
  const { matchedCount } = await services().updateOne(
    {
      ...own,
      $or: [
        { [expiryPath]: { $exists: false } },
        { [expiryPath]: { $lt: body.expiry } },
      ],
    },
    {
      $set: {
        [`secrets.${body.field}`]: encrypt(
          body.value,
          aadFor(c.userId, c.id, body.field)
        ),
        [expiryPath]: body.expiry,
        updatedAt: new Date(),
        updatedBy: c.deviceId,
      },
      $inc: { version: 1 },
    }
  );
  if (matchedCount) {
    await changed(req, c, "token.writeback");
    return Response.json({ kept: "new" });
  }
  if (await services().countDocuments(own, { limit: 1 })) {
    return Response.json({ kept: "existing" });
  }
  return notFound("service");
}
