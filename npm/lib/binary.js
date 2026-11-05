"use strict";
const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");

const ARCHES = { x64: "amd64", arm64: "arm64" };
const OSES = { darwin: "darwin", linux: "linux", win32: "windows" };

function platformKey(platform, arch) {
  const os = OSES[platform];
  const cpu = ARCHES[arch];
  return os && cpu ? `${os}-${cpu}` : null;
}

function binaryPath(home, platform) {
  const name = platform === "win32" ? "dokky.exe" : "dokky";
  return path.join(home, ".local", "bin", name);
}

function stateDir({ env, home, platform }) {
  if (platform === "darwin") return path.join(home, "Library", "Application Support", "dokky");
  if (platform === "win32") return path.join(env.LocalAppData || path.join(home, "AppData", "Local"), "dokky");
  return path.join(env.XDG_CONFIG_HOME || path.join(home, ".config"), "dokky");
}

function cmp(a, b) {
  const x = String(a).split(/[.-]/).map(Number);
  const y = String(b).split(/[.-]/).map(Number);
  for (let i = 0; i < 3; i++) {
    const d = (x[i] || 0) - (y[i] || 0);
    if (d) return d;
  }
  return 0;
}

function installedVersion(dir) {
  try {
    return fs.readFileSync(path.join(dir, "version"), "utf8").trim();
  } catch {
    return null;
  }
}

// exact: npm just installed this version, so the binary follows it, older or pinned or not
async function ensureBinary({ pkg, home, platform, arch, env = process.env, fetchImpl = fetch, rename = fs.renameSync, exact = false }) {
  const bin = binaryPath(home, platform);
  const dir = stateDir({ env, home, platform });
  const have = installedVersion(dir);
  const pinned = fs.existsSync(path.join(dir, "pin"));
  const keep = exact ? have === pkg.version : pinned || (have && cmp(have, pkg.version) >= 0);
  if (fs.existsSync(bin) && keep) return bin;

  const key = platformKey(platform, arch);
  const entry = key && pkg.dokky && pkg.dokky.binaries && pkg.dokky.binaries[key];
  if (!entry) throw new Error(`dokky ${pkg.version} has no binary for ${platform}/${arch}`);

  const res = await fetchImpl(entry.url);
  if (!res.ok) throw new Error(`download failed: HTTP ${res.status}`);
  const data = Buffer.from(await res.arrayBuffer());
  const sum = crypto.createHash("sha256").update(data).digest("hex");
  if (sum !== String(entry.sha256).toLowerCase()) throw new Error(`sha256 mismatch for ${entry.url}`);

  fs.mkdirSync(path.dirname(bin), { recursive: true });
  fs.mkdirSync(dir, { recursive: true });
  const tmp = `${bin}.${process.pid}.tmp`;
  try {
    fs.writeFileSync(tmp, data, { mode: 0o755 });
    fs.chmodSync(tmp, 0o755);
    replace(tmp, bin, path.join(dir, "dokky.old"), platform === "win32" && fs.existsSync(bin), rename);
  } finally {
    fs.rmSync(tmp, { force: true });
  }
  fs.writeFileSync(path.join(dir, "version"), pkg.version);
  return bin;
}

// Windows can rename a running exe but not replace it, so the old one moves aside first
function replace(tmp, bin, old, aside, rename) {
  if (!aside) return rename(tmp, bin);
  rename(bin, old);
  try {
    rename(tmp, bin);
  } catch (err) {
    rename(old, bin);
    throw err;
  }
}

async function resolveBinary(opts, warn = console.warn) {
  try {
    return await ensureBinary(opts);
  } catch (err) {
    const bin = binaryPath(opts.home, opts.platform);
    if (!fs.existsSync(bin)) throw err;
    warn(`dokky: ${err.message}; using installed binary`);
    return bin;
  }
}

function recordInstall(dir, pkgDir) {
  const file = path.join(dir, "installs.json");
  let list = [];
  try {
    list = JSON.parse(fs.readFileSync(file, "utf8"));
  } catch {
    list = [];
  }
  if (!Array.isArray(list)) list = [];
  if (list.includes(pkgDir)) return;
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(file, JSON.stringify([...list, pkgDir], null, 2));
}

module.exports = { platformKey, binaryPath, stateDir, ensureBinary, resolveBinary, recordInstall };
