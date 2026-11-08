import { signInAgain, signOutOfDokky } from "@/app/actions";

export const logout = () => signOutOfDokky();

// Lost or stolen removal and account deletion need a GitHub sign-in from the last few minutes.
export const signInFresh = (returnTo: string) => signInAgain(returnTo);
