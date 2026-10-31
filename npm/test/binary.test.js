"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { platformKey, binaryPath, stateDir, ensureBinary, resolveBinary, recordInstall } = require("../lib/binary");

const sha = (s) => crypto.createHash("sha256").update(s).digest("hex");
const tmp = () => fs.mkdtempSync(path.join(os.tmpdir(), "dokky-npm-"));
const fakeFetch = (body, calls = []) => async (url) => {
  calls.push(url);
  return { ok: true, status: 200, arrayBuffer: async () => Buffer.from(body) };
};
const pkgFor = (body, version = "1.0.0") => ({
  version,
  dokky: { binaries: { "linux-amd64": { url: "http://x/dokky", sha256: sha(body) } } },
});
const args = (home, extra) => ({ home, platform: "linux", arch: "x64", env: {}, ...extra });

test("platform keys", () => {
  assert.equal(platformKey("darwin", "arm64"), "darwin-arm64");
  assert.equal(platformKey("win32", "x64"), "windows-amd64");
  assert.equal(platformKey("linux", "ia32"), null);
});

test("binary paths", () => {
  assert.equal(binaryPath("/h", "linux"), path.join("/h", ".local", "bin", "dokky"));
  assert.equal(binaryPath("/h", "win32"), path.join("/h", ".local", "bin", "dokky.exe"));
});

test("stateDir per platform", () => {
  assert.equal(stateDir({ env: {}, home: "/h", platform: "darwin" }), path.join("/h", "Library", "Application Support", "dokky"));
  assert.equal(stateDir({ env: { XDG_CONFIG_HOME: "/x" }, home: "/h", platform: "linux" }), path.join("/x", "dokky"));
  assert.equal(stateDir({ env: {}, home: "/h", platform: "linux" }), path.join("/h", ".config", "dokky"));
  assert.equal(stateDir({ env: { LocalAppData: "C:\\L" }, home: "/h", platform: "win32" }), path.join("C:\\L", "dokky"));
});

test("downloads, verifies, installs and records version", async () => {
  const home = tmp();
  const bin = await ensureBinary(args(home, { pkg: pkgFor("y"), fetchImpl: fakeFetch("y") }));
  assert.equal(fs.readFileSync(bin, "utf8"), "y");
  assert.equal(fs.statSync(bin).mode & 0o777, 0o755);
  assert.equal(fs.readFileSync(path.join(stateDir({ env: {}, home, platform: "linux" }), "version"), "utf8"), "1.0.0");
  const calls = [];
  await ensureBinary(args(home, { pkg: pkgFor("y"), fetchImpl: fakeFetch("y", calls) }));
  assert.equal(calls.length, 0);
});

test("rejects hash mismatch and leaves no file", async () => {
  const home = tmp();
  await assert.rejects(ensureBinary(args(home, { pkg: pkgFor("y"), fetchImpl: fakeFetch("x") })), /sha256 mismatch/);
  assert.equal(fs.existsSync(binaryPath(home, "linux")), false);
});

test("never replaces a newer binary", async () => {
  const home = tmp();
  await ensureBinary(args(home, { pkg: pkgFor("y", "2.0.0"), fetchImpl: fakeFetch("y") }));
  const calls = [];
  await ensureBinary(args(home, { pkg: pkgFor("z", "1.0.0"), fetchImpl: fakeFetch("z", calls) }));
  assert.equal(calls.length, 0);
  assert.equal(fs.readFileSync(binaryPath(home, "linux"), "utf8"), "y");
});

test("upgrades an older binary", async () => {
  const home = tmp();
  await ensureBinary(args(home, { pkg: pkgFor("y", "1.0.0"), fetchImpl: fakeFetch("y") }));
  await ensureBinary(args(home, { pkg: pkgFor("z", "1.1.0"), fetchImpl: fakeFetch("z") }));
  assert.equal(fs.readFileSync(binaryPath(home, "linux"), "utf8"), "z");
});

test("rejects clearly when no binary for the platform", async () => {
  const pkg = { version: "0.0.1", dokky: { binaries: {} } };
  await assert.rejects(ensureBinary(args(tmp(), { pkg, fetchImpl: fakeFetch("") })), /no binary for linux\/x64/);
});

test("recordInstall is idempotent", () => {
  const dir = tmp();
  recordInstall(dir, "/a");
  recordInstall(dir, "/a");
  recordInstall(dir, "/b");
  assert.deepEqual(JSON.parse(fs.readFileSync(path.join(dir, "installs.json"), "utf8")), ["/a", "/b"]);
});

test("resolveBinary falls back to the installed binary when the upgrade fails", async () => {
  const home = tmp();
  await ensureBinary(args(home, { pkg: pkgFor("y", "1.0.0"), fetchImpl: fakeFetch("y") }));
  const warnings = [];
  const failing = async () => {
    throw new Error("offline");
  };
  const bin = await resolveBinary(args(home, { pkg: pkgFor("z", "1.1.0"), fetchImpl: failing }), (m) => warnings.push(m));
  assert.equal(bin, binaryPath(home, "linux"));
  assert.match(warnings[0], /offline/);
  await assert.rejects(resolveBinary(args(tmp(), { pkg: pkgFor("z"), fetchImpl: failing })), /offline/);
});
