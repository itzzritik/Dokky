import { MongoDBAdapter } from "@auth/mongodb-adapter";
import NextAuth from "next-auth";
import GitHub from "next-auth/providers/github";
import { dbName, mongo } from "@/lib/db";

export const { handlers, auth, signIn, signOut } = NextAuth({
  adapter: MongoDBAdapter(mongo, { databaseName: dbName }),
  providers: [GitHub],
  session: { strategy: "database" },
});
