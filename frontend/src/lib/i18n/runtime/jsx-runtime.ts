// A drop-in for react/jsx-runtime (tsconfig's jsxImportSource points here): every
// JSX element goes through translateProps first. See lib/i18n/translate.ts.
import * as base from "react/jsx-runtime";
import { translateProps } from "../translate";

export type { JSX } from "react/jsx-runtime";
export const Fragment = base.Fragment;

export const jsx = ((
  type: unknown,
  props: Record<string, unknown>,
  key?: unknown,
) =>
  (base.jsx as (...a: unknown[]) => unknown)(
    type,
    translateProps(type, props),
    key,
  )) as typeof base.jsx;

export const jsxs = ((
  type: unknown,
  props: Record<string, unknown>,
  key?: unknown,
) =>
  (base.jsxs as (...a: unknown[]) => unknown)(
    type,
    translateProps(type, props),
    key,
  )) as typeof base.jsxs;
