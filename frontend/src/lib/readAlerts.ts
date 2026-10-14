// Which alerts the person has marked as read. Remembered in a cookie (web storage is off limits in
// this app, and the theme already lives in one): the ids of the last few occurrences, so the list
// stays small. An id is an occurrence — the same alert firing again later is unread again.
const COOKIE = "vinaire_alerts_read";
const KEEP = 40;

export function readAlertIds(): string[] {
  if (typeof document === "undefined") return [];
  const m = document.cookie.split("; ").find((c) => c.startsWith(COOKIE + "="));
  if (!m) return [];
  try {
    const v = JSON.parse(decodeURIComponent(m.slice(COOKIE.length + 1)));
    return Array.isArray(v) ? v.filter((x) => typeof x === "string") : [];
  } catch {
    return [];
  }
}

export function rememberRead(ids: string[]): string[] {
  const all = [...new Set([...ids, ...readAlertIds()])].slice(0, KEEP);
  document.cookie = `${COOKIE}=${encodeURIComponent(JSON.stringify(all))}; path=/; max-age=31536000; SameSite=Lax`;
  return all;
}
