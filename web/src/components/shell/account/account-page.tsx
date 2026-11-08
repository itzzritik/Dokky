"use client";

import {
  CircleUserIcon,
  LogOutIcon,
  type LucideIcon,
  MonitorIcon,
  SunMoonIcon,
  TrashIcon,
  TriangleAlertIcon,
} from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { type ReactNode, useEffect, useState } from "react";
import { Skeleton } from "@/components/ui/skeleton";
import { useResource } from "@/hooks/use-resource";
import { useShortcuts } from "@/hooks/use-shortcuts";
import { useHints } from "../frame/frame";
import { useThemeToggle } from "../frame/theme";
import { Action } from "../kit/action";
import { Count } from "../kit/count";
import { Panel } from "../kit/panel";
import { logout } from "../session";
import type { Me } from "../types";
import { DeleteAccountDialog } from "./delete-account-dialog";

const WORD_BREAK = /[\s._-]+/u;

const initials = (name: string) => {
  const words = name.split(WORD_BREAK).filter(Boolean);
  return [words[0], words.length > 1 ? words.at(-1) : undefined]
    .map((word) => word?.[0] ?? "")
    .join("")
    .toUpperCase();
};

function Setting({
  action,
  icon: Icon,
  note,
  title,
}: {
  action: ReactNode;
  icon: LucideIcon;
  note: string;
  title: string;
}) {
  return (
    <div className="setting">
      <span aria-hidden className="setting-icon">
        <Icon />
      </span>
      <p>
        {title}
        <small>{note}</small>
      </p>
      {action}
    </div>
  );
}

function Stat({
  children,
  href,
  label,
}: {
  children: ReactNode;
  href?: string;
  label: string;
}) {
  const body = (
    <>
      <span className="stat-value">{children}</span>
      <span className="stat-label">{label}</span>
    </>
  );
  return href ? (
    <Link className="stat" href={href}>
      {body}
    </Link>
  ) : (
    <div className="stat">{body}</div>
  );
}

export function AccountPage() {
  const router = useRouter();
  const params = useSearchParams();
  const me = useResource<Me>("/api/dash/me");
  const toggleTheme = useThemeToggle();
  const [deleting, setDeleting] = useState(false);

  useEffect(() => {
    if (params.get("delete") !== "1") {
      return;
    }
    setDeleting(true);
    router.replace("/dashboard/account", { scroll: false });
  }, [params, router]);

  useShortcuts({ s: () => void logout() });
  useHints([
    { combo: "s", label: "Sign out" },
    { combo: "t", label: "Switch theme" },
  ]);

  let identity = (
    <div aria-busy className="identity-who">
      <Skeleton className="size-[52px] rounded-xl" />
      <div className="grid gap-2">
        <Skeleton className="h-5 w-44" />
        <Skeleton className="h-4 w-56" />
      </div>
    </div>
  );
  if (me.error) {
    identity = (
      <div className="identity-who flex-wrap">
        <p className="text-muted-foreground">
          Couldn't load your profile. {me.error}
        </p>
        <Action onClick={me.retry} tone="primary">
          Retry
        </Action>
      </div>
    );
  }
  if (me.data) {
    const { email, image } = me.data;
    const name = me.data.name.trim() || email.split("@")[0];
    identity = (
      <div className="identity-who">
        <span aria-hidden className="identity-tile overflow-hidden">
          {image ? (
            // biome-ignore lint/performance/noImgElement: tiny GitHub avatar, not worth next/image config
            <img
              alt=""
              className="size-full object-cover"
              height={52}
              src={image}
              width={52}
            />
          ) : (
            initials(name)
          )}
        </span>
        <div className="min-w-0">
          <p className="identity-name">{name}</p>
          <p className="identity-email">{email}</p>
        </div>
      </div>
    );
  }

  const stat = (value: number | undefined) =>
    value === undefined ? "-" : <Count value={value} />;
  return (
    <div className="account-layout">
      <Panel
        aside="Signed in with GitHub"
        className="account-wide"
        icon={CircleUserIcon}
        title="Profile"
      >
        <div className="identity">
          {identity}
          <div className="stats">
            <Stat href="/dashboard" label="Drives">
              {stat(me.data?.drives)}
            </Stat>
            <Stat href="/dashboard/devices" label="Devices">
              {stat(me.data?.devices)}
            </Stat>
            <Stat label="Accounts">{stat(me.data?.accounts)}</Stat>
          </div>
        </div>
      </Panel>
      <Panel
        className="account-wide"
        delay={80}
        icon={MonitorIcon}
        title="This browser"
      >
        <Setting
          action={
            <Action combo="t" onClick={toggleTheme}>
              Switch
            </Action>
          }
          icon={SunMoonIcon}
          note="Follows your system until you switch it here."
          title="Light and dark"
        />
        <Setting
          action={
            <Action combo="s" onClick={() => void logout()}>
              Sign out
            </Action>
          }
          icon={LogOutIcon}
          note="Signs this browser out. Your devices stay signed in."
          title="Sign out"
        />
      </Panel>
      <Panel
        className="account-wide"
        delay={120}
        icon={TriangleAlertIcon}
        title="Danger zone"
        tone="danger"
      >
        <Setting
          action={
            <Action onClick={() => setDeleting(true)} tone="danger">
              Delete account
            </Action>
          }
          icon={TrashIcon}
          note="Removes everything dokky keeps for you. Files in your storage stay."
          title="Delete account"
        />
      </Panel>
      <DeleteAccountDialog onOpenChange={setDeleting} open={deleting} />
    </div>
  );
}
