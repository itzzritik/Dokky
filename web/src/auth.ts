import { MongoDBAdapter } from "@auth/mongodb-adapter";
import { ObjectId } from "mongodb";
import NextAuth from "next-auth";
import GitHub from "next-auth/providers/github";
import { db, dbName, mongo } from "@/lib/db";

export const { handlers, auth, signIn, signOut } = NextAuth({
  adapter: MongoDBAdapter(mongo, { databaseName: dbName }),
  providers: [
    GitHub({
      clientId: process.env.GITHUB_CLIENT_ID,
      clientSecret: process.env.GITHUB_CLIENT_SECRET,
    }),
  ],
  session: { strategy: "database" },
  events: {
    // removing a lost or stolen device needs a sign-in from the last few minutes
    async signIn({ user }) {
      if (user.id && ObjectId.isValid(user.id)) {
        await db()
          .collection("users")
          .updateOne(
            { _id: new ObjectId(user.id) },
            { $set: { signedInAt: new Date() } }
          );
      }
    },
  },
});
