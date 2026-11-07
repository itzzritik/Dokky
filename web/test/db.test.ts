import { afterAll, beforeAll, expect, test } from "bun:test";
import type { MongoClient } from "mongodb";
import { MongoMemoryServer } from "mongodb-memory-server";
import { db, ensureIndexes, mongo } from "@/lib/db";

let server: MongoMemoryServer;

beforeAll(async () => {
  server = await MongoMemoryServer.create();
  process.env.DATABASE_URL = server.getUri();
}, 120_000);

afterAll(async () => {
  await mongo().close();
  await server.stop();
});

test("a failed connect is not cached", async () => {
  const cache = globalThis as { mongo?: MongoClient };
  await cache.mongo?.close();
  cache.mongo = undefined;
  process.env.DATABASE_URL =
    "mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=200";
  await expect(db().collection("probe").findOne()).rejects.toThrow();

  process.env.DATABASE_URL = server.getUri();
  expect(await db().collection("probe").findOne()).toBeNull();
});

interface Index {
  expireAfterSeconds?: number;
  key: Record<string, unknown>;
  name?: string;
  unique?: boolean;
}

const indexes = async (collection: string): Promise<Index[]> =>
  (await db().collection(collection).indexes())
    .filter((i) => i.name !== "_id_")
    .map(({ name, key, unique, expireAfterSeconds }) => ({
      name,
      key,
      unique,
      expireAfterSeconds,
    }));

test("ensureIndexes creates the unique and TTL indexes", async () => {
  await ensureIndexes();
  await ensureIndexes();

  expect(db().databaseName).toBe("dokky");
  expect(db().client).toBe(mongo());
  expect(await indexes("devices")).toEqual([
    { name: "tokenHash_1", key: { tokenHash: 1 }, unique: true },
    {
      name: "erasedAt_1",
      key: { erasedAt: 1 },
      expireAfterSeconds: 7_776_000,
    },
  ]);
  expect(await indexes("services")).toEqual([
    { name: "userId_1_name_1", key: { userId: 1, name: 1 }, unique: true },
  ]);
  expect(await indexes("mounts")).toEqual([
    { name: "userId_1_name_1", key: { userId: 1, name: 1 }, unique: true },
  ]);
  expect(await indexes("cliLogins")).toEqual([
    { name: "expiresAt_1", key: { expiresAt: 1 }, expireAfterSeconds: 0 },
    { name: "deviceCodeHash_1", key: { deviceCodeHash: 1 }, unique: true },
    { name: "userCode_1", key: { userCode: 1 }, unique: true },
  ]);
  expect(await indexes("audit")).toEqual([
    { name: "at_1", key: { at: 1 }, expireAfterSeconds: 31_536_000 },
  ]);
});
