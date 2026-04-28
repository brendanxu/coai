// v0.6 carbon — single source of truth for the leaf glyph.
// Using lucide-react's <Leaf /> for v0.6 (clean stroke, fits "considered" voice).
// Swap to a custom hand-drawn SVG in v0.7 if /design-review flags as too generic.
import { Leaf } from "lucide-react";
import { cn } from "@/components/ui/lib/utils.ts";

type Props = {
  size?: number;
  className?: string;
  strokeWidth?: number;
};

export function LeafIcon({ size = 14, className, strokeWidth = 1.5 }: Props) {
  return (
    <Leaf
      size={size}
      strokeWidth={strokeWidth}
      className={cn("inline-block shrink-0", className)}
      aria-hidden="true"
    />
  );
}
