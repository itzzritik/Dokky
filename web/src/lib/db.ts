import { type Db, MongoClient } from "mongodb";

export const dbName = "dokky";

const cache = globalThis as typeof globalThis & { mongo?: MongoClient };

export function mongo(): MongoClient {
  if (!cache.mongo) {
    const uri = process.env.DATABASE_URL;
    if (!uri) {
      throw new Error("DATABASE_URL is not set");
    }
    const client = new MongoClient(uri);
    cache.mongo = client;
    // The driver keeps a closed topology after a failed connect, so drop the client.
    client.connect().catch(() => {
      if (cache.mongo === client) {
        cache.mongo = undefined;
      }
    });
  }
  return cache.mongo;
}

export const db = (): Db => mongo().db(dbName);

export async function ensureIndexes(): Promise<void> {
  const d = db();
  await Promise.all([
    d.collection("devices").createIndex({ tokenHash: 1 }, { unique: true }),
    d
      .collection("services")
      .createIndex({ userId: 1, name: 1 }, { unique: true }),
    d
      .collection("mounts")
      .createIndex({ userId: 1, name: 1 }, { unique: true }),
    d.collection("cliLogins").createIndexes([
      { key: { expiresAt: 1 }, expireAfterSeconds: 0 },
      { key: { deviceCodeHash: 1 }, unique: true },
      { key: { userCode: 1 }, unique: true },
    ]),
    d
      .collection("audit")
      .createIndex({ at: 1 }, { expireAfterSeconds: 31_536_000 }),
    d.collection("events").createIndexes([
      { key: { userId: 1, at: -1 } },
      { key: { userId: 1, key: 1, open: 1 } },
      {
        key: { userId: 1, clientId: 1 },
        unique: true,
        partialFilterExpression: { clientId: { $exists: true } },
      },
      { key: { at: 1 }, expireAfterSeconds: 7_776_000 },
    ]),
  ]);
}
