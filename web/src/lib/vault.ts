import { MongoServerError } from "mongodb";
import type { z } from "zod";
import { db } from "@/lib/db";
import { apiError } from "@/lib/errors";
import type { vaultBody, vaultUpdateBody } from "@/lib/schemas";
import { aadFor, type Box, decrypt, encrypt } from "@/lib/secrets";

export interface Kdf {
  alg: "argon2id";
  m: number;
  p: number;
  salt: string;
  t: number;
}

// _id is the user id; the account key itself never reaches the server, only its wraps
export interface VaultDoc {
  _id: string;
  createdAt: Date;
  createdBy: string;
  kdf: Kdf;
  keyId: string;
  passwordChangedAt: Date;
  recoveryWrappedKey: Box;
  updatedAt: Date;
  updatedBy: string;
  version: number;
  wrappedKey: Box;
}

type Wrap = "wrappedKey" | "recoveryWrappedKey";

export const vaults = () => db().collection<VaultDoc>("vaults");

// the device's envelope stays opaque; this outer layer only keeps a database leak from starting an offline guess
const lock = (userId: string, field: Wrap, envelope: string) =>
  encrypt(envelope, aadFor(userId, "vault", field));

const unlock = (doc: VaultDoc, field: Wrap) =>
  decrypt(doc[field], aadFor(doc._id, "vault", field));

const kdfOf = ({ alg, salt, t, m, p }: Kdf): Kdf => ({ alg, salt, t, m, p });

const sameKdf = (a: Kdf, b: Kdf) =>
  a.alg === b.alg &&
  a.salt === b.salt &&
  a.t === b.t &&
  a.m === b.m &&
  a.p === b.p;

export const vaultWire = (doc: VaultDoc) => ({
  version: doc.version,
  keyId: doc.keyId,
  kdf: kdfOf(doc.kdf),
  wrappedKey: unlock(doc, "wrappedKey"),
  recoveryWrappedKey: unlock(doc, "recoveryWrappedKey"),
  createdAt: doc.createdAt.toISOString(),
  passwordChangedAt: doc.passwordChangedAt.toISOString(),
});

export const findVaultKey = (userId: string) =>
  vaults().findOne({ _id: userId }, { projection: { keyId: 1, version: 1 } });

export const noVault = (status: 404 | 409) =>
  apiError(status, "no_vault", "this account has no vault yet");

export const keyMismatch = () =>
  apiError(
    409,
    "key_mismatch",
    "this device's key is not the account's key, enter the account's master password"
  );

const withCurrent = (code: string, message: string, current: VaultDoc) =>
  Response.json(
    { error: { code, message }, current: vaultWire(current) },
    { status: 409 }
  );

export async function createVault(
  userId: string,
  deviceId: string,
  body: z.infer<typeof vaultBody>
): Promise<VaultDoc | Response> {
  const now = new Date();
  const doc: VaultDoc = {
    _id: userId,
    version: 1,
    keyId: body.keyId,
    kdf: kdfOf(body.kdf),
    wrappedKey: lock(userId, "wrappedKey", body.wrappedKey),
    recoveryWrappedKey: lock(
      userId,
      "recoveryWrappedKey",
      body.recoveryWrappedKey
    ),
    createdAt: now,
    updatedAt: now,
    passwordChangedAt: now,
    createdBy: deviceId,
    updatedBy: deviceId,
  };
  try {
    await vaults().insertOne(doc);
    return doc;
  } catch (error) {
    if (!(error instanceof MongoServerError && error.code === 11_000)) {
      throw error;
    }
  }
  const current = await vaults().findOne({ _id: userId });
  if (!current) {
    return createVault(userId, deviceId, body);
  }
  return withCurrent(
    "vault_exists",
    "this account already has a vault",
    current
  );
}

// a rewrap only: the account key and its keyId never change here
export async function updateVault(
  userId: string,
  deviceId: string,
  body: z.infer<typeof vaultUpdateBody>
): Promise<VaultDoc | Response> {
  const current = await vaults().findOne({ _id: userId });
  if (!current) {
    return noVault(404);
  }
  if (current.keyId !== body.keyId) {
    return keyMismatch();
  }
  const conflict = (latest: VaultDoc) =>
    withCurrent("conflict", "the vault was changed elsewhere first", latest);
  if (current.version !== body.baseVersion) {
    return conflict(current);
  }
  const now = new Date();
  const rewrapped =
    body.wrappedKey !== unlock(current, "wrappedKey") ||
    !sameKdf(current.kdf, body.kdf);
  const doc = await vaults().findOneAndUpdate(
    { _id: userId, keyId: body.keyId, version: body.baseVersion },
    {
      $set: {
        kdf: kdfOf(body.kdf),
        wrappedKey: lock(userId, "wrappedKey", body.wrappedKey),
        recoveryWrappedKey: lock(
          userId,
          "recoveryWrappedKey",
          body.recoveryWrappedKey
        ),
        updatedAt: now,
        updatedBy: deviceId,
        ...(rewrapped ? { passwordChangedAt: now } : {}),
      },
      $inc: { version: 1 },
    },
    { returnDocument: "after" }
  );
  if (doc) {
    return doc;
  }
  const moved = await vaults().findOne({ _id: userId });
  return moved ? conflict(moved) : noVault(404);
}
