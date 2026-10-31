import { afterAll, beforeAll, beforeEach, expect, test } from "bun:test";
import { randomBytes } from "node:crypto";
import { MongoMemoryServer } from "mongodb-memory-server";
import { db, mongo } from "@/lib/db";
import { aadFor, decrypt, encrypt } from "@/lib/secrets";
import { rotateKeys } from "../scripts/rotate-keys";

const reKeys = /DOKKY_SECRET_KEYS/u;
const reJson = /JSON/u;
const reBytes = /32 bytes/u;
const reCurrent = /DOKKY_SECRET_KEY_CURRENT/u;

const key = () => randomBytes(32).toString("base64");
const k1 = key();
const k2 = key();

const setKeys = (current: string, keys: Record<string, string>) => {
  process.env.DOKKY_SECRET_KEYS = JSON.stringify(keys);
  process.env.DOKKY_SECRET_KEY_CURRENT = current;
};

beforeEach(() => setKeys("1", { "1": k1 }));

const aad = aadFor("u1", "s1", "password");

test("round trip", () => {
  const box = encrypt("hunter2", aad);
  expect(box.v).toBe(1);
  expect(Buffer.from(box.n, "base64")).toHaveLength(12);
  expect(decrypt(box, aad)).toBe("hunter2");
});

test("aad format", () => {
  expect(aad).toBe("u1|s1|password");
});

test("another field's aad throws", () => {
  const box = encrypt("hunter2", aad);
  expect(() => decrypt(box, aadFor("u1", "s1", "token"))).toThrow();
  expect(() => decrypt(box, aadFor("u2", "s1", "password"))).toThrow();
});

test("tampered ciphertext throws", () => {
  const box = encrypt("hunter2", aad);
  const c = Buffer.from(box.c, "base64");
  c[0] = (c[0] ?? 0) === 0 ? 1 : 0;
  expect(() => decrypt({ ...box, c: c.toString("base64") }, aad)).toThrow();
});

test("unknown version throws", () => {
  const box = encrypt("hunter2", aad);
  expect(() => decrypt({ ...box, v: 9 }, aad)).toThrow();
});

test("old version still decrypts after current changes", () => {
  const box = encrypt("hunter2", aad);
  setKeys("2", { "1": k1, "2": k2 });
  expect(decrypt(box, aad)).toBe("hunter2");
  expect(encrypt("x", aad).v).toBe(2);
});

test("nonces differ for equal plaintexts", () => {
  expect(encrypt("a", aad).n).not.toBe(encrypt("a", aad).n);
});

test("config errors are clear", () => {
  process.env.DOKKY_SECRET_KEYS = "";
  expect(() => encrypt("a", aad)).toThrow(reKeys);
  process.env.DOKKY_SECRET_KEYS = "{nope";
  expect(() => encrypt("a", aad)).toThrow(reJson);
  setKeys("1", { "1": randomBytes(16).toString("base64") });
  expect(() => encrypt("a", aad)).toThrow(reBytes);
  setKeys("7", { "1": k1 });
  expect(() => encrypt("a", aad)).toThrow(reCurrent);
});

let server: MongoMemoryServer;

beforeAll(async () => {
  (globalThis as { mongo?: unknown }).mongo = undefined;
  server = await MongoMemoryServer.create();
  process.env.MONGODB_URI = server.getUri();
}, 120_000);

afterAll(async () => {
  await mongo().close();
  await server.stop();
});

test("rotate-keys moves old boxes to the current version, idempotently", async () => {
  const services = db().collection("services");
  const field = aadFor("u1", "s1", "password");
  const old = encrypt("hunter2", field);
  await services.insertOne({
    _id: "s1" as never,
    userId: "u1",
    secrets: { password: old },
  });
  await services.insertOne({ _id: "s2" as never, userId: "u1" });

  setKeys("2", { "1": k1, "2": k2 });
  expect(await rotateKeys()).toEqual({ changed: 0, rotated: 1, skipped: 0 });

  const doc = await services.findOne({ _id: "s1" as never });
  const box = doc?.secrets.password;
  expect(box.v).toBe(2);
  expect(decrypt(box, field)).toBe("hunter2");

  expect(await rotateKeys()).toEqual({ changed: 0, rotated: 0, skipped: 1 });
});

test("config errors never contain key text", () => {
  const messages: string[] = [];
  const attempt = () => {
    try {
      encrypt("a", aad);
    } catch (error) {
      const e = error as Error;
      messages.push(e.message, String(e.cause ?? ""));
    }
  };
  process.env.DOKKY_SECRET_KEYS = k1;
  attempt();
  setKeys("1", { [k1]: "1" });
  attempt();
  setKeys("1", { v1: k1 });
  attempt();
  process.env.DOKKY_SECRET_KEY_CURRENT = "";
  setKeys("", { "1": k1 });
  attempt();
  expect(messages.filter(Boolean).length).toBeGreaterThanOrEqual(4);
  for (const m of messages) {
    expect(m).not.toContain(k1);
  }
});

test("non-numeric versions are rejected", () => {
  setKeys("v1", { v1: k1 });
  expect(() => encrypt("a", aad)).toThrow();
  setKeys("1", { v1: k1, "1": k1 });
  expect(() => encrypt("a", aad)).toThrow();
});

test("rotate skips a box changed between read and write", async () => {
  const services = db().collection("services");
  const field = aadFor("u1", "s3", "token");
  await services.insertOne({
    _id: "s3" as never,
    userId: "u1",
    secrets: { token: encrypt("old", field) },
  });
  setKeys("2", { "1": k1, "2": k2 });
  const concurrent = encrypt("new", field);
  const result = await rotateKeys(async () => {
    await services.updateOne(
      { _id: "s3" as never },
      { $set: { "secrets.token": concurrent } }
    );
  });
  expect(result.changed).toBe(1);
  const doc = await services.findOne({ _id: "s3" as never });
  expect(doc?.secrets.token).toEqual(concurrent);
});
