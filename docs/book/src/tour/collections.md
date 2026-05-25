# Collections

Two collection types ship with the language: lists and maps.

## Lists

```yoru
let xs: [Int] = [1, 2, 3]
let mixed = ["a", "b", "c"]

let first = xs[0]              // 1
let count = len(xs)            // 3
let joined = join(mixed, ",")  // "a,b,c"
```

### Higher-order

```yoru
let doubled  = map(xs, fn(x: Int) -> Int { x * 2 })
let evens    = filter(xs, fn(x: Int) -> Bool { x % 2 == 0 })
let sum      = reduce(xs, 0, fn(acc: Int, x: Int) -> Int { acc + x })
let sorted_  = sort(xs)
let reversed = reverse(xs)
let has2     = contains(xs, 2)
let chunk    = slice(xs, 1, 3)      // [2, 3]
let nested   = [[1, 2], [3, 4]]
let flat     = flatten(nested)      // [1, 2, 3, 4]
let pairs    = zip([1, 2], ["a", "b"])  // [[1, "a"], [2, "b"]]
let appended = append(xs, 99)       // [1, 2, 3, 99]
let r        = range(5)             // [0, 1, 2, 3, 4]
let r2       = range(2, 6)          // [2, 3, 4, 5]
```

`range(n)` produces `[0, 1, ..., n-1]`. `range(lo, hi)` produces
`[lo, lo+1, ..., hi-1]`. When `hi <= lo`, the result is empty.

Indexing out of range produces `Result.Err{kind: "index_out_of_bounds"}`.
Use `xs[i]?` to propagate it cleanly, or guard with `len(xs)`.

## Maps

Maps are string-keyed and dynamically typed in their values. Two
constructor forms work:

```yoru
let m1 = Map.of("name", "Ada", "age", 36)   // alternating k, v args
let m2 = Map.of({ name: "Ada", age: 36 })   // single object literal
```

Both produce the same map. Use whichever reads better at the call site —
the object-literal form is usually nicer for human-readable config.

All other map operations are **methods on the map value**, not
namespaced functions. `Map.of` and `Map.new` are the only entries in
the `Map` namespace.

```yoru
let name = m.get("name")        // "Ada"
let yes  = m.has("name")        // true
let ks   = m.keys()             // ["name", "age"]
let vs   = m.values()           // ["Ada", 36]
let es   = m.entries()          // [["name", "Ada"], ["age", 36]]
```

### `set` and `delete` return a new map

Maps are values. `set` and `delete` build and return a fresh map; the
original is unchanged. Capture the result with `let` (or reassign a
`mut` binding) to keep the updated copy.

```yoru
let m  = Map.of("a", 1)
let m2 = m.set("b", 2)           // m unchanged; m2 has both keys
let m3 = m2.delete("a")          // m3 = { b: 2 }

mut acc = Map.new()
acc = acc.set("count", 1)
acc = acc.set("count", 2)
```

This matches the language's "values, not references" framing and means
maps passed to another actor cannot be corrupted by either side.

## Iteration

`for ... in` walks lists. To iterate a map, walk `m.entries()`:

```yoru
for kv in m.entries() {
  println(kv[0] + " = " + to_string(kv[1]))
}
```

Each `kv` is a two-element list: `[key, value]`.
