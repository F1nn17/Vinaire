import { useCallback, useState } from "react";

// State that survives leaving the page and coming back (a list tab, a namespace
// filter): kept in memory for the life of the tab, so a click through to a detail
// page and back lands where the user was. Not web storage — a reload starts fresh.
const remembered = new Map<string, unknown>();

export function useRememberedState<T>(
  key: string,
  initial: T,
): [T, (value: T) => void] {
  const [value, setValue] = useState<T>(() =>
    remembered.has(key) ? (remembered.get(key) as T) : initial,
  );
  const set = useCallback(
    (next: T) => {
      remembered.set(key, next);
      setValue(next);
    },
    [key],
  );
  return [value, set];
}
