# Self-hosted fonts

These `.woff2` files are IBM Plex Sans (400/500/600) and JetBrains Mono
(500/600/700), latin subset only — checked into the repo instead of using
`next/font/google`, because that loader fetches font files from Google's
servers *at build time*, which made the Docker build fail whenever that
network hop was unreachable or flaky (build environments don't always have
the same network access as a dev machine).

## Re-fetching (e.g. to add a weight, or pick up a font update)

For each weight, fetch the CSS with a browser User-Agent (required — Google
serves woff2 only to browser-like clients) and pull the `latin` block's
`url(...)`:

```bash
curl -s -A "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36" \
  "https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:wght@400&display=swap"
```

Take the `url(...)` from the `/* latin */` block specifically (not
`latin-ext`/`cyrillic`/etc.) — that's the actual Latin-only subset. **Fetch
one weight per request** — combining multiple weights in one
`:wght@400;500;600` query has, in practice, sometimes returned the same
file for every weight (a Google Fonts API quirk, not a config error on our
end); fetching each weight separately reliably returns distinct files.

Download the resulting `fonts.gstatic.com` URL directly (`curl -o`) and
replace the matching file here. Update the `src` array in
`../layout.tsx` if a filename or weight list changes.
