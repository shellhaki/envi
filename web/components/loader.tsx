"use client";
import { Atom } from "loading-dev";

/**
 * A focal loading indicator, for the moments where there is nothing on screen
 * yet and the wait deserves a centrepiece.
 *
 * Lists use skeletons instead (see Skeleton in the dashboard): a shape that
 * matches the content holds the layout still, so nothing jumps when the data
 * lands. Buttons use the small CSS `.spinner`, because this animation needs
 * room to read as anything but a smudge.
 */
export default function Loader({ label, size = 44 }: { label: string; size?: number }) {
  return (
    <div className="loader" role="status" aria-live="polite">
      {/* No color prop: it inherits the surrounding text colour, which .loader
          sets from the theme, so this works in light and dark alike. */}
      <Atom size={size} />
      <span className="sr-only">{label}</span>
    </div>
  );
}
