// Roadmap Phase 73: Redis and Valkey with ACL keep their users in a file on the data volume.
//
// The first version of "Use ACL" passed the user as command-line options, so the user was created
// afresh at every start and anything added later with `ACL SETUSER` was gone after a restart. With
// `--aclfile` the file is the truth: it is written once, on the first start, from the generated
// password (as a SHA-256 hash — the password itself is not left on the volume), and after that the
// server only reads it. `ACL SETUSER` still changes users in memory; `ACL SAVE` writes them to the
// file (Redis never does that on its own).
//
// The script runs the image's own entrypoint (`docker-entrypoint.sh`) at the end, so the server is
// still started as the unprivileged `redis` user and the volume's ownership is still fixed up first.

// The user name is checked by the preset's pattern before it gets here; it is put into the script
// as a word of letters, digits and underscores only.
const SAFE_USER = /^[a-z_][a-z0-9_]{0,30}$/;

export function aclArgs(
  server: "redis-server" | "valkey-server",
  passwordEnv: string,
  user: string,
): string[] {
  if (!SAFE_USER.test(user)) throw new Error("not a valid ACL user name");
  const script = [
    "set -e",
    "f=/data/users.acl",
    'if [ ! -f "$f" ]; then',
    `  h=$(printf %s "$${passwordEnv}" | sha256sum | cut -d' ' -f1)`,
    `  printf 'user default off\\nuser ${user} on #%s ~* &* +@all\\n' "$h" > "$f"`,
    "fi",
    `exec docker-entrypoint.sh ${server} --appendonly yes --aclfile "$f"`,
  ].join("\n");
  return ["sh", "-c", script];
}
