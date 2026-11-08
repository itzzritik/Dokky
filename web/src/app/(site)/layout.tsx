import { Nav } from "@/components/landing/nav";

// one Nav for / and /docs, so the logo fill plays once
export default function SiteLayout({ children }: LayoutProps<"/">) {
  return (
    <>
      <Nav />
      {children}
    </>
  );
}
