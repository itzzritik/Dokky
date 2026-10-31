import type { Metadata } from "next";
import { Geist } from "next/font/google";
import "./globals.css";

const geist = Geist({ subsets: ["latin"], variable: "--font-sans" });

export const metadata: Metadata = {
  title: "dokky",
  description: "Mount NAS and cloud storage as native drives.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html className={`${geist.variable} font-sans`} lang="en">
      <body>{children}</body>
    </html>
  );
}
