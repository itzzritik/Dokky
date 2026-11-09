import { z } from "zod";
import { apiError } from "@/lib/errors";

const MAX_BODY = 65_536;
// format byte, 24-byte nonce and 16-byte tag around an empty plaintext
const ENVELOPE_MIN = 41;
const ENVELOPE_VERSION = 0x01;
const BASE64 =
  /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/u;
const SALT_32 = /^[A-Za-z0-9+/]{43}=$/u;

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
  .trim()
  .min(1)
  .regex(/^[^/:\p{Cc}]+$/u)
  .refine((s) => [...s].length <= 40, "at most 40 characters");
const serviceName = z.string().min(1).max(64);
const serviceType = z
  .string()
  .max(64)
  .regex(/^[a-z0-9]+(?: [a-z0-9]+)*$/u);
const isoDate = z.iso.datetime({ offset: true }).pipe(z.coerce.date());

// an XChaCha20-Poly1305 envelope sealed on a device: 0x01, nonce, ciphertext and tag, base64
const envelope = (maxBytes: number) =>
  z
    .string()
    .max(Math.ceil(maxBytes / 3) * 4)
    .regex(BASE64, "must be base64")
    .refine((value) => {
      const bytes = Buffer.from(value, "base64");
      return (
        bytes.length >= ENVELOPE_MIN &&
        bytes.length <= maxBytes &&
        bytes[0] === ENVELOPE_VERSION
      );
    }, `must be a version 1 envelope of ${ENVELOPE_MIN} to ${maxBytes} bytes`);

export const keyId = z.string().regex(/^[0-9a-f]{16}$/u);
const wrap = envelope(512);
const sealedConfig = envelope(32_768 + ENVELOPE_MIN);
const sealedToken = envelope(16_384 + ENVELOPE_MIN);

export const loginStartBody = z.object({
  deviceName: z.string().trim().min(1).max(64),
  os: platform,
  arch: platform,
  version: z.string().max(64),
});

export const deviceRenameBody = z.object({
  name: z.string().trim().min(1).max(64),
});

export const deviceDrivesBody = z.object({
  mountIds: z.array(entityId).max(200),
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
  type: serviceType,
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
  expiry: isoDate,
});

export const sealedServiceBody = z
  .object({
    name: serviceName,
    type: serviceType,
    keyId,
    sealed: sealedConfig,
    sealedToken: sealedToken.nullable().default(null),
    tokenExpiry: isoDate.nullable().default(null),
    baseVersion,
  })
  .refine((body) => body.sealedToken !== null || body.tokenExpiry === null, {
    path: ["tokenExpiry"],
    message: "must be null without a sealedToken",
  });

export const sealedTokenBody = z.object({
  keyId,
  sealedToken,
  expiry: isoDate,
});

const kdf = z.object({
  alg: z.literal("argon2id"),
  salt: z.string().regex(SALT_32, "must be base64 of 32 bytes"),
  t: z.int().min(1).max(10),
  m: z.int().min(19_456).max(262_144),
  p: z.int().min(1).max(16),
});

export const vaultBody = z.object({
  keyId,
  kdf,
  wrappedKey: wrap,
  recoveryWrappedKey: wrap,
});

export const vaultUpdateBody = vaultBody.extend({
  baseVersion: z.int().min(1),
});

const eventIn = z.object({
  id: z.string().max(64).optional(),
  kind: z
    .string()
    .max(64)
    .regex(/^[a-z]+\.[A-Za-z]+$/u),
  target: z.string().max(128),
  name: z.string().max(256),
  ok: z.boolean(),
  reason: z.string().max(500),
  at: z.iso.datetime({ offset: true }),
  key: z.string().max(200).optional(),
});

export const eventsBody = z.object({ entries: z.array(eventIn).max(100) });

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
