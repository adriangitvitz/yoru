# Fuzzy and Diff

Two narrow stdlib providers that ship algorithms - not data shapes -
for the editing patterns LLM agents need most: forgiving find/replace
and unified-diff rendering.

## `Fuzzy.find_replace`

4-level progressive matcher inspired by Codex CLI's `seek_sequence`.
The LLM proposes a `before → after` rewrite; the runtime tolerates
whitespace and Unicode-punctuation differences between what the model
emits and what's actually in the file.

```yoru
Fuzzy.find_replace(content, old_text, new_text, count)
```

| Argument | Meaning |
|----------|---------|
| `content`  | The haystack. |
| `old_text` | The pattern. Multi-line allowed. |
| `new_text` | The replacement. Multi-line allowed. |
| `count`    | Max replacements. `0` means "all". |

Returns `{result: String, match_level: String, replacements: Int}` on
hit, or `Result.Err{kind: "fuzzy_no_match" \| "fuzzy_empty_needle" \|
"fuzzy_bad_args"}` on miss.

### The four levels

The matcher tries each in order and reports the **worst** level that
fired across all replacements:

1. **`exact`** - byte-for-byte match.
2. **`trim_trailing`** - trailing whitespace on each line is ignored.
3. **`trim_all`** - both leading and trailing whitespace ignored.
4. **`unicode_normalized`** - `trim_all` plus a small punctuation
   normalization (smart quotes → straight, en/em-dashes → hyphen,
   NBSP → space, arrows `→ ⇒` → `-> =>`, ellipsis → `...`).

At levels ≥ `trim_all` the replacement is **re-indented** to match the
haystack's existing leading whitespace. The LLM can emit a flat,
unindented `println("new")` and the runtime slots it in at whatever
indent the original line carried. This is the single most useful
property when an LLM edits structured code.

```yoru
let file = "fn main() {\n    println(\"old\")\n}\n"
let r = Fuzzy.find_replace(file, "println(\"old\")", "println(\"NEW\")", 0)
match r {
  Err(_) => println("no match")
  out    => {
    println("level: " + out.match_level)        // "trim_all"
    println("result: " + out.result)            // ...println("NEW") at original 4-space indent
  }
}
```

## `Diff.unified`

A unified-diff renderer backed by `pmezard/go-difflib`. Same output
shape as `git diff`.

| Function | Purpose |
|----------|---------|
| `Diff.unified(a, b)`                | Unified diff with header path `"file"`. |
| `Diff.unified_named(a, b, path)`    | Same with a caller-supplied header (used in the `--- a/<path>` / `+++ b/<path>` lines). |

Empty result when `a == b`. Otherwise a string suitable for showing
back to the user - or feeding back to the LLM as part of a tool result.

```yoru
let before = FS.read("src/main.yr") ?? ""
let after  = replace(before, "old_name", "new_name")
FS.write("src/main.yr", after)
println(Diff.unified_named(before, after, "src/main.yr"))
```

## Why these are stdlib

Two reasons:

1. **The algorithms are the reusable part.** Every agent that edits
   text wants fuzzy match and a diff to display. Reimplementing them
   in Yoru on top of `split` / `slice` / `join` would be both verbose
   and slower than calling out to native Go.
2. **The data shape isn't.** Stdlib ships `Fuzzy` and `Diff`, not a
   blessed `PatchOp` enum. Different agents want different patch
   shapes (line-based, hunk-based, semantic). Yoru leaves the data
   model to user code and ships the primitives that compose under any
   of them - see the
   [`examples/showcase/llm_file_editor.yr`](https://github.com/adriangitvitz/yoru/blob/master/examples/showcase/llm_file_editor.yr)
   file for one full assembly.
