import { createCipheriv, createDecipheriv, randomBytes } from "node:crypto";

export interface Box {
  c: string;
  n: string;
  v: number;
}

const KEY_BYTES = 32;
const NONCE_BYTES = 12;
const TAG_BYTES = 16;
const VERSION = /^[1-9]\d{0,8}$/u;

function loadKeys(): { current: number; keys: Map<number, Buffer> } {
  const raw = process.env.DOKKY_SECRET_KEYS;
  if (!raw) {
    throw new Error("DOKKY_SECRET_KEYS is not set");
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    // biome-ignore lint/style/useErrorCause: the parse error echoes the key
    throw new Error("DOKKY_SECRET_KEYS is not valid JSON");
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("DOKKY_SECRET_KEYS must be a JSON map of version to key");
  }
  const keys = new Map<number, Buffer>();
  for (const [version, value] of Object.entries(parsed)) {
    if (!VERSION.test(version)) {
      throw new Error("DOKKY_SECRET_KEYS versions must be positive integers");
    }
    const key = Buffer.from(String(value), "base64");
    if (key.length !== KEY_BYTES) {
      throw new Error(
        `DOKKY_SECRET_KEYS version ${version} must be base64 of ${KEY_BYTES} bytes`
      );
    }
    keys.set(Number(version), key);
  }
  const currentRaw = process.env.DOKKY_SECRET_KEY_CURRENT ?? "";
  const current = Number(currentRaw);
  if (!(VERSION.test(currentRaw) && keys.has(current))) {
    throw new Error(
      "DOKKY_SECRET_KEY_CURRENT is not set or not in DOKKY_SECRET_KEYS"
    );
  }
  return { current, keys };
}

export const aadFor = (userId: string, serviceId: string, field: string) =>
  `${userId}|${serviceId}|${field}`;

export function currentVersion(): number {
  return loadKeys().current;
}

export function encrypt(plain: string, aad: string): Box {
  const { current, keys } = loadKeys();
  const nonce = randomBytes(NONCE_BYTES);
  const cipher = createCipheriv(
    "aes-256-gcm",
    keys.get(current) as Buffer,
    nonce
  );
  cipher.setAAD(Buffer.from(aad));
  const body = Buffer.concat([cipher.update(plain, "utf8"), cipher.final()]);
  return {
    v: current,
    n: nonce.toString("base64"),
    c: Buffer.concat([body, cipher.getAuthTag()]).toString("base64"),
  };
}

export function decrypt(box: Box, aad: string): string {
  const key = loadKeys().keys.get(box.v);
  if (!key) {
    throw new Error(`unknown secret key version ${box.v}`);
  }
  const nonce = Buffer.from(box.n, "base64");
  const data = Buffer.from(box.c, "base64");
  if (nonce.length !== NONCE_BYTES || data.length < TAG_BYTES) {
    throw new Error("secret box is malformed");
  }
  const decipher = createDecipheriv(
    "aes-256-gcm",
    key,
    Buffer.from(box.n, "base64")
  );
  decipher.setAAD(Buffer.from(aad));
  decipher.setAuthTag(data.subarray(data.length - TAG_BYTES));
  return Buffer.concat([
    decipher.update(data.subarray(0, data.length - TAG_BYTES)),
    decipher.final(),
  ]).toString("utf8");
}
