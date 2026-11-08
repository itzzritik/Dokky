"use client";

import { TriangleAlertIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { send } from "../api";
import { Action } from "../kit/action";
import { ShellDialog } from "../kit/dialog";
import { FieldLabel, TextField } from "../kit/field";
import { logout, signInFresh } from "../session";

const CONFIRM = "DELETE";

export function DeleteAccountDialog({
  onOpenChange,
  open,
}: {
  onOpenChange: (open: boolean) => void;
  open: boolean;
}) {
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) {
      return;
    }
    setTyped("");
    setError(null);
  }, [open]);

  const submit = async () => {
    if (typed !== CONFIRM || busy) {
      return;
    }
    setBusy(true);
    setError(null);
    const sent = await send("/api/dash/account", "DELETE");
    if (sent.code === "fresh_signin") {
      await signInFresh("/dashboard/account?delete=1");
      return;
    }
    if (!sent.ok) {
      setError("Couldn't finish deleting the account. Try again.");
      setBusy(false);
      return;
    }
    await logout();
  };

  return (
    <ShellDialog
      actions={
        <Action disabled={typed !== CONFIRM || busy} tone="fill" type="submit">
          {busy ? "Deleting" : "Delete account"}
        </Action>
      }
      description="Removes your saved sign-ins, drives and Activity from dokky. Every device finishes its uploads, then erases itself. Files in your storage stay. This can't be undone."
      error={error}
      heading="Delete your dokky account?"
      icon={TriangleAlertIcon}
      initialFocus={input}
      onOpenChange={onOpenChange}
      onSubmit={() => void submit()}
      open={open}
      title="Delete account"
      tone="danger"
    >
      <div className="warn-box">
        <b>!</b>
        <span>You may need to sign in to GitHub again.</span>
      </div>
      <FieldLabel className="mt-1" htmlFor="delete-confirm">
        Type {CONFIRM} to confirm
      </FieldLabel>
      <TextField
        autoComplete="off"
        id="delete-confirm"
        onValueChange={setTyped}
        readOnly={busy}
        ref={input}
        value={typed}
      />
    </ShellDialog>
  );
}
