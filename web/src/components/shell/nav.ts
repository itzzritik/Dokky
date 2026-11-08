import {
  ActivityIcon,
  CircleUserIcon,
  HardDriveIcon,
  type LucideIcon,
  MonitorSmartphoneIcon,
} from "lucide-react";

interface Page {
  href: string;
  icon: LucideIcon;
  key: "1" | "2" | "3" | "4";
  keywords: string[];
  label: string;
}

// Tabs, the palette and the 1 2 3 4 shortcuts all read from this list.
export const PAGES: Page[] = [
  {
    href: "/dashboard",
    icon: HardDriveIcon,
    key: "1",
    keywords: ["drives", "mounts", "storage", "home"],
    label: "Drives",
  },
  {
    href: "/dashboard/devices",
    icon: MonitorSmartphoneIcon,
    key: "2",
    keywords: ["devices", "machines", "computers"],
    label: "Devices",
  },
  {
    href: "/dashboard/activity",
    icon: ActivityIcon,
    key: "3",
    keywords: ["activity", "log", "history", "events"],
    label: "Activity",
  },
  {
    href: "/dashboard/account",
    icon: CircleUserIcon,
    key: "4",
    keywords: ["account", "profile", "sign out", "delete"],
    label: "Account",
  },
];

export const pageIndex = (pathname: string) =>
  Math.max(
    0,
    PAGES.findLastIndex(
      (page) => pathname === page.href || pathname.startsWith(`${page.href}/`)
    )
  );
