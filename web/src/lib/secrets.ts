import { createCipheriv, createDecipheriv, randomBytes } from "node:crypto";

export interface Box {
  c: string;
  n: string;
}

const KEY_BYTES = 32;
const NONCE_BYTES = 12;
const TAG_BYTES = 16;

function loadKey(): Buffer {
  const raw = process.env.ENCRYPTION_KEY;
  if (!raw) {
    throw new Error("ENCRYPTION_KEY is not set");
  }
  const key = Buffer.from(raw, "base64");
  if (key.length !== KEY_BYTES) {
    throw new Error(`ENCRYPTION_KEY must be base64 of ${KEY_BYTES} bytes`);
  }
  return key;
}

export const aadFor = (userId: string, serviceId: string, field: string) =>
  `${userId}|${serviceId}|${field}`;

export function encrypt(plain: string, aad: string): Box {
  const nonce = randomBytes(NONCE_BYTES);
  const cipher = createCipheriv("aes-256-gcm", loadKey(), nonce);
  cipher.setAAD(Buffer.from(aad));
  const body = Buffer.concat([cipher.update(plain, "utf8"), cipher.final()]);
  return {
    n: nonce.toString("base64"),
    c: Buffer.concat([body, cipher.getAuthTag()]).toString("base64"),
  };
}

export function decrypt(box: Box, aad: string): string {
  const key = loadKey();
  const nonce = Buffer.from(box.n, "base64");
  const data = Buffer.from(box.c, "base64");
  if (nonce.length !== NONCE_BYTES || data.length < TAG_BYTES) {
    throw new Error("secret box is malformed");
  }
  const decipher = createDecipheriv("aes-256-gcm", key, nonce);
  decipher.setAAD(Buffer.from(aad));
  decipher.setAuthTag(data.subarray(data.length - TAG_BYTES));
  return Buffer.concat([
    decipher.update(data.subarray(0, data.length - TAG_BYTES)),
    decipher.final(),
  ]).toString("utf8");
}
