export async function register() {
  if (process.env.NEXT_RUNTIME === "nodejs") {
    const { ensureIndexes, migrateRemovals } = await import("@/lib/db");
    ensureIndexes().catch((error) => console.error("ensureIndexes", error));
    migrateRemovals().catch((error) => console.error("migrateRemovals", error));
  }
}
