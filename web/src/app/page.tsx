import { auth, signIn, signOut } from "@/auth";
import { Button } from "@/components/ui/button";

async function signInWithGitHub() {
  "use server";
  await signIn("github");
}

async function signOutOfDokky() {
  "use server";
  await signOut();
}

export default async function Home() {
  const session = await auth();

  return (
    <main className="flex min-h-svh flex-col items-center justify-center gap-6 p-6">
      <h1 className="text-2xl font-semibold">dokky</h1>
      {session?.user ? (
        <form
          action={signOutOfDokky}
          className="flex flex-col items-center gap-4"
        >
          <p className="text-muted-foreground">
            Signed in as {session.user.name ?? session.user.email}
          </p>
          <Button type="submit" variant="outline">
            Sign out
          </Button>
        </form>
      ) : (
        <form action={signInWithGitHub}>
          <Button size="lg" type="submit">
            Sign in with GitHub
          </Button>
        </form>
      )}
    </main>
  );
}
