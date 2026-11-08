import { redirect } from "next/navigation";
import { auth } from "@/auth";
import { Shell } from "@/components/shell/shell";

export default async function DashboardLayout({
  children,
}: LayoutProps<"/dashboard">) {
  if (!(await auth())?.user?.id) {
    redirect("/");
  }
  return <Shell>{children}</Shell>;
}
