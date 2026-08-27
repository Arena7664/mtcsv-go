# mtcsv-go

A Go parser, encoder and decoder for **MTCSV** ("Multi-Table CSV") — RFC 4180 CSV
with two extra conventions: blank lines separate tables, and `#` lines carry
structure or commentary. See [MTCSV-SPEC.md](MTCSV-SPEC.md) for the format.

**[Full documentation → DOCS.md](DOCS.md)** — struct tags, marshalling,
unmarshalling, multi-table containers, the document API, diagnostics and
recipes. This page is the tour.

The API mirrors `encoding/json`, so it should already be familiar:

```go
b, err := mtcsv.Marshal(v)
err   := mtcsv.Unmarshal(b, &v)

enc := mtcsv.NewEncoder(w); enc.Encode(v)
dec := mtcsv.NewDecoder(r); dec.Decode(&v)
```

```go
import "github.com/Arena7664/mtcsv-go"
```

## Struct mapping

```go
type User struct {
	ID    int    `mtcsv:"id"    mtcsv.type:"int"   mtcsv.doc:"unique user id"`
	Name  string `mtcsv:"name"`
	Email string `mtcsv:"email" mtcsv.type:"email"`
}

type File struct {
	Users []User `mtcsv:"users,schema=v2" mtcsv.doc:"Registered accounts."`
	Tags  []Tag  `mtcsv:"tags"`
}
```

`mtcsv.Marshal(File{...})` writes:

```mtcsv
# users schema=v2
#! Registered accounts.
id,name,email
#: id    (int)   unique user id
#: email (email)
1,Alice,alice@example.com

# tags
order_id,tag
10,priority
```

| Go value | MTCSV |
|---|---|
| `[]T` | one table; anonymous unless named by a tag, `EncodeTable`, or `TableDescriptor` |
| `struct{ A []T; B []U }` | a container: one table per field, in declaration order |
| `map[string][]T` | one table per key, keys sorted for deterministic output |
| `[]map[string]T` | rows keyed by column name; the header is the union of keys |
| `[][]string` | raw rows, with the header as row 0 |
| `[]string`, `[]int`, … | a single-column table named `value` |
| `*Document`, `*Table` | written as-is |

Decoding accepts the same shapes. Tables match struct fields by name (exactly,
then case-insensitively); leftover tables fill the remaining fields in order,
which is how anonymous tables are addressed. Sections that share a name are
appended into one field, per the spec's optional append semantics.

### Tags

| Tag | Meaning |
|---|---|
| `mtcsv:"name"` | column name (row struct) or table name (container struct) |
| `mtcsv:"-"` | never encoded or decoded |
| `mtcsv:"id,omitempty"` | write an empty cell instead of a zero value |
| `mtcsv:"users,schema=v2"` | table marker metadata |
| `mtcsv:"rows,anon"` | table written without a marker |
| `mtcsv.type:"int"` | declared column type, emitted as `#: col (int)` |
| `mtcsv.doc:"text"` | column comment, or table description on a container field |
| `mtcsv.meta:"a=1 b=\"x y\""` | table metadata in marker syntax |

### Cell types

Strings, booleans, all integer and float kinds, `[]byte` (verbatim, not base64),
`time.Time`, pointers (nil ⇄ empty cell), and anything implementing
`mtcsv.Marshaler`/`Unmarshaler` or `encoding.TextMarshaler`/`TextUnmarshaler`.
An empty cell decodes to the zero value. A column declared `date` or `time`
formats a `time.Time` accordingly; decoding accepts RFC 3339 and the common
`YYYY-MM-DD[ HH:MM[:SS]]` shapes. MTCSV is flat and rectangular, so nested
composites are rejected rather than silently flattened.

## Documents

`Parse` exposes the full document model — tables, metadata, descriptions,
column types and diagnostics — for tooling that needs more than struct mapping:

```go
doc, err := mtcsv.Parse(data)      // err is non-nil only for error-severity diagnostics
t := doc.Table("users")
t.Meta.Get("schema")               // "v2"
t.Column("id").Type                // "int"
t.Get(0, "email")                  // "alice@example.com"
doc.Merge()                        // concatenate same-named sections
os.Stdout.Write(doc.Bytes())
```

`Parse` always returns a usable document: problems are reported as
`Document.Diagnostics` with the standard codes (`too-many-fields`,
`unterminated-quote`, `unknown-column`, `duplicate-column`, `no-header`, and the
opt-in `ragged-short-row` hint). `Decoder.Strict` turns error-severity
diagnostics into a failure; `DisallowUnknownColumns` and `DisallowUnknownTables`
add the strictness `encoding/json` users expect.

Untrusted input should set `ParseOptions.MaxRecordBytes`, since a quoted field
may span the whole file.

## Conformance

A full reader and a conformant writer: quote-aware line
classification, markers with quoted names and metadata, table descriptions,
column comments with types and multi-line documentation bound by name, short-row
padding, and the writer's quoting rules — including the `#` first-cell rule that
would otherwise turn a data row into a comment.

Two behaviors worth knowing:

- Line breaks inside a quoted field are normalized to LF, as in the spec's own
  reference algorithm.
- Free comments are not preserved when a parsed document is written back;
  markers, descriptions and column comments are.

## Testing

```bash
go test ./...                          # unit, round-trip and CSV-compatibility tests
go test -run xxx -bench . ./...        # benchmarks
go test -run xxx -fuzz FuzzParse ./... # fuzz parse → write → parse stability
```

Indicative throughput (AMD Ryzen AI 7 350, Go 1.26):

| Benchmark | Throughput |
|---|---|
| `Parse` (10k rows, quoting and comments) | ~120 MB/s |
| `Parse` plain CSV | ~130 MB/s |
| `Unmarshal` into structs | ~65 MB/s, 2 allocs/row |
| `Marshal` from structs | ~70 MB/s |
| Write a parsed document | ~165 MB/s |
