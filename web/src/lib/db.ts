import { type Db, MongoClient } from "mongodb";

export const dbName = "dokky";

const cache = globalThis as typeof globalThis & { mongo?: MongoClient };

export function mongo(): MongoClient {
  if (!cache.mongo) {
    const uri = process.env.MONGODB_URI;
    if (!uri) {
      throw new Error("MONGODB_URI is not set");
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
    d
      .collection("cliLogins")
      .createIndex({ expiresAt: 1 }, { expireAfterSeconds: 0 }),
    d
      .collection("audit")
      .createIndex({ at: 1 }, { expireAfterSeconds: 31_536_000 }),
  ]);
}
