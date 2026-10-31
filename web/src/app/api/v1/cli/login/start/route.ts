import { startLogin } from "@/lib/device-auth";
import { loginStartBody, readBody } from "@/lib/schemas";

export const runtime = "nodejs";

export async function POST(req: Request): Promise<Response> {
  const body = await readBody(req, loginStartBody);
  if (body instanceof Response) {
    return body;
  }
  return Response.json(await startLogin(body, new Date()));
}
