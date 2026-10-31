import { z } from "zod";
import { apiError } from "@/lib/errors";

const MAX_BODY = 65_536;

const fieldName = z
  .string()
  .max(64)
  .regex(/^[a-z0-9_]+$/u);
const options = z.record(fieldName, z.string().max(4096));
const secretValue = z.string().max(16_384);
const secrets = z.record(fieldName, secretValue);
const baseVersion = z.int().min(0);
const duration = z
  .string()
  .max(32)
  .regex(/^(?:\d+(?:\.\d+)?(?:ns|us|ms|s|m|h|d|w|M|y))+$/u);
const size = z
  .string()
  .max(32)
  .regex(/^(?:off|\d+(?:\.\d+)?(?:[bkmgtpe]i?b?)?)$/iu);
const platform = z
  .string()
  .max(32)
  .regex(/^[a-z0-9_]+$/u);

export const USER_CODE =
  /^[BCDFGHJKLMNPQRSTVWXZ]{4}-[BCDFGHJKLMNPQRSTVWXZ]{4}$/u;

export const entityId = z.uuid();
const mountName = z
  .string()
  .max(40)
  .regex(/^[a-z0-9]+(?:-[a-z0-9]+)*$/u);
const serviceName = z.string().min(1).max(64);

export const loginStartBody = z.object({
  deviceName: z.string().trim().min(1).max(64),
  os: platform,
  arch: platform,
  version: z.string().max(64),
});

export const loginPollBody = z.object({
  deviceCode: z.string().regex(/^[\w-]{43}$/u),
});

const vfsBody = z.object({
  cacheMode: z.enum(["off", "minimal", "writes", "full"]),
  dirCacheTime: duration,
  readAhead: size,
  readChunkStreams: z.int().min(0).max(64),
  cacheMaxSize: size,
  cacheMaxAge: z.enum(["5m", "15m", "1h", "6h", "24h", "7d"]),
  cachePollInterval: duration,
  cacheMinFreeSpace: size,
  fastFingerprint: z.boolean(),
});

export const serviceBody = z.object({
  name: serviceName,
  type: z
    .string()
    .max(64)
    .regex(/^[a-z0-9]+(?: [a-z0-9]+)*$/u),
  options,
  secrets,
  baseVersion,
});

export const mountBody = z.object({
  name: mountName,
  serviceId: entityId,
  remotePath: z.string().max(4096),
  vfs: vfsBody,
  baseVersion,
});

export const tokenBody = z.object({
  field: fieldName,
  value: secretValue.min(1),
  expiry: z.iso.datetime({ offset: true }).pipe(z.coerce.date()),
});

export async function readBody<T extends z.ZodType>(
  req: Request,
  schema: T
): Promise<z.infer<T> | Response> {
  const tooLarge = () =>
    apiError(413, "body_too_large", `body must be at most ${MAX_BODY} bytes`);
  if (Number(req.headers.get("content-length")) > MAX_BODY) {
    return tooLarge();
  }
  const raw = await req.arrayBuffer();
  if (raw.byteLength > MAX_BODY) {
    return tooLarge();
  }
  let json: unknown;
  try {
    json = JSON.parse(new TextDecoder().decode(raw));
  } catch {
    return apiError(400, "bad_request", "body must be JSON");
  }
  const parsed = schema.safeParse(json);
  if (!parsed.success) {
    const [issue] = parsed.error.issues;
    const path = issue?.path.join(".") || "body";
    return apiError(400, "bad_request", `${path}: ${issue?.message}`);
  }
  return parsed.data;
}
