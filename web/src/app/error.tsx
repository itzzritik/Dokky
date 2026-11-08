"use client";

import { TriangleAlertIcon } from "lucide-react";
import { GlitchButton } from "@/components/landing/client";
import { CORAL, Frame } from "@/components/landing/frame";

export default function ErrorPage({
  error,
  retry,
}: {
  error: unknown;
  retry: () => void;
}) {
  const digest =
    error instanceof Error && "digest" in error ? String(error.digest) : "";
  return (
    <Frame
      color={CORAL}
      icon={TriangleAlertIcon}
      text="This page hit an error. Try again or go home."
      title="Something went wrong"
    >
      <div className="grid grid-cols-2 gap-3">
        <GlitchButton
          className="h-12 w-full text-[12px] tracking-widest"
          onClick={retry}
        >
          Try again
        </GlitchButton>
        <GlitchButton
          className="h-12 w-full text-[12px] tracking-widest"
          href="/"
          variant="secondary"
        >
          Go home
        </GlitchButton>
      </div>
      {digest ? (
        <p className="mt-6 text-center font-mono text-[11px] text-[#52525b]">
          Ref {digest}
        </p>
      ) : null}
    </Frame>
  );
}
