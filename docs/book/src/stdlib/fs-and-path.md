# FS and Path

The `FS` and `Path` effects give Yoru a native filesystem story without
shelling out. Everything is backed by Go's `os` and `path/filepath` -
no `subprocess` to `cat`, no glob mini-language. Errors come back as
`Result.Err{kind: "fs_*" | "path_*"}` so callers can branch the same way
they do for `HTTP` or `DB`.

Both providers are installed by default (no env-var gate) and recognized
as effect namespaces at the identifier layer, so `FS.read("...")`
resolves without `effect [FS]` boilerplate at top-level.

## FS - file operations

| Function | Purpose |
|----------|---------|
| `FS.read(path)`                    | Read a text file. Rejects binary content with `fs_binary`. |
| `FS.read_bytes(path)`              | Read raw bytes. Returns `Bytes`. |
| `FS.read_lines(path, offset, limit)` | Paginated read. `limit=0` reads to EOF. Returns `{content, total_lines, lines_returned, offset}`. |
| `FS.write(path, content)`          | Atomic write (temp+rename). Auto-creates parent dirs. Returns bytes written. |
| `FS.write_bytes(path, bytes)`      | Same, but accepts `Bytes`. |
| `FS.write_with(path, content, opts)` | Atomic write with `{backup: Bool, no_overwrite: Bool}`. |
| `FS.exists(path)`                  | `Bool`. |
| `FS.stat(path)`                    | `{name, size, is_dir, is_file, modified_unix}`. |
| `FS.is_binary(path)`               | `Bool`. Standard heuristic (null byte OR invalid UTF-8). |
| `FS.list(path)`                    | One-level entries: `[{name, is_dir}]`. |
| `FS.list_recursive(path, max_depth)` | Depth-limited recursion: `[{path, is_dir}]`. |
| `FS.delete(path)`                  | File or empty dir. |
| `FS.mkdir(path)`                   | Recursive (mkdir -p). |
| `FS.copy(src, dst)`                | Returns bytes copied. |

```yoru
match FS.read("/etc/hosts") {
  Err(e)  => println("read failed: " + e.message)
  content => println(content)
}

let n = FS.write("/tmp/out.txt", "hello") ?? 0
println("wrote " + to_string(n) + " bytes")
```

### Atomic writes by default

`FS.write` and `FS.write_with` are always atomic - they write to a
sibling tempfile and rename. That removes a whole class of half-written
file bugs when a process dies mid-write. Parent directories are
auto-created. There is no opt-out; if you need a non-atomic raw write,
use `FS.write_bytes` (still atomic - the contract is consistent).

### Pagination for large files

```yoru
let page = FS.read_lines("server.log", 1000, 100)
match page {
  Err(e) => println("read: " + e.message)
  p     => println("got " + to_string(p.lines_returned) + " of " +
                    to_string(p.total_lines) + " total")
}
```

### Binary handling

`FS.read` is **strict**: any file with a null byte or invalid UTF-8 is
rejected with `Result.Err{kind: "fs_binary"}` instead of silently
producing mangled text. An agent can no longer corrupt a PNG into
nonsense.

```yoru
match FS.read("/usr/bin/ls") {
  Err(e) => println("kind=" + e.kind)   // "fs_binary"
  _      => println("unexpected")
}
```

For binary I/O, use the bytes variants:

```yoru
let raw = FS.read_bytes("/usr/bin/ls") ?? Bytes.new(0)
FS.write_bytes("/tmp/ls.copy", raw)
```

If you need to know up-front, ask:

```yoru
if FS.is_binary(path) {
  // base64 it for transport, or use read_bytes
} else {
  let text = FS.read(path) ?? ""
}
```

## Path - path utilities

A separate provider so non-IO code can compose paths without taking
the FS dependency.

| Function | Purpose |
|----------|---------|
| `Path.join(parts)`            | `filepath.Join` over a list of strings. |
| `Path.resolve(path)`          | Absolute path, with symlinks resolved when the target exists. |
| `Path.dirname(path)`          | Parent directory. |
| `Path.basename(path)`         | Final path component. |
| `Path.extname(path)`          | File extension (including the leading dot). |
| `Path.is_within(child, parent)` | `Bool` - sandbox primitive. |

```yoru
Path.join(["/etc", "hosts"])      // "/etc/hosts"
Path.basename("/a/b/c.txt")       // "c.txt"
Path.extname("/a/b/c.txt")        // ".txt"
```

### Sandbox check

`Path.is_within` is the building block for "the agent can only touch
files under this root":

```yoru
let workspace = "/tmp/sandbox"

fn safe_read(p: String) -> String {
  if !Path.is_within(p, workspace) {
    "ERROR: path escapes workspace"
  } else {
    FS.read(p) ?? "ERROR: not readable"
  }
}
```

The function resolves both paths (eval'ing symlinks where possible,
falling back to the longest existing ancestor when the child doesn't
exist yet) and checks the relative path doesn't escape with `..`.
That last detail matters on macOS - `/tmp` is a symlink to
`/private/tmp`, so a naive prefix check fails for any file that
doesn't exist yet under a `/tmp` workspace. The ancestor-walking trick
makes the sandbox honest in both cases.

## Read-before-edit: `FS.with_session(fn)`

Wraps a block in a per-session tracker that records every `FS.read`
and refuses to overwrite a path the session hasn't observed (or whose
on-disk hash has changed since reading).

```yoru
FS.with_session(fn() => {
  let original = FS.read("/tmp/notes.txt")?
  let edited   = replace(original, "TODO", "DONE")
  FS.write_tracked("/tmp/notes.txt", edited)
})
```

Failure modes - all `Result.Err`:

| Kind | When |
|------|------|
| `fs_no_session`   | `FS.write_tracked(...)` called outside a `with_session` block. |
| `fs_not_read`     | The path was never read in this session. |
| `fs_stale_read`   | The on-disk SHA-256 differs from what was recorded at read time. Someone else changed the file. |

The session is **scoped to the block**: the tracker is pushed on
entry, popped on exit (success or panic). Nested `with_session` blocks
are independent - the inner one doesn't see the outer's records.

```yoru
| Function                              | What it does |
|---------------------------------------|--------------|
| `FS.with_session(fn() => ...)`        | Open a tracker scope. Returns the closure's value. |
| `FS.write_tracked(path, content)`     | Atomic write that requires the path was read in this session and hasn't drifted. |
| `FS.write_tracked_with(path, content, opts)` | Same with `backup` / `no_overwrite` options. |
```

### Why this exists

The "agent overwrites a file it never read" failure mode is one of the
most common destructive-action paths in production agent systems. The
tracker makes it structurally impossible inside a `with_session` block
- the model has to read first or the write fails with a recoverable
error it can correct on the next turn. It's small, capability-flavoured,
and composes naturally with `with_capability` from
[Capability scoping](../agents/capability-scoping.md).

## Error kinds, all of them

| Kind | Where it comes from |
|------|---------------------|
| `fs_not_found`    | Anywhere a path doesn't exist. |
| `fs_permission`   | OS reported `EACCES`. |
| `fs_exists`       | `write_with(opts={no_overwrite: true})` against an existing file. |
| `fs_binary`       | `FS.read` on a file that's binary by heuristic. |
| `fs_io`           | Any other IO error. |
| `fs_bad_args`     | Wrong argument shape. |
| `fs_no_session`   | `write_tracked` outside `with_session`. |
| `fs_not_read`     | `write_tracked` against an un-read path. |
| `fs_stale_read`   | `write_tracked` against a drifted file. |
| `path_bad_args`   | Wrong argument shape for a `Path.*` call. |
| `path_io`         | `Path.resolve` couldn't compute the absolute form. |
