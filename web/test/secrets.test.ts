import { beforeEach, expect, test } from "bun:test";
import { randomBytes } from "node:crypto";
import { aadFor, decrypt, encrypt } from "@/lib/secrets";

const reKey = /ENCRYPTION_KEY is not set/u;
const reBytes = /32 bytes/u;
const reMalformed = /malformed/u;

const key = () => randomBytes(32).toString("base64");
const k1 = key();

beforeEach(() => {
  process.env.ENCRYPTION_KEY = k1;
});

const aad = aadFor("u1", "s1", "password");

test("round trip", () => {
  const box = encrypt("hunter2", aad);
  expect(Object.keys(box).sort()).toEqual(["c", "n"]);
  expect(Buffer.from(box.n, "base64")).toHaveLength(12);
  expect(Buffer.from(box.c, "base64")).toHaveLength(7 + 16);
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

test("a different key throws", () => {
  const box = encrypt("hunter2", aad);
  process.env.ENCRYPTION_KEY = key();
  expect(() => decrypt(box, aad)).toThrow();
});

test("malformed boxes throw", () => {
  const box = encrypt("hunter2", aad);
  expect(() => decrypt({ ...box, n: "AAAA" }, aad)).toThrow(reMalformed);
  expect(() => decrypt({ ...box, c: "AAAA" }, aad)).toThrow(reMalformed);
});

test("nonces differ for equal plaintexts", () => {
  expect(encrypt("a", aad).n).not.toBe(encrypt("a", aad).n);
});

test("config errors are clear", () => {
  process.env.ENCRYPTION_KEY = "";
  expect(() => encrypt("a", aad)).toThrow(reKey);
  process.env.ENCRYPTION_KEY = randomBytes(16).toString("base64");
  expect(() => encrypt("a", aad)).toThrow(reBytes);
});

test("config errors never contain key text", () => {
  const short = randomBytes(16).toString("base64");
  process.env.ENCRYPTION_KEY = short;
  expect(() => encrypt("a", aad)).toThrow();
  try {
    encrypt("a", aad);
  } catch (error) {
    const e = error as Error;
    expect(e.message).not.toContain(short);
    expect(String(e.cause ?? "")).not.toContain(short);
  }
});

test("the key is read lazily", () => {
  const box = encrypt("x", aad);
  process.env.ENCRYPTION_KEY = key();
  expect(() => decrypt(box, aad)).toThrow();
  process.env.ENCRYPTION_KEY = k1;
  expect(decrypt(box, aad)).toBe("x");
});
