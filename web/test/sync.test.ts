import { afterAll, beforeAll, expect, test } from "bun:test";
import { createHash, randomBytes, randomUUID } from "node:crypto";
import { MongoMemoryServer } from "mongodb-memory-server";
import {
  DELETE as mountDelete,
  PUT as mountPut,
} from "@/app/api/v1/mounts/[id]/route";
import {
  DELETE as serviceDelete,
  PUT as servicePut,
} from "@/app/api/v1/services/[id]/route";
import { POST as tokenPost } from "@/app/api/v1/services/[id]/token/route";
import { GET as syncGet } from "@/app/api/v1/sync/route";
import { db, ensureIndexes, mongo } from "@/lib/db";
import { aadFor, decrypt } from "@/lib/secrets";

const k1 = randomBytes(32).toString("base64");

let server: MongoMemoryServer;

beforeAll(async () => {
  (globalThis as { mongo?: unknown }).mongo = undefined;
  server = await MongoMemoryServer.create();
  process.env.DATABASE_URL = server.getUri();
  process.env.ENCRYPTION_KEY = k1;
  await ensureIndexes();
}, 120_000);

afterAll(async () => {
  await mongo().close();
  await server.stop();
});

interface User {
  token: string;
  userId: string;
}

async function newUser(): Promise<User> {
  const { insertedId } = await db()
    .collection("users")
    .insertOne({ name: "someone" });
  const token = `dky_${randomBytes(32).toString("base64url")}`;
  const now = new Date();
  await db()
    .collection("devices")
    .insertOne({
      userId: String(insertedId),
      name: "mac",
      os: "darwin",
      arch: "arm64",
      version: "0.1.0",
      tokenHash: createHash("sha256").update(token).digest("hex"),
      createdAt: now,
      lastSeenAt: now,
      revokedAt: null,
    });
  return { token, userId: String(insertedId) };
}

type Handler = (
  req: Request,
  ctx: { params: Promise<{ id: string }> }
) => Promise<Response>;

function call(
  handler: Handler,
  user: User | null,
  id: string,
  body?: unknown,
  headers: Record<string, string> = {}
) {
  return handler(
    new Request("http://localhost/api/v1/x", {
      method: body === undefined ? "DELETE" : "PUT",
      headers: {
        "content-type": "application/json",
        "x-dokky-version": "0.1.0",
        ...(user ? { authorization: `Bearer ${user.token}` } : {}),
        ...headers,
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    }),
    { params: Promise.resolve({ id }) }
  );
}

const putService = (user: User, id: string, body: unknown) =>
  call(servicePut, user, id, body);
const delService = (user: User, id: string) => call(serviceDelete, user, id);
const putMount = (user: User, id: string, body: unknown) =>
  call(mountPut, user, id, body);
const delMount = (user: User, id: string) => call(mountDelete, user, id);
const writeToken = (user: User, id: string, body: unknown) =>
  call(tokenPost, user, id, body);

const sync = (user: User, headers: Record<string, string> = {}) =>
  syncGet(
    new Request("http://localhost/api/v1/sync", {
      headers: {
        authorization: `Bearer ${user.token}`,
        "x-dokky-version": "0.1.0",
        ...headers,
      },
    })
  );

async function snapshot(user: User) {
  const res = await sync(user);
  expect(res.status).toBe(200);
  return await res.json();
}

const rev = async (user: User) => (await snapshot(user)).rev as number;

const service = (fields: Record<string, unknown> = {}) => ({
  name: "Home NAS",
  type: "smb",
  options: { host: "nas.local", user: "ritik" },
  secrets: { pass: "hunter2" },
  baseVersion: 0,
  ...fields,
});

const vfs = {
  cacheMode: "full",
  dirCacheTime: "30s",
  readAhead: "2G",
  readChunkStreams: 4,
  cacheMaxSize: "50G",
  cacheMaxAge: "5m",
  cachePollInterval: "10s",
  cacheMinFreeSpace: "10G",
  fastFingerprint: true,
};

const mount = (serviceId: string, fields: Record<string, unknown> = {}) => ({
  name: "nas-media",
  serviceId,
  remotePath: "media/Films 2026",
  vfs,
  baseVersion: 0,
  ...fields,
});

const isoTime = expect.stringMatching(/^\d{4}-\d\d-\d\dT[\d:.]+Z$/u);

async function created(user: User, body = service()) {
  const id = randomUUID();
  const res = await putService(user, id, body);
  expect(res.status).toBe(200);
  return id;
}

async function errorOf(res: Response) {
  return { status: res.status, body: await res.json() };
}

test("create shows up in the snapshot with secrets decrypted and stored as boxes", async () => {
  const user = await newUser();
  expect(await snapshot(user)).toEqual({ rev: 0, services: [], mounts: [] });

  const id = randomUUID();
  const res = await putService(user, id, service());
  expect(res.status).toBe(200);
  const item = {
    id,
    name: "Home NAS",
    type: "smb",
    options: { host: "nas.local", user: "ritik" },
    secrets: { pass: "hunter2" },
    version: 1,
    updatedAt: isoTime,
  };
  expect(await res.json()).toEqual(item);

  const mountId = randomUUID();
  const m = await putMount(user, mountId, mount(id));
  expect(m.status).toBe(200);
  const mountItem = {
    id: mountId,
    name: "nas-media",
    serviceId: id,
    remotePath: "media/Films 2026",
    vfs,
    version: 1,
    createdAt: isoTime,
    updatedAt: isoTime,
  };
  expect(await m.json()).toEqual(mountItem);

  expect(await snapshot(user)).toEqual({
    rev: 2,
    services: [item],
    mounts: [mountItem],
  });

  const raw = await db()
    .collection("services")
    .findOne({ _id: id as never });
  expect(raw).toMatchObject({ userId: user.userId, version: 1 });
  expect(raw?.updatedAt).toBeInstanceOf(Date);
  expect(Object.keys(raw?.secrets.pass).sort()).toEqual(["c", "n"]);
  expect(decrypt(raw?.secrets.pass, aadFor(user.userId, id, "pass"))).toBe(
    "hunter2"
  );
  expect(JSON.stringify(raw)).not.toContain("hunter2");

  const actions = await db()
    .collection("audit")
    .find({ userId: user.userId })
    .map((a) => [a.action, a.target])
    .toArray();
  expect(actions).toEqual([
    ["service.put", id],
    ["mount.put", mountId],
  ]);
});

test("same rev answers 304 with no body, a change gives a new ETag", async () => {
  const user = await newUser();
  const first = await sync(user);
  expect(first.headers.get("etag")).toBe('"0"');
  expect(first.headers.get("cache-control")).toBe("no-store");

  for (const tag of ['"0"', 'W/"0"']) {
    // biome-ignore lint/performance/noAwaitInLoops: one request at a time
    const same = await sync(user, { "if-none-match": tag });
    expect(same.status).toBe(304);
    expect(await same.text()).toBe("");
  }

  await created(user);
  const changed = await sync(user, { "if-none-match": '"0"' });
  expect(changed.status).toBe(200);
  expect(changed.headers.get("etag")).toBe('"1"');
  expect((await changed.json()).rev).toBe(1);
  expect((await sync(user, { "if-none-match": '"1"' })).status).toBe(304);
});

test("matching baseVersion updates, a stale one gets 409 with the current item", async () => {
  const user = await newUser();
  const id = await created(user);

  const updated = await putService(
    user,
    id,
    service({ name: "NAS", secrets: { pass: "new" }, baseVersion: 1 })
  );
  expect(updated.status).toBe(200);
  expect(await updated.json()).toMatchObject({
    id,
    name: "NAS",
    secrets: { pass: "new" },
    version: 2,
  });
  const before = await rev(user);

  const current = {
    id,
    name: "NAS",
    type: "smb",
    options: { host: "nas.local", user: "ritik" },
    secrets: { pass: "new" },
    version: 2,
    updatedAt: isoTime,
  };
  for (const baseVersion of [1, 0, 3]) {
    // biome-ignore lint/performance/noAwaitInLoops: one request at a time
    const stale = await putService(
      user,
      id,
      service({ name: "Other", baseVersion })
    );
    expect(await errorOf(stale)).toEqual({
      status: 409,
      body: {
        error: { code: "conflict", message: expect.any(String) },
        current,
      },
    });
  }
  expect(await rev(user)).toBe(before);

  const mountId = randomUUID();
  await putMount(user, mountId, mount(id));
  const staleMount = await putMount(
    user,
    mountId,
    mount(id, { remotePath: "x", baseVersion: 2 })
  );
  const { status, body } = await errorOf(staleMount);
  expect(status).toBe(409);
  expect(body.error.code).toBe("conflict");
  expect(body.current).toMatchObject({
    id: mountId,
    remotePath: "media/Films 2026",
    version: 1,
  });
});

test("a name used by another id is name_taken, on create and on rename", async () => {
  const user = await newUser();
  const a = await created(user, service({ name: "nas" }));
  const b = await created(user, service({ name: "cloud" }));

  const clash = await putService(user, randomUUID(), service({ name: "nas" }));
  expect(await errorOf(clash)).toEqual({
    status: 409,
    body: { error: { code: "name_taken", message: expect.any(String) } },
  });
  const rename = await putService(
    user,
    b,
    service({ name: "nas", baseVersion: 1 })
  );
  expect((await errorOf(rename)).body.error.code).toBe("name_taken");

  await putMount(user, randomUUID(), mount(a, { name: "media" }));
  const m2 = randomUUID();
  await putMount(user, m2, mount(b, { name: "photos" }));
  const mountClash = await putMount(
    user,
    randomUUID(),
    mount(b, { name: "media" })
  );
  expect((await errorOf(mountClash)).body.error.code).toBe("name_taken");
  const mountRename = await putMount(
    user,
    m2,
    mount(b, { name: "media", baseVersion: 1 })
  );
  expect((await errorOf(mountRename)).body.error.code).toBe("name_taken");

  const other = await newUser();
  expect(
    (await putService(other, randomUUID(), service({ name: "nas" }))).status
  ).toBe(200);
});

test("deleting a service removes its mounts and bumps rev once", async () => {
  const user = await newUser();
  const doomed = await created(user, service({ name: "doomed" }));
  const kept = await created(user, service({ name: "kept" }));
  await putMount(user, randomUUID(), mount(doomed, { name: "a" }));
  await putMount(user, randomUUID(), mount(doomed, { name: "b" }));
  const keptMount = randomUUID();
  await putMount(user, keptMount, mount(kept, { name: "c" }));
  const before = await rev(user);

  const res = await delService(user, doomed);
  expect(res.status).toBe(204);
  const snap = await snapshot(user);
  expect(snap.rev).toBe(before + 1);
  expect(snap.services.map((s: { id: string }) => s.id)).toEqual([kept]);
  expect(snap.mounts.map((m: { id: string }) => m.id)).toEqual([keptMount]);
  expect(
    await db().collection("mounts").countDocuments({ serviceId: doomed })
  ).toBe(0);

  const again = await delService(user, doomed);
  expect((await errorOf(again)).body.error.code).toBe("not_found");
  expect(again.status).toBe(404);

  expect((await delMount(user, keptMount)).status).toBe(204);
  expect((await delMount(user, keptMount)).status).toBe(404);
  expect((await snapshot(user)).mounts).toEqual([]);

  const actions = await db()
    .collection("audit")
    .find({
      userId: user.userId,
      action: { $in: ["service.delete", "mount.delete"] },
    })
    .map((a) => [a.action, a.target])
    .toArray();
  expect(actions).toEqual([
    ["service.delete", doomed],
    ["mount.delete", keptMount],
  ]);
});

test("token write-back keeps the later expiry both ways", async () => {
  const user = await newUser();
  const id = await created(
    user,
    service({ secrets: { pass: "hunter2", token: "t0" } })
  );
  const t = (hour: number) =>
    `2026-11-01T${String(hour).padStart(2, "0")}:00:00Z`;
  const tokenOf = async () => (await snapshot(user)).services[0].secrets.token;

  const first = await writeToken(user, id, {
    field: "token",
    value: "t2",
    expiry: t(2),
  });
  expect(first.status).toBe(200);
  expect(await first.json()).toEqual({ kept: "new" });
  expect(await tokenOf()).toBe("t2");
  const afterNew = await rev(user);

  for (const expiry of [t(1), t(2), "2026-11-01T07:30:00+05:30"]) {
    // biome-ignore lint/performance/noAwaitInLoops: one request at a time
    const older = await writeToken(user, id, {
      field: "token",
      value: "old",
      expiry,
    });
    expect(await older.json()).toEqual({ kept: "existing" });
  }
  expect(await tokenOf()).toBe("t2");
  expect(await rev(user)).toBe(afterNew);

  const later = await writeToken(user, id, {
    field: "token",
    value: "t3",
    expiry: "2026-11-01T03:00:00.123456789Z",
  });
  expect(await later.json()).toEqual({ kept: "new" });
  expect(await rev(user)).toBe(afterNew + 1);
  const snap = await snapshot(user);
  expect(snap.services[0].secrets).toEqual({ pass: "hunter2", token: "t3" });
  expect(snap.services[0].version).toBe(3);

  const raw = await db()
    .collection("services")
    .findOne({ _id: id as never });
  expect(Object.keys(raw?.secrets.token).sort()).toEqual(["c", "n"]);
  expect(raw?.tokenExpiry).toEqual({
    token: new Date("2026-11-01T03:00:00.123Z"),
  });
  expect(JSON.stringify(raw)).not.toContain("t3");

  const fresh = await writeToken(user, id, {
    field: "refresh_token",
    value: "r1",
    expiry: t(0),
  });
  expect(await fresh.json()).toEqual({ kept: "new" });

  for (const body of [
    { field: "Token", value: "x", expiry: t(5) },
    { field: "token|pass", value: "x", expiry: t(5) },
    { field: "token", value: "x", expiry: "tomorrow" },
    { field: "token", value: "", expiry: t(5) },
    { field: "token", value: "x".repeat(16_385), expiry: t(5) },
  ]) {
    // biome-ignore lint/performance/noAwaitInLoops: one request at a time
    const bad = await writeToken(user, id, body);
    expect((await errorOf(bad)).body.error.code).toBe("bad_request");
  }

  const audits = await db()
    .collection("audit")
    .countDocuments({ userId: user.userId, action: "token.writeback" });
  expect(audits).toBe(3);

  const missing = await writeToken(user, randomUUID(), {
    field: "token",
    value: "x",
    expiry: t(9),
  });
  expect(missing.status).toBe(404);
});

test("a kept write-back bumps the version, so a stale edit gets 409", async () => {
  const user = await newUser();
  const id = await created(
    user,
    service({ secrets: { pass: "hunter2", token: "t1" } })
  );
  await db()
    .collection("services")
    .updateOne(
      { _id: id as never },
      { $set: { updatedAt: new Date("2020-01-01T00:00:00Z") } }
    );

  const kept = await writeToken(user, id, {
    field: "token",
    value: "t2",
    expiry: "2026-11-01T02:00:00Z",
  });
  expect(await kept.json()).toEqual({ kept: "new" });
  const {
    services: [after],
  } = await snapshot(user);
  expect(after.version).toBe(2);
  expect(after.updatedAt).not.toBe("2020-01-01T00:00:00.000Z");

  const stale = await writeToken(user, id, {
    field: "token",
    value: "old",
    expiry: "2026-11-01T01:00:00Z",
  });
  expect(await stale.json()).toEqual({ kept: "existing" });
  expect((await snapshot(user)).services[0]).toEqual(after);

  const edit = await putService(
    user,
    id,
    service({
      name: "Edited",
      secrets: { pass: "hunter2", token: "t1" },
      baseVersion: 1,
    })
  );
  expect(await errorOf(edit)).toEqual({
    status: 409,
    body: {
      error: { code: "conflict", message: expect.any(String) },
      current: after,
    },
  });
  expect((await snapshot(user)).services[0].secrets.token).toBe("t2");
});

test("deleting a service clears its orphan mounts even if the service is gone", async () => {
  const user = await newUser();
  const gone = randomUUID();
  await db()
    .collection("mounts")
    .insertOne({
      _id: randomUUID() as never,
      userId: user.userId,
      name: "orphan",
      serviceId: gone,
      remotePath: "",
      vfs,
      version: 1,
      updatedAt: new Date(),
      updatedBy: "elsewhere",
    });
  const before = await rev(user);

  expect((await delService(user, gone)).status).toBe(204);
  expect(
    await db().collection("mounts").countDocuments({ serviceId: gone })
  ).toBe(0);
  expect(await rev(user)).toBe(before + 1);

  expect((await delService(user, gone)).status).toBe(404);
  expect(await rev(user)).toBe(before + 1);
});

test("another user's ids are never revealed or touched", async () => {
  const owner = await newUser();
  const id = await created(owner, service({ secrets: { token: "mine" } }));
  const mountId = randomUUID();
  await putMount(owner, mountId, mount(id));
  const ownerSnap = await snapshot(owner);

  const thief = await newUser();
  const notFound = async (res: Promise<Response>) =>
    expect(await errorOf(await res)).toEqual({
      status: 404,
      body: { error: { code: "not_found", message: expect.any(String) } },
    });

  await notFound(putService(thief, id, service({ name: "stolen" })));
  await notFound(putService(thief, id, service({ baseVersion: 1 })));
  await notFound(delService(thief, id));
  await notFound(
    writeToken(thief, id, {
      field: "token",
      value: "evil",
      expiry: "2099-01-01T00:00:00Z",
    })
  );
  await notFound(delMount(thief, mountId));

  const ownId = await created(thief, service({ name: "thief" }));
  await notFound(putMount(thief, mountId, mount(ownId, { name: "x" })));
  await notFound(
    putMount(thief, mountId, mount(ownId, { name: "x", baseVersion: 1 }))
  );

  const foreign = await putMount(thief, randomUUID(), mount(id));
  expect(await errorOf(foreign)).toEqual({
    status: 400,
    body: { error: { code: "bad_request", message: expect.any(String) } },
  });

  const thiefSnap = await snapshot(thief);
  expect(thiefSnap.services.map((s: { id: string }) => s.id)).toEqual([ownId]);
  expect(thiefSnap.mounts).toEqual([]);
  expect(await snapshot(owner)).toEqual(ownerSnap);
});

test("concurrent saves on the same baseVersion: exactly one wins", async () => {
  const user = await newUser();
  const id = await created(user);
  const updates = await Promise.all(
    ["one", "two", "three"].map((name) =>
      putService(user, id, service({ name, baseVersion: 1 }))
    )
  );
  expect(updates.map((r) => r.status).sort()).toEqual([200, 409, 409]);
  const conflict = (version: number, itemId: string) => ({
    error: { code: "conflict", message: expect.any(String) },
    current: expect.objectContaining({ id: itemId, version }),
  });
  expect(
    await Promise.all(
      updates.filter((r) => r.status === 409).map((r) => r.json())
    )
  ).toEqual([conflict(2, id), conflict(2, id)]);

  const fresh = randomUUID();
  const creates = await Promise.all(
    ["a", "b"].map((name) => putService(user, fresh, service({ name })))
  );
  expect(creates.map((r) => r.status).sort()).toEqual([200, 409]);
  expect(
    await Promise.all(
      creates.filter((r) => r.status === 409).map((r) => r.json())
    )
  ).toEqual([conflict(1, fresh)]);

  const snap = await snapshot(user);
  expect(snap.services).toHaveLength(2);
  expect(snap.rev).toBe(3);
});

test("routes need a device token, a uuid id and a valid body", async () => {
  const user = await newUser();
  const id = randomUUID();
  expect((await call(servicePut, null, id, service())).status).toBe(401);
  expect((await call(mountDelete, null, id)).status).toBe(401);
  expect((await call(tokenPost, null, id, {})).status).toBe(401);
  expect(
    (await syncGet(new Request("http://localhost/api/v1/sync"))).status
  ).toBe(401);

  const bad = await Promise.all([
    putService(user, "not-a-uuid", service()),
    delService(user, `${id}|x`),
    putMount(user, "1", mount(id)),
    writeToken(user, "x", {
      field: "token",
      value: "x",
      expiry: "2026-11-01T00:00:00Z",
    }),
    putService(user, id, service({ secrets: { "pass|x": "a" } })),
    putMount(user, id, mount(id, { name: "Bad Name" })),
  ]);
  for (const res of bad) {
    expect(res.status).toBe(400);
  }
  const codes = await Promise.all(
    bad.map(async (res) => (await res.json()).error.code)
  );
  expect(new Set(codes)).toEqual(new Set(["bad_request"]));
  expect(await rev(user)).toBe(0);
});
