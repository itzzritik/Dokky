import { ObjectId } from "mongodb";
import { db } from "@/lib/db";

const FRESH_MS = 300_000;

export async function freshSignIn(
  userId: string,
  now = new Date()
): Promise<boolean> {
  if (!ObjectId.isValid(userId)) {
    return false;
  }
  const user = await db()
    .collection<{ _id: ObjectId; signedInAt?: Date }>("users")
    .findOne({ _id: new ObjectId(userId) }, { projection: { signedInAt: 1 } });
  return (
    !!user?.signedInAt && now.getTime() - user.signedInAt.getTime() < FRESH_MS
  );
}
