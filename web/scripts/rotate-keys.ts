import { db, mongo } from "../src/lib/db";
import {
  aadFor,
  type Box,
  currentVersion,
  decrypt,
  encrypt,
} from "../src/lib/secrets";

export async function rotateKeys(
  afterRead?: () => Promise<void>
): Promise<{ changed: number; rotated: number; skipped: number }> {
  const current = currentVersion();
  const services = db().collection("services");
  let rotated = 0;
  let skipped = 0;
  let changed = 0;
  for await (const doc of services.find({ secrets: { $type: "object" } })) {
    const pending: [string, Box, Box][] = [];
    for (const [field, box] of Object.entries(
      doc.secrets as Record<string, Box>
    )) {
      if (box.v === current) {
        skipped += 1;
        continue;
      }
      const aad = aadFor(String(doc.userId), String(doc._id), field);
      try {
        pending.push([field, box, encrypt(decrypt(box, aad), aad)]);
      } catch (error) {
        // biome-ignore lint/style/useErrorCause: the original message is folded in
        throw new Error(
          `cannot rotate service ${String(doc._id)} field ${field}: ${error instanceof Error ? error.message : "failed"}`
        );
      }
    }
    await afterRead?.();
    for (const [field, old, fresh] of pending) {
      // biome-ignore lint/performance/noAwaitInLoops: guarded writes run one at a time
      const res = await services.updateOne(
        { _id: doc._id, [`secrets.${field}.n`]: old.n },
        { $set: { [`secrets.${field}`]: fresh } }
      );
      if (res.matchedCount) {
        rotated += 1;
      } else {
        changed += 1;
      }
    }
  }
  return { changed, rotated, skipped };
}

if (import.meta.main) {
  try {
    const { rotated, skipped, changed } = await rotateKeys();
    console.log(
      `rotated ${rotated}, already current ${skipped}, changed during run ${changed} (re-run if above 0)`
    );
  } finally {
    await mongo().close();
  }
}
