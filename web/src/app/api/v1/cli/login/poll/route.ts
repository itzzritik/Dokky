import { clientIp } from "@/lib/audit";
import { pollLogin } from "@/lib/device-auth";
import { loginPollBody, readBody } from "@/lib/schemas";

export const runtime = "nodejs";

export async function POST(req: Request): Promise<Response> {
  const body = await readBody(req, loginPollBody);
  if (body instanceof Response) {
    return body;
  }
  return Response.json(
    await pollLogin(body.deviceCode, clientIp(req), new Date())
  );
}
