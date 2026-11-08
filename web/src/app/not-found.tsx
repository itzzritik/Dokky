import { CompassIcon } from "lucide-react";
import { GlitchButton } from "@/components/landing/client";
import { Frame } from "@/components/landing/frame";

export default function NotFound() {
  return (
    <Frame
      icon={CompassIcon}
      text="This page doesn't exist or has moved."
      title="Page not found"
    >
      <div className="flex justify-center">
        <GlitchButton
          className="h-12 px-10 text-[12px] tracking-widest"
          href="/"
        >
          Go home
        </GlitchButton>
      </div>
    </Frame>
  );
}
