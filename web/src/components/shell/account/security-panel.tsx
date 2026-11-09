"use client";

import {
  CircleCheckIcon,
  FingerprintPatternIcon,
  HourglassIcon,
  LockKeyholeIcon,
  LockKeyholeOpenIcon,
  ShieldCheckIcon,
} from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { useResource } from "@/hooks/use-resource";
import { formatDate, plural } from "@/lib/format";
import { Action } from "../kit/action";
import { Panel } from "../kit/panel";
import type { Security } from "../types";
import { Setting } from "./setting";

const GROUP = /.{1,4}/gu;

// in groups of four, like the recovery key
const fingerprint = (keyId: string) => keyId.match(GROUP)?.join(" ") ?? keyId;

type SealedData = Security & { vault: NonNullable<Security["vault"]> };

function Sealed({ data }: { data: SealedData }) {
  const { accounts, legacy, vault } = data;
  return (
    <>
      <Setting
        icon={LockKeyholeIcon}
        note={`Since ${formatDate(vault.since)}. Your drive logins are sealed on your devices with a key only your master password or recovery key can open. dokky's server stores them but can't read them.`}
        title="End-to-end encrypted"
        tone="success"
      />
      <Setting
        action={
          <code className="key-print" title="Key fingerprint">
            {fingerprint(vault.keyId)}
          </code>
        }
        icon={FingerprintPatternIcon}
        note="The same on every device signed in to this account."
        title="Key fingerprint"
      />
      {legacy > 0 ? (
        <Setting
          icon={HourglassIcon}
          note="Their logins still use dokky's older server-side encryption. Open dokky on any of your devices to finish encrypting them."
          title={`${plural(legacy, "account")} waiting`}
          tone="warning"
        />
      ) : (
        accounts > 0 && (
          <Setting
            icon={CircleCheckIcon}
            note="None are left on the older server-side encryption."
            title={
              accounts === 1
                ? "Your account is encrypted"
                : `All ${plural(accounts, "account")} encrypted`
            }
          />
        )
      )}
    </>
  );
}

export function SecurityPanel({ delay }: { delay: number }) {
  const security = useResource<Security>("/api/dash/security");
  const { data } = security;

  let body = (
    <div aria-busy className="setting">
      <Skeleton className="size-[30px] rounded-lg" />
      <div className="grid flex-1 gap-2">
        <Skeleton className="h-4 w-44" />
        <Skeleton className="h-3.5 w-72 max-w-full" />
      </div>
    </div>
  );
  if (security.error) {
    body = (
      <div className="setting flex-wrap">
        <p className="text-muted-foreground">
          Couldn't load your security details. {security.error}
        </p>
        <Action onClick={security.retry} tone="primary">
          Retry
        </Action>
      </div>
    );
  }
  if (data?.vault) {
    body = <Sealed data={{ ...data, vault: data.vault }} />;
  } else if (data) {
    body = (
      <Setting
        icon={LockKeyholeOpenIcon}
        note="Open dokky on a device and create a master password. Until then, dokky's server encrypts your logins with its own key."
        title="Not set up yet"
        tone="warning"
      />
    );
  }

  let aside: string | undefined;
  if (data) {
    aside = data.vault ? "XChaCha20-Poly1305" : "Not set up";
  }
  return (
    <Panel
      aside={aside}
      className="account-wide"
      delay={delay}
      icon={ShieldCheckIcon}
      title="Security"
    >
      {body}
    </Panel>
  );
}
