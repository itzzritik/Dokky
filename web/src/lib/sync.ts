import {
  type Collection,
  type Filter,
  MongoServerError,
  ObjectId,
  type OptionalUnlessRequiredId,
} from "mongodb";
import type { z } from "zod";
import { type AuditAction, audit, clientIp } from "@/lib/audit";
import { db } from "@/lib/db";
import { authDevice } from "@/lib/device-auth";
import { apiError } from "@/lib/errors";
import { entityId, type mountBody } from "@/lib/schemas";
import { aadFor, type Box, decrypt, encrypt } from "@/lib/secrets";

interface Item {
  _id: string;
  createdAt?: Date;
  updatedAt: Date;
  updatedBy: string;
  userId: string;
  version: number;
}

interface ServiceDoc extends Item {
  name: string;
  options: Record<string, string>;
  secrets: Record<string, Box>;
  tokenExpiry?: Record<string, Date>;
  type: string;
}

interface MountDoc extends Item {
  name: string;
  remotePath: string;
  serviceId: string;
  vfs: z.infer<typeof mountBody>["vfs"];
}

interface Caller {
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

export const serviceWire = (doc: ServiceDoc) => ({
  id: doc._id,
  name: doc.name,
  type: doc.type,
  options: doc.options,
  secrets: Object.fromEntries(
    Object.entries(doc.secrets).map(([field, box]) => [
      field,
      decrypt(box, aadFor(doc.userId, doc._id, field)),
    ])
  ),
  version: doc.version,
  updatedAt: doc.updatedAt.toISOString(),
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

export async function caller(
  req: Request,
  params: IdContext["params"]
): Promise<Caller | Response> {
  const who = await authDevice(req);
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
  wire: (doc: T) => unknown
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
    const doc = await coll.findOneAndUpdate(
      { ...own, version: baseVersion },
      { $set: { ...set, version: baseVersion + 1 } as Partial<T> },
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
