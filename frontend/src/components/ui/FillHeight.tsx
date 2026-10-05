"use client";

import { useLayoutEffect, useRef, useState, type ReactNode } from "react";

// The one pattern for "this block takes the rest of the window and scrolls inside
// itself" — a YAML editor, a log, a long list. The page (the shell's <main>) then
// has nothing left to scroll and leaves no empty band under the block.
//
// It measures where the block starts inside <main> and gives it the remaining
// height (minus `bottomGap` — the pages' own bottom padding, py-7 = 28px — never less than `minHeight`), re-measuring when the
// window or <main> is resized or what sits above the block changes height. Put
// the scrolling part inside with `h-full overflow-auto` (or a Monaco editor with
// `height="100%"`).
export function FillHeight({
  children,
  className = "",
  bottomGap = 28,
  minHeight = 240,
}: {
  children: ReactNode;
  className?: string;
  bottomGap?: number;
  minHeight?: number;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState<number | undefined>(undefined);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const main = el.closest("main");

    const measure = () => {
      const base = main ?? document.documentElement;
      // distance from the top of <main>'s content to the block — independent of
      // how far the user has scrolled
      const offset = main
        ? el.getBoundingClientRect().top -
          main.getBoundingClientRect().top +
          main.scrollTop
        : el.getBoundingClientRect().top;
      const avail = (main ? base.clientHeight : window.innerHeight) - offset;
      setHeight(Math.max(minHeight, Math.floor(avail - bottomGap)));
    };

    measure();
    const ro = new ResizeObserver(measure);
    if (main) ro.observe(main);
    // something above the block growing or shrinking moves it
    for (let s = el.previousElementSibling; s; s = s.previousElementSibling) {
      ro.observe(s);
    }
    window.addEventListener("resize", measure);
    return () => {
      ro.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, [bottomGap, minHeight]);

  return (
    <div
      ref={ref}
      className={`min-h-0 ${className}`}
      style={height === undefined ? { minHeight } : { height }}
    >
      {children}
    </div>
  );
}
