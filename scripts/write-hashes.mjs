import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const [version, dist] = process.argv.slice(2);
if (!version || !dist) {
  console.error("usage: write-hashes.mjs <version> <dist>");
  process.exit(1);
}

const pkgPath = new URL("../npm/package.json", import.meta.url);
const pkg = JSON.parse(readFileSync(pkgPath, "utf8"));
const base = `https://github.com/itzzritik/dokky/releases/download/v${version}/`;

const binaries = {};
for (const line of readFileSync(join(dist, "checksums.txt"), "utf8").split("\n")) {
  const [sha256, file] = line.trim().split(/\s+/);
  const m = /^dokky-(darwin|linux|windows)-(amd64|arm64)(\.exe)?$/.exec(file || "");
  if (m) binaries[`${m[1]}-${m[2]}`] = { url: base + file, sha256 };
}

const count = Object.keys(binaries).length;
if (count !== 6) {
  console.error(`expected 6 binaries in checksums.txt, found ${count}`);
  process.exit(1);
}

pkg.version = version;
pkg.dokky = { ...pkg.dokky, binaries };
writeFileSync(pkgPath, JSON.stringify(pkg, null, 2) + "\n");
