import {
  type Collection,
  type Filter,
  MongoServerError,
  ObjectId,
  type OptionalUnlessRequiredId,
  type UpdateFilter,
} from "mongodb";
import type { z } from "zod";
import { type AuditAction, audit, clientIp } from "@/lib/audit";
import { db } from "@/lib/db";
import { authDevice } from "@/lib/device-auth";
import { apiError } from "@/lib/errors";
import { entityId, type mountBody } from "@/lib/schemas";
import { aadFor, type Box, decrypt, encrypt } from "@/lib/secrets";
import { vaults } from "@/lib/vault";

interface Item {
  _id: string;
  createdAt?: Date;
  updatedAt: Date;
  updatedBy: string;
  userId: string;
  version: number;
}

// options and secrets are the server-encrypted shape from before the account had a vault
interface ServiceDoc extends Item {
  keyId?: string;
  name: string;
  options?: Record<string, string>;
  sealed?: string;
  sealedToken?: string | null;
  secrets?: Record<string, Box>;
  tokenExpiry?: Record<string, Date>;
  type: string;
}

interface MountDoc extends Item {
  name: string;
  remotePath: string;
  serviceId: string;
  vfs: z.infer<typeof mountBody>["vfs"];
}

export interface Caller {
  deviceId: string;
  id: string;
  userId: string;
}

export interface IdContext {
  params: Promise<{ id: string }>;
}

export const services = () => db().collection<ServiceDoc>("services");
export const mounts = () => db().collection<MountDoc>("mounts");
export const users = () =>
  db().collection<{
    _id: ObjectId;
    email?: string | null;
    name?: string | null;
    rev?: number;
  }>("users");

export const seal = (c: Caller, secrets: Record<string, string>) =>
  Object.fromEntries(
    Object.entries(secrets).map(([field, value]) => [
      field,
      encrypt(value, aadFor(c.userId, c.id, field)),
    ])
  );

const opened = (doc: ServiceDoc) =>
  Object.fromEntries(
    Object.entries(doc.secrets ?? {}).map(([field, box]) => [
      field,
      decrypt(box, aadFor(doc.userId, doc._id, field)),
    ])
  );

export const serviceWire = (doc: ServiceDoc) => ({
  id: doc._id,
  name: doc.name,
  type: doc.type,
  options: doc.options ?? {},
  secrets: opened(doc),
  version: doc.version,
  updatedAt: doc.updatedAt.toISOString(),
});

const isLegacy = (doc: ServiceDoc) =>
  doc.options !== undefined || doc.secrets !== undefined;

// sealed on a device; legacy rides along until a device seals it, so the server stops decrypting
export const sealedWire = (doc: ServiceDoc) => ({
  id: doc._id,
  name: doc.name,
  type: doc.type,
  version: doc.version,
  updatedAt: doc.updatedAt.toISOString(),
  keyId: doc.keyId ?? null,
  sealed: doc.sealed ?? null,
  sealedToken: doc.sealedToken ?? null,
  tokenExpiry: doc.tokenExpiry?.token?.toISOString() ?? null,
  ...(isLegacy(doc)
    ? { legacy: { options: doc.options ?? {}, secrets: opened(doc) } }
    : {}),
});

export const mountWire = (doc: MountDoc) => ({
  id: doc._id,
  name: doc.name,
  serviceId: doc.serviceId,
  remotePath: doc.remotePath,
  vfs: doc.vfs,
  version: doc.version,
  createdAt: doc.createdAt?.toISOString(),
  updatedAt: doc.updatedAt.toISOString(),
});

export const vaultAware = (req: Request) =>
  req.headers.get("x-dokky-vault") === "1";

export const updateRequired = () =>
  apiError(
    426,
    "update_required",
    "Update dokky to keep syncing: npm i -g dokky"
  );

// dokky from before vaults syncs as it always did until the account has a vault
export async function legacyGate(
  req: Request,
  userId: string
): Promise<Response | null> {
  if (vaultAware(req)) {
    return null;
  }
  const locked = await vaults().countDocuments({ _id: userId }, { limit: 1 });
  return locked ? updateRequired() : null;
}

export async function caller(
  req: Request,
  params: IdContext["params"],
  opts: { removed?: "report" | "writeback" } = {}
): Promise<Caller | Response> {
  const who = await authDevice(req, new Date(), opts);
  if (who instanceof Response) {
    return who;
  }
  const { id } = await params;
  if (!entityId.safeParse(id).success) {
    return apiError(400, "bad_request", "id must be a uuid");
  }
  return { ...who, id };
}

export async function changed(
  req: Request,
  c: Caller,
  action: AuditAction
): Promise<void> {
  await users().updateOne(
    { _id: new ObjectId(c.userId) },
    { $inc: { rev: 1 } }
  );
  await audit(c.userId, c.deviceId, action, c.id, clientIp(req));
}

export const notFound = (kind: string) =>
  apiError(404, "not_found", `no such ${kind}`);

export async function save<T extends Item>(
  coll: Collection<T>,
  c: Caller,
  kind: string,
  baseVersion: number,
  fields: Omit<T, keyof Item>,
  wire: (doc: T) => unknown,
  unset: readonly Exclude<keyof T, keyof Item>[] = []
): Promise<T | Response> {
  const set = { ...fields, updatedAt: new Date(), updatedBy: c.deviceId };
  const own = { _id: c.id, userId: c.userId } as Filter<T>;
  try {
    if (baseVersion === 0) {
      const doc = {
        ...set,
        _id: c.id,
        userId: c.userId,
        version: 1,
        createdAt: set.updatedAt,
      } as T;
      await coll.insertOne(doc as OptionalUnlessRequiredId<T>);
      return doc;
    }
    const update = {
      $set: { ...set, version: baseVersion + 1 },
      ...(unset.length > 0
        ? { $unset: Object.fromEntries(unset.map((field) => [field, ""])) }
        : {}),
    } as UpdateFilter<T>;
    const doc = await coll.findOneAndUpdate(
      { ...own, version: baseVersion },
      update,
      { returnDocument: "after" }
    );
    if (doc) {
      return doc as T;
    }
  } catch (error) {
    if (!(error instanceof MongoServerError && error.code === 11_000)) {
      throw error;
    }
    if (!error.keyPattern?._id) {
      return apiError(409, "name_taken", `another ${kind} uses this name`);
    }
  }
  const current = await coll.findOne(own);
  if (!current) {
    return notFound(kind);
  }
  return Response.json(
    {
      error: {
        code: "conflict",
        message: `this ${kind} was saved elsewhere first`,
      },
      current: wire(current as T),
    },
    { status: 409 }
  );
}
