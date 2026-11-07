"use client";

import { useCallback, useState } from "react";
import { Button } from "@/components/ui/button";

interface Props {
  accounts: string[];
  erase: boolean;
  name: string;
  offline: boolean;
}

export function RemoveDevice({ accounts, erase, name, offline }: Props) {
  const [asking, setAsking] = useState(erase);
  const [lost, setLost] = useState(erase);
  const ask = useCallback(() => setAsking(true), []);
  const cancel = useCallback(() => setAsking(false), []);
  const toggle = useCallback(() => setLost((v) => !v), []);

  if (!asking) {
    return (
      <Button onClick={ask} size="sm" type="button" variant="outline">
        Remove
      </Button>
    );
  }
  const keeps =
    accounts.length > 0
      ? ` Until then it keeps its sign-ins for: ${accounts.join(", ")}.`
      : "";
  return (
    <div className="flex max-w-sm flex-col items-end gap-2 text-left">
      <p className="w-full font-medium">Remove {name}?</p>
      <p className="text-muted-foreground w-full">
        {lost
          ? "It erases right away. Files it hasn't uploaded are lost."
          : "It signs out and erases its dokky data once its transfers finish. Your files stay in your storage."}
      </p>
      {offline ? (
        <p className="text-muted-foreground w-full">
          {`It's offline, so it erases itself when it next comes online.${keeps}`}
        </p>
      ) : null}
      <label className="flex w-full items-center gap-2">
        <input
          checked={lost}
          name="mode"
          onChange={toggle}
          type="checkbox"
          value="erase"
        />
        Lost or stolen
      </label>
      <div className="flex gap-2">
        <Button onClick={cancel} size="sm" type="button" variant="outline">
          Cancel
        </Button>
        <Button size="sm" type="submit" variant="destructive">
          Remove
        </Button>
      </div>
    </div>
  );
}
