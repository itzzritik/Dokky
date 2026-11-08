import Link from "next/link";
import { signInWithGitHub } from "@/app/actions";
import { auth } from "@/auth";
import { DokkyMark } from "./brand";
import { GlitchButton } from "./client";
import { UserMenu } from "./user-menu";

export async function Nav() {
  const user = (await auth())?.user;
  return (
    <nav
      className="fixed top-0 right-0 left-0 z-50 border-b border-[#27272a] bg-black/80 backdrop-blur-xl"
      data-landing
    >
      <div className="flex h-14 w-full items-center justify-between px-6 lg:px-16">
        <Link aria-label="dokky home" href="/">
          <DokkyMark water width={74} />
        </Link>
        {user ? (
          <UserMenu
            image={user.image ?? null}
            name={user.name ?? user.email ?? "You"}
          />
        ) : (
          <form action={signInWithGitHub}>
            <GlitchButton className="h-8 px-5 text-[12px]" type="submit">
              Log in
            </GlitchButton>
          </form>
        )}
      </div>
    </nav>
  );
}
