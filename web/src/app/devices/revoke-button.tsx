"use client";

import { useCallback, useState } from "react";
import { Button } from "@/components/ui/button";

export function RevokeButton() {
  const [asking, setAsking] = useState(false);
  const ask = useCallback(() => setAsking(true), []);
  const cancel = useCallback(() => setAsking(false), []);

  if (!asking) {
    return (
      <Button onClick={ask} size="sm" type="button" variant="outline">
        Revoke
      </Button>
    );
  }
  return (
    <div className="flex flex-col items-end gap-2">
      <p className="text-muted-foreground">
        This device will be wiped, including any uploads it has not sent yet.
      </p>
      <div className="flex gap-2">
        <Button onClick={cancel} size="sm" type="button" variant="outline">
          Cancel
        </Button>
        <Button size="sm" type="submit" variant="destructive">
          Revoke device
        </Button>
      </div>
    </div>
  );
}
