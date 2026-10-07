// The development twin of jsx-runtime.ts (the compiler emits jsxDEV in dev builds).
import * as base from "react/jsx-dev-runtime";
import { translateProps } from "../translate";

export type { JSX } from "react/jsx-dev-runtime";
export const Fragment = base.Fragment;

export const jsxDEV = ((
  type: unknown,
  props: Record<string, unknown>,
  ...rest: unknown[]
) =>
  (base.jsxDEV as (...a: unknown[]) => unknown)(
    type,
    translateProps(type, props),
    ...rest,
  )) as typeof base.jsxDEV;
