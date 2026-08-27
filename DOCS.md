# mtcsv-go — package documentation

Complete reference for `github.com/Arena7664/mtcsv-go`, a full MTCSV 1.0 reader
and conformant writer with an `encoding/json`-shaped API.

For the file format itself, see [MTCSV-SPEC.md](MTCSV-SPEC.md). Cross-references
like "see 7.2" point at a section of this page. For a short tour, see the
[README](README.md).

## Contents

1. [Installation](#1-installation)
2. [The format in one page](#2-the-format-in-one-page)
3. [Quick start](#3-quick-start)
4. [The mapping model](#4-the-mapping-model)
5. [Struct tags](#5-struct-tags)
6. [Marshalling](#6-marshalling)
7. [Unmarshalling](#7-unmarshalling)
8. [Multiple tables and multiple structs](#8-multiple-tables-and-multiple-structs)
9. [Cell types](#9-cell-types)
10. [Custom cell types](#10-custom-cell-types)
11. [The document API](#11-the-document-api)
12. [Diagnostics](#12-diagnostics)
13. [Errors](#13-errors)
14. [Options and untrusted input](#14-options-and-untrusted-input)
15. [Round-trip fidelity](#15-round-trip-fidelity)
16. [Cookbook](#16-cookbook)
17. [Performance](#17-performance)
18. [Conformance and limitations](#18-conformance-and-limitations)
19. [API index](#19-api-index)

---

## 1. Installation

```bash
go get github.com/Arena7664/mtcsv-go
```

```go
import "github.com/Arena7664/mtcsv-go"   // package name: mtcsv
```

The package has no dependencies outside the standard library. `go.mod` declares
`go 1.26.5`; lower that line if you need to build with an older toolchain (the
library itself uses nothing newer than Go 1.20, though the benchmarks use
`testing.B.Loop`, which is Go 1.24).

---

## 2. The format in one page

MTCSV is RFC 4180 CSV plus two conventions: **a blank line separates tables**,
and **a line starting with `#` carries structure or commentary**. Everything
else is ordinary CSV, so a generic CSV reader still sees all the data.

```mtcsv
# users currency=AUD schema=v2          ← marker: table name + metadata
#! Registered accounts.                 ← table description
id,name,email                           ← header (first record of the section)
#: id    (int)   unique user id         ← column comment: doc + optional type
#: email (email) primary contact
1,Alice,alice@example.com               ← data rows
2,Bob,"bob, jr.@example.com"

# tags                                  ← blank line above starts a new table
order_id,tag
10,priority
```

| Line | Meaning |
|---|---|
| blank | separates tables (not inside a quoted field) |
| `# name key=value` | table marker — only the first such line, and only before the header |
| `#! text` | table description; several lines join with `\n` |
| `#: col (type) text` | column comment; binds by name, several lines join with `\n` |
| `# anything else` | free comment, ignored |
| anything else | a record: the first one is the header, the rest are data rows |

Types and metadata are **advisory**. Nothing in the format is enforced, and this
package does not enforce it either: a column declared `(int)` whose cell says
`banana` is not a format error — it only becomes an error if you decode that
cell into a Go `int`.

---

## 3. Quick start

### Structs in, structs out

```go
type User struct {
	ID    int    `mtcsv:"id"    mtcsv.type:"int"   mtcsv.doc:"unique user id"`
	Name  string `mtcsv:"name"`
	Email string `mtcsv:"email" mtcsv.type:"email"`
}

data, err := mtcsv.Marshal([]User{{1, "Alice", "alice@example.com"}})

var users []User
err = mtcsv.Unmarshal(data, &users)
```

### A whole file of several tables

```go
type File struct {
	Users []User `mtcsv:"users,schema=v2" mtcsv.doc:"Registered accounts."`
	Tags  []Tag  `mtcsv:"tags"`
}

var f File
err := mtcsv.Unmarshal(data, &f)   // each field takes its own table
```

### Without a schema

```go
doc, err := mtcsv.Parse(data)
for _, t := range doc.Tables {
	fmt.Println(t.Name, t.ColumnNames(), len(t.Rows))
}
```

---

## 4. The mapping model

MTCSV has three levels — **document → table → row → cell** — and so does this
package's mapping.

| MTCSV | Go |
|---|---|
| document (whole file) | a container struct, a `map[string]…`, or a single slice |
| table (one section) | a slice of rows |
| row (one record) | a struct, a `map[string]T`, a `[]string`, or a single value |
| cell (one field) | `string`, number, `bool`, `time.Time`, a custom type, … |

### 4.1 What `Marshal` accepts

`Marshal(v)` inspects `v` after following pointers and interfaces:

| `v` | Result |
|---|---|
| `nil` | empty output |
| `[]T`, `[N]T` | **one table** |
| `map[string]V` | **one table per key**, keys sorted for deterministic output |
| `struct{…}` | **a container**: one table per exported field, in declaration order |
| `*Document`, `Document` | the document's tables, written as they are |
| `*Table`, `Table`, `[]*Table`, `[]Table` | those tables, written as they are |
| anything else | `*UnsupportedTypeError` |

A **top-level struct is always a container**, never a single row. This is a
deliberate rule rather than a heuristic: adding a field to a struct must never
silently change what the type means. To write one row, pass a one-element slice.

```go
mtcsv.Marshal(User{ID: 1})     // error: field ID of mtcsv.User is not a table
mtcsv.Marshal([]User{{ID: 1}}) // one table, one row
```

Inside a container, a field may be:

| Field type | Result |
|---|---|
| `[]T`, `[N]T`, `*[]T` | one table |
| `map[string]V` | one table per key |
| `struct{…}` | a one-row table |
| `*Table` | that table |
| `nil` slice, map, or pointer | an empty table (header only) |
| `int`, `string`, … | `*UnsupportedTypeError` |

### 4.2 What `Unmarshal` accepts

`Unmarshal(data, v)` requires `v` to be a non-nil pointer, then dispatches on
what it points at:

| Target | Result |
|---|---|
| `*struct{…}` | a container: tables are matched to fields (see 7.1) |
| `*map[string][]T` | one entry per table, keyed by name or position |
| `*[]T` | the **first** table, plus any later table sharing its name |
| `*[N]T` | the same, truncated to the array's length |
| `*Document`, `**Document` | the parsed document itself |
| `*[]*Table` | the tables, undecoded |
| `*any` | set to `*Document` |
| anything else | `*UnsupportedTypeError` |

### 4.3 Row shapes

Whatever holds the rows, the element type decides how a record is read or
written:

| Element type | Row mapping |
|---|---|
| `struct` (not a cell type) | fields ↔ columns by name |
| `*struct` | same; `nil` writes an all-empty row, decoding allocates |
| `map[string]T` | keys ↔ column names; header is the sorted union of keys |
| `[]string`, `[N]string` | raw cells; **row 0 is the header** in both directions |
| `any` | encode: decided by the first element's dynamic type; decode: `map[string]string` |
| any cell type (`string`, `int`, `time.Time`, a `Marshaler`, …) | a single-column table whose column is named `value` |

```go
mtcsv.Marshal([]string{"a", "b"})              // "value\na\nb\n"
mtcsv.Marshal([][]string{{"id"}, {"1"}})       // "id\n1\n"
mtcsv.Marshal([]map[string]string{{"a": "1"}}) // "a\n1\n"
```

---

## 5. Struct tags

Four tag keys are recognized. Go permits a dot in a tag key, which lets the
documentation tags carry free text — including commas — without fighting the
option list.

```go
type User struct {
	ID int `mtcsv:"id,omitempty" mtcsv.type:"int" mtcsv.doc:"unique user id, never reused"`
}
```

| Key | Applies to | Emits |
|---|---|---|
| `mtcsv` | row-struct fields (columns) and container fields (tables) | the name, plus options |
| `mtcsv.type` | row-struct fields | the `(type)` group of a `#:` line |
| `mtcsv.doc` | row-struct fields (column comment) and container fields (table description) | `#:` / `#!` lines |
| `mtcsv.meta` | container fields | extra `key=value` pairs on the marker |

### 5.1 `mtcsv` — name and options

The tag is `name` followed by comma-separated options, exactly like
`encoding/json`:

| Tag | Meaning |
|---|---|
| `mtcsv:"id"` | the column (or table) is named `id` |
| `mtcsv:""`, no tag at all | the **Go field name** is used, verbatim and case-sensitive |
| `mtcsv:",omitempty"` | the field name, with an option |
| `mtcsv:"-"` | the field is never encoded or decoded |
| `mtcsv:"-,"` | a column literally named `-` |
| `mtcsv:"id,omitempty"` | write an empty cell instead of a zero value (see 6.3) |
| `mtcsv:"users,schema=v2,currency=AUD"` | table marker metadata (container fields only) |
| `mtcsv:"rows,anon"` | the table is written with no marker line |

Unexported fields are always skipped. Options that are neither `omitempty`,
`anon`/`anonymous`, nor `key=value` pairs are ignored, so unknown options are
forward-compatible rather than fatal.

```go
type Tricky struct {
	Untagged string                  // column "Untagged"
	Opts     string `mtcsv:",omitempty"` // column "Opts"
	Dash     string `mtcsv:"-,"`         // column "-"
	Skipped  string `mtcsv:"-"`          // no column
	hidden   string                      // no column (unexported)
}
// header: Untagged,Opts,-
```

### 5.2 `mtcsv.type` — declared column type

The value is written into the column's `#:` line as `(type)` and is advisory.
Any string is legal; `string`, `int`, `number`, `bool`, `date`, `datetime` and
`enum` are the spec's recommended vocabulary.

```go
Score float64 `mtcsv:"score" mtcsv.type:"number"`
// #: score (number)
```

Two type names change encoding behavior for `time.Time`, and only for
`time.Time` (see 9.3): `date` writes `2006-01-02`, `time` writes `15:04:05`.
Everything else writes RFC 3339. Decoding never depends on the declared type —
it accepts all of those layouts regardless.

### 5.3 `mtcsv.doc` — documentation

On a **row-struct field**, the text becomes that column's comment. On a
**container field**, it becomes the table's description. Embedded newlines are
split across several `#:`/`#!` lines, because one line can't carry a break.

```go
type File struct {
	Users []User `mtcsv:"users" mtcsv.doc:"Registered accounts.\nDeactivated users are kept."`
}
```

```mtcsv
# users
#! Registered accounts.
#! Deactivated users are kept.
```

### 5.4 `mtcsv.meta` — marker metadata

Metadata may be written either as `key=value` options inside the `mtcsv` tag or
as a `mtcsv.meta` tag in marker syntax. The second form accepts quoted values,
so it is the one to use when a value contains spaces:

```go
Users []User `mtcsv:"users,schema=v2" mtcsv.meta:"source=\"orders export.json\""`
// # users schema=v2 source="orders export.json"
```

Both forms may appear together; the `mtcsv` options come first. Metadata is
advisory — this package never interprets a key.

### 5.5 Precedence

For a table's identity, the first rule that applies wins:

1. the name passed to `Encoder.EncodeTable` or `Document.AddTable`;
2. an explicit `mtcsv:"name"` tag on the container field;
3. the row type's `TableDescriptor` (see 8.6);
4. the Go field name, for a container field;
5. otherwise the table is anonymous.

Description and metadata follow the same idea: an explicit tag wins, and a
`TableDescriptor` fills in only what the tag left empty. The `anon` option
always wins — an explicitly anonymous table stays anonymous.

### 5.6 Embedded structs

An **untagged embedded struct** is flattened, so its fields become columns of
the outer table, positioned where the embedded field itself appears:

```go
type Base struct {
	ID int `mtcsv:"id"`
}
type Row struct {
	Base
	Name string `mtcsv:"name"`
}
// header: id,name
```

Rules, matching `encoding/json`:

- A **tagged** embedded struct is an ordinary field, and therefore a single cell
  — which only works if its type is a cell type (see 9).
- On a name collision, the **shallowest** field wins; at equal depth a **tagged**
  field beats an untagged one; an unresolved tie drops the name entirely.
- Embedded **pointers** are followed, and allocated on decode when needed.
- A field reached through an embedded pointer to an **unexported type** cannot be
  set by reflection. It is skipped rather than failing the decode.

---

## 6. Marshalling

```go
func Marshal(v any) ([]byte, error)

type Encoder struct{ … }
func NewEncoder(w io.Writer) *Encoder
func (e *Encoder) Encode(v any) error
func (e *Encoder) EncodeTable(name string, v any) error
func (e *Encoder) SetAlignComments(on bool)
```

`Marshal` is `NewEncoder(&buf).Encode(v)`.

### 6.1 Columns

Columns come from the row type in declaration order — struct fields (see 5),
sorted keys for map rows, or row 0 for `[][]string`. The header and its column
documentation are always written, even for an empty or nil slice, so the shape
of the data survives:

```go
mtcsv.Marshal([]User{})
// id,name,email
// #: id    (int)   unique user id
// #: email (email)
```

### 6.2 Column documentation

A column gets a `#:` line when it has a declared type, a description, or both.
Lines are aligned into columns by default, which is what makes a hand-edited
file readable:

```mtcsv
id,name,email,status
#: id     (int)   unique user id, never reused
#: email  (email) primary contact
#: email          may be blank for SSO accounts
#: status (enum)
```

Call `enc.SetAlignComments(false)` for minimal, single-space output. Alignment
never changes meaning, and both forms parse back identically.

### 6.3 `omitempty`

`omitempty` writes an **empty cell** instead of the value's zero form. Rows stay
rectangular — the cell is still there, it is just blank. This matters because
`0` and `false` are meaningful text in a CSV, while a blank cell reads as "no
value" and decodes back to the zero value.

```go
type Row struct {
	N int `mtcsv:"n,omitempty"`
	M int `mtcsv:"m"`
}
mtcsv.Marshal([]Row{{}})   // "n,m\n,0\n"
```

Empty means: `false`, `0`, `""`, a nil pointer, interface, map or slice, a
zero-length map or slice, a zero `time.Time`, and any other zero struct value.

### 6.4 Quoting

The writer quotes a cell when the format requires it, and when quoting is
needed to preserve meaning:

- the cell contains a comma, a double quote, CR or LF;
- it is the **first cell of a row and begins with `#`** — without the quotes the
  whole row would be read back as a comment and the data would vanish;
- it begins or ends with whitespace;
- it is the **only cell of the row and is empty** — otherwise the row would be a
  blank line, which separates tables.

```go
mtcsv.Marshal([]string{"", "x"})   // "value\n\"\"\nx\n"
```

Table names, metadata values and column-comment names are quoted when they
contain whitespace, a quote or `=`. Metadata **keys** are written verbatim,
because a reader scans a key as the raw run of characters before the first `=`;
a key that contains whitespace or `=` has no representation in the format and is
skipped.

### 6.5 Determinism

Regenerating a file from unchanged data produces unchanged bytes: map keys are
sorted, struct fields keep their declaration order, metadata keeps its insertion
order, and quoting is decided only by the cell's content.

### 6.6 Streaming

An `Encoder` appends. Every call after the first is separated by exactly one
blank line, so a document can be built table by table without holding it all in
memory:

```go
enc := mtcsv.NewEncoder(os.Stdout)
for _, batch := range batches {
	if err := enc.EncodeTable(batch.Name, batch.Rows); err != nil {
		return err
	}
}
```

`EncodeTable("", rows)` writes an anonymous table.

---

## 7. Unmarshalling

```go
func Unmarshal(data []byte, v any) error

type Decoder struct{ … }
func NewDecoder(r io.Reader) *Decoder
func (d *Decoder) Decode(v any) error
func (d *Decoder) DecodeTable(v any) error
func (d *Decoder) NextTable() (*Table, error)
func (d *Decoder) More() bool
func (d *Decoder) Document() (*Document, error)
func (d *Decoder) Diagnostics() []Diagnostic
func (d *Decoder) SetParseOptions(opts ParseOptions)
func (d *Decoder) Strict()
func (d *Decoder) DisallowUnknownColumns()
func (d *Decoder) DisallowUnknownTables()
```

`Unmarshal(data, v)` is `Parse(data)` followed by `doc.Decode(v)`, discarding
diagnostics. Use a `Decoder` when you want the strictness knobs, the
diagnostics, or table-at-a-time streaming.

### 7.1 Matching tables to struct fields

For a container struct, tables are assigned in three passes:

1. **Exact name.** Every named table whose name equals the field's name (from
   the tag, a `TableDescriptor`, or the Go field name) is assigned to it. A
   field may collect several tables — that is how same-named sections append
   (see 8.3). Fields tagged `anon` are skipped in this pass.
2. **Case-insensitive name.** The same, for fields still unmatched.
3. **Position.** Remaining fields, in declaration order, take the remaining
   tables in file order. This is how anonymous tables are addressed, and how an
   `anon` field gets its table.

A field with no table is left untouched — a nil slice stays nil. A table with no
field is ignored unless `DisallowUnknownTables` is set.

```go
type File struct {
	Users []User `mtcsv:"users"` // by name, wherever it appears in the file
	Other []Row                  // by field name, case-insensitively
	Extra []Row                  // whatever is left, in order
}
```

### 7.2 Matching columns to fields

Within a row struct, each column is looked up by **exact name first, then
case-insensitively**, mirroring `encoding/json`. So a header of `ID,Name`
decodes into fields tagged `id` and `name`.

- A column with no matching field is skipped, unless `DisallowUnknownColumns`
  is set, which turns it into an `*UnknownColumnError`.
- A field with no matching column is left at its zero value.
- Duplicate column names bind to the first matching field.
- Short rows were already padded by the parser, so missing trailing cells decode
  as empty.

### 7.3 Reading rows

Rows are decoded into a freshly built slice, which replaces whatever the target
held. For an array target, surplus rows are dropped and the unused tail is
zeroed.

`[][]string` is the exception to "rows are data rows": it receives the **header
as row 0**, so it round trips with what `Marshal` writes for the same type.

### 7.4 Strictness

By default decoding is forgiving, in the spirit of the format: unknown columns
and tables are ignored, and a malformed document still yields whatever data
could be read. Three switches tighten that:

| Call | Effect |
|---|---|
| `dec.Strict()` | error-severity diagnostics (`too-many-fields`, `unterminated-quote`, `record-too-large`) fail the decode with a `*DiagnosticError` |
| `dec.DisallowUnknownColumns()` | a column with no struct field fails with `*UnknownColumnError` |
| `dec.DisallowUnknownTables()` | a table with no struct field or map entry fails with `*UnknownTableError` |

`Strict` applies per call: `Decode` checks the whole document, `DecodeTable`
checks only that table's diagnostics.

### 7.5 Streaming

A `Decoder` reads and parses the whole stream on first use — a record can span
physical lines and a section only ends at a blank line, so there is no smaller
safe unit — then hands out tables one at a time:

```go
dec := mtcsv.NewDecoder(f)
for dec.More() {
	t, err := dec.NextTable()          // metadata without decoding
	if err != nil {
		return err
	}
	switch t.Name {
	case "users":
		var users []User
		if err := t.Decode(&users); err != nil {
			return err
		}
	case "tags":
		var tags []Tag
		if err := t.Decode(&tags); err != nil {
			return err
		}
	}
}
```

`DecodeTable(&v)` combines the two steps and returns `io.EOF` when no tables
remain. `Decode(&v)` consumes **all remaining** tables, so it can follow a few
`DecodeTable` calls to handle "a header table, then the rest".

---

## 8. Multiple tables and multiple structs

This is what the format exists for, so it is worth spelling out. There are four
ways to move a multi-table document in and out of Go, and they compose.

### 8.1 A container struct — one field per table

The common case. Field order is the document's table order on the way out; on
the way in, names match in any order.

```go
type Order struct {
	ID    int     `mtcsv:"id"    mtcsv.type:"int"`
	Total float64 `mtcsv:"total" mtcsv.type:"number"`
}
type LineItem struct {
	OrderID int    `mtcsv:"order_id" mtcsv.type:"int"`
	SKU     string `mtcsv:"sku"`
	Qty     int    `mtcsv:"qty"      mtcsv.type:"int"`
}

type Export struct {
	Orders []Order    `mtcsv:"orders,schema=v2" mtcsv.doc:"Completed checkouts only."`
	Items  []LineItem `mtcsv:"line_items"`
}

out, err := mtcsv.Marshal(Export{
	Orders: []Order{{1, 9.99}},
	Items:  []LineItem{{1, "ABC", 2}},
})
```

```mtcsv
# orders schema=v2
#! Completed checkouts only.
id,total
#: id    (int)
#: total (number)
1,9.99

# line_items
order_id,sku,qty
#: order_id (int)
#: qty      (int)
1,ABC,2
```

Decoding the same bytes into `Export` restores both slices. Tables the struct
does not mention are skipped; fields with no table stay nil.

### 8.2 Anonymous tables, addressed by position

A section with no marker is anonymous and is addressed by its zero-based
position among **all** sections. In a container, anonymous tables fill the fields
that name-matching left over, in order:

```go
var f struct {
	First  [][]string   // takes table 0
	Second [][]string   // takes table 1
}
mtcsv.Unmarshal([]byte("a,b\n1,2\n\nx,y\n3,4\n"), &f)
```

To write anonymous tables, tag the field `anon` (or use
`EncodeTable("", rows)`):

```go
var f struct {
	Rows []Row `mtcsv:"rows,anon"`
}
// header and rows only, no "# rows" marker
```

### 8.3 Same-named sections append

A large table may be split across the file, or appended to later. Both sections
carry the same name:

```mtcsv
# events
id,kind
1,login

# events
id,kind
2,logout
```

A container field named `events` collects **both**, in file order, so
`f.Events` has two rows. A bare `[]Event` target does the same for the first
table's name. This is the spec's optional append semantics, and this package
opts in — the alternative would silently drop half the data.

At the document level the sections stay separate until you ask:

```go
doc, _ := mtcsv.Parse(data)
len(doc.Tables)                    // 2
len(doc.Merge().Tables)            // 1
doc.TablesNamed("events")          // both, undecoded
```

`Merge` keeps the first section's header, description and metadata, realigns
later sections **by column name**, and appends any column they introduce — so
sections whose headers differ in order or width still merge correctly.

### 8.4 A map of tables

When the tables are not known at compile time, decode into a map. Keys are table
names; anonymous tables are keyed by their decimal position:

```go
// # users …   # tags …   then an unnamed third section
var byName map[string][]map[string]string
mtcsv.Unmarshal(data, &byName)
// {"users": […], "tags": […], "2": […]}
```

The key for an anonymous table is its position among **all** sections, so the
third section is `"2"` whether or not the first two were named.

Same-named sections are appended into one entry. Encoding a map writes one table
per key, sorted, with the key as the table name.

### 8.5 Heterogeneous documents

When each table needs a different Go type and the file's shape is not fixed,
walk the tables (see 7.5) or use the document API (see 11). `Table.Decode` accepts the
same targets as `Unmarshal`, so a dispatch loop stays short.

### 8.6 Self-describing row types

A row type can carry its own table identity by implementing `TableDescriptor`.
Both the encoder and the decoder use it, so the type round trips wherever it is
embedded:

```go
type AuditEntry struct {
	Action string `mtcsv:"action"`
}

func (AuditEntry) MTCSVTable() mtcsv.TableInfo {
	return mtcsv.TableInfo{
		Name:        "audit_log",
		Description: "Every action taken.",
		Meta:        mtcsv.Metadata{{Key: "schema", Value: "v3"}},
	}
}

type File struct {
	Entries []AuditEntry   // no tag needed: the table is "audit_log"
}
```

An explicit `mtcsv:"name"` tag on the field overrides the descriptor's name;
description and metadata fall back to the descriptor when the tag omits them.
`TableInfo{Anonymous: true}` makes the type write no marker at all.

---

## 9. Cell types

Cells are text. Both directions understand the same set of types.

### 9.1 Encoding

| Go type | Cell |
|---|---|
| `string` | verbatim |
| `bool` | `true` / `false` |
| all int and uint kinds | base 10 |
| `float32`, `float64` | shortest exact decimal; exponent form below 1e-6 or at/above 1e21; `NaN`, `Inf`, `-Inf` |
| `[]byte` | the bytes **verbatim**, not base64 — MTCSV is a human-readable format |
| `time.Time` | RFC 3339, or `2006-01-02` / `15:04:05` when the column is typed `date` / `time` |
| pointer, interface | the pointee, or an empty cell when nil |
| `Marshaler` | whatever `MarshalMTCSV` returns |
| `encoding.TextMarshaler` | whatever `MarshalText` returns |
| anything else | `*UnsupportedTypeError` |

Nested slices, maps and structs are rejected rather than flattened: MTCSV is
rectangular by design, and a format that quietly invents an encoding for nested
data would not survive a round trip.

### 9.2 Decoding

| Go type | Accepts |
|---|---|
| `string` | any cell, verbatim |
| `bool` | `1 t true y yes on` / `0 f false n no off`, case-insensitive |
| int, uint kinds | base 10, surrounding whitespace tolerated, range-checked per width |
| floats | Go float syntax, including exponents, `NaN`, `Inf` |
| `[]byte` | the cell's bytes |
| `time.Time` | RFC 3339, `2006-01-02T15:04:05`, `2006-01-02 15:04:05[.fff][Z07:00]`, `2006-01-02 15:04`, `2006-01-02`, `15:04:05`, `15:04` |
| pointer | allocates; an empty cell leaves it nil |
| `Unmarshaler` | whatever `UnmarshalMTCSV` accepts |
| `encoding.TextUnmarshaler` | whatever `UnmarshalText` accepts |
| `any` | the cell as a `string` |

**An empty cell is the zero value.** For every type above except `string` and
custom unmarshalers, `""` decodes to the zero value instead of failing, because
a blank cell in a CSV means "no value", and the format has no separate null.
Custom `Unmarshaler`s do see the empty string and may treat it as they like;
`TextUnmarshaler`s do not — they get the zero value without being called.

A cell that will not convert produces an `*UnmarshalTypeError` naming the table,
column and row, and wrapping the underlying `strconv` error.

### 9.3 Times

Declared types steer only the **encoder**, so a column typed `date` stays a
date in the file:

```go
type Event struct {
	Day time.Time `mtcsv:"day" mtcsv.type:"date"`
	At  time.Time `mtcsv:"at"`
}
// day,at
// #: day (date)
// 2024-05-06,2024-05-06T07:08:09Z
```

Decoding tries the layouts in order and takes the first that parses, so both
columns come back regardless of how they were written. A time-only cell yields
year 0; a date-only cell yields midnight UTC.

---

## 10. Custom cell types

Implement either interface to control a single cell:

```go
type Marshaler interface {
	MarshalMTCSV() (string, error)
}

type Unmarshaler interface {
	UnmarshalMTCSV(cell string) error
}
```

`encoding.TextMarshaler` / `TextUnmarshaler` work too and are checked after the
MTCSV-specific pair, so existing types (`net.IP`, `uuid.UUID`, `time.Time`, your
own) work with no extra code.

```go
type Money int64 // cents

func (m Money) MarshalMTCSV() (string, error) {
	return fmt.Sprintf("%d.%02d", m/100, m%100), nil
}

func (m *Money) UnmarshalMTCSV(cell string) error {
	if cell == "" {
		*m = 0
		return nil
	}
	var dollars, cents int64
	if _, err := fmt.Sscanf(cell, "%d.%02d", &dollars, &cents); err != nil {
		return err
	}
	*m = Money(dollars*100 + cents)
	return nil
}

type Line struct {
	Total Money `mtcsv:"total" mtcsv.type:"currency"`
}
```

Method sets follow the usual Go rule: a `MarshalMTCSV` on the value receiver
works for both values and pointers, while `UnmarshalMTCSV` must be on the
pointer receiver. The encoder takes an addressable copy when a marshaler is
declared on the pointer receiver, so both spellings encode.

A type that implements either interface is treated as a **cell**, not a row —
so a struct with a `MarshalMTCSV` method inside a slice becomes a
single-column table, not a set of columns.

---

## 11. The document API

Parsing exposes the whole document, which is what editors, linters and
converters need.

```go
func Parse(data []byte) (*Document, error)
func ParseString(s string) (*Document, error)
func ParseReader(r io.Reader) (*Document, error)
func ParseWith(data []byte, opts ParseOptions) (*Document, error)
func Valid(data []byte) bool
```

**`Parse` always returns a usable document.** The error is non-nil only when an
error-severity diagnostic was found, so `doc, _ := Parse(data)` is a legitimate
best-effort read, and `doc, err := Parse(data)` is the strict one.

```go
type Document struct {
	Tables      []*Table
	Diagnostics []Diagnostic
}
```

| Method | Purpose |
|---|---|
| `Table(name)` | first table with that name; also accepts a decimal position |
| `TablesNamed(name)` | every section with that name, in file order |
| `At(i)` | the table at that position |
| `Merge()` | append same-named sections into one table (new document) |
| `Err()` | the error-severity diagnostics as a `*DiagnosticError` |
| `Decode(v)` | decode into Go values, as `Unmarshal` does |
| `AddTable(name, v)` | build a table from Go values and append it |
| `Bytes()`, `String()`, `WriteTo(w)` | write the document back out |

```go
type Table struct {
	Name        string   // "" when anonymous
	Anonymous   bool
	Index       int      // position among all sections, in file order
	Description string   // joined "#!" lines
	Meta        Metadata // marker key=value pairs, in file order
	Columns     []Column
	Rows        [][]string
	Line        int      // 1-based line where the section starts
}
```

| Method | Purpose |
|---|---|
| `ID()` | the name, or the decimal position when anonymous |
| `ColumnNames()`, `Column(name)`, `ColumnIndex(name)` | header access |
| `Get(row, column)` | one cell, empty when out of range |
| `Records()` | rows as `[]map[string]string` |
| `SetColumns(names…)`, `AppendRow(cells…)` | build a table by hand (rows are padded) |
| `Decode(v)` | decode this table's rows |
| `String()` | this section as MTCSV |

```go
type Column struct {
	Name        string
	Index       int
	Type        string // declared "(type)", advisory, "" when undeclared
	Description string // joined "#:" lines
}
```

`Metadata` is an ordered `[]MetaEntry` with `Get`, `Has`, `Set`, `Map` and
`String`; `ParseMetadata` reads marker syntax. Order is preserved so that
rewriting a file does not reshuffle its markers.

Building a document by hand:

```go
doc := &mtcsv.Document{}
if err := doc.AddTable("users", users); err != nil {
	return err
}
t := &mtcsv.Table{Name: "totals"}
t.SetColumns("label", "amount")
t.AppendRow("subtotal", "9.99")
doc.Tables = append(doc.Tables, t)
os.Stdout.Write(doc.Bytes())
```

---

## 12. Diagnostics

Every diagnostic in the spec is reported, plus one extension for resource
limits. Diagnostics carry a code, a severity, a message, the 1-based physical
line and the table's identity.

| Code | Severity | Condition |
|---|---|---|
| `too-many-fields` | error | a data row has more fields than the header defines; the surplus cells are kept on the row |
| `unterminated-quote` | error | a quoted field is still open at end of file |
| `record-too-large` | error | a record exceeded `ParseOptions.MaxRecordBytes` and was abandoned (extension) |
| `unknown-column` | warning | a `#:` comment names a column the header does not have |
| `duplicate-column` | warning | two or more header columns share a name |
| `no-header` | warning | a section has structural lines but no record |
| `ragged-short-row` | hint | a legal short row was padded; opt in with `ParseOptions.Hints` |

```go
doc, err := mtcsv.Parse(data)
for _, d := range doc.Diagnostics {
	fmt.Printf("%s:%d: %s: %s (%s)\n", path, d.Line, d.Severity, d.Message, d.Code)
}
if err != nil {
	// at least one error-severity diagnostic
}
```

Only error-severity diagnostics make `Parse` return an error or fail a strict
decode. Warnings and hints never do — they are for linters and editors.

---

## 13. Errors

Every error this package returns is one of these types, so `errors.As` can
inspect it.

| Type | Returned when |
|---|---|
| `*InvalidUnmarshalError` | the target is nil or not a pointer |
| `*UnmarshalTypeError` | a cell will not convert; names the table, column, row, and wraps the cause |
| `*UnsupportedTypeError` | a Go type has no MTCSV representation |
| `*UnknownColumnError` | `DisallowUnknownColumns` and the document has an extra column |
| `*UnknownTableError` | `DisallowUnknownTables` and the document has an extra table |
| `*DiagnosticError` | `Parse`, `Document.Err`, or a strict decode found error-severity diagnostics |

```go
var typeErr *mtcsv.UnmarshalTypeError
if errors.As(err, &typeErr) {
	fmt.Printf("table %s, column %q, row %d: %q is not a %s\n",
		typeErr.Table, typeErr.Column, typeErr.Row, typeErr.Value, typeErr.Type)
}

var diagErr *mtcsv.DiagnosticError
if errors.As(err, &diagErr) {
	for _, d := range diagErr.Diagnostics {
		fmt.Println(d) // "line 3: error: … (too-many-fields)"
	}
}
```

`UnmarshalTypeError.Unwrap` exposes the underlying `strconv` error, so
`errors.Is(err, strconv.ErrRange)` works.

An error from `Marshal` is wrapped with the table, column and row it came from:

```
mtcsv: table users, column "meta", row 0: mtcsv: unsupported type: map[string]int: MTCSV cells are flat text
```

---

## 14. Options and untrusted input

```go
type ParseOptions struct {
	Hints          bool // enable hint-severity diagnostics
	MaxRecordBytes int  // bound one record; 0 means unlimited
}

mtcsv.ParseWith(data, mtcsv.ParseOptions{MaxRecordBytes: 1 << 20})
dec.SetParseOptions(mtcsv.ParseOptions{MaxRecordBytes: 1 << 20})
```

For untrusted input, set `MaxRecordBytes`. A quoted field may contain newlines,
so a single record can span the whole file; the limit is enforced *while* the
record is read, so an oversized record is abandoned rather than buffered.
The record is reported as `record-too-large` and reading resumes on the next
physical line.

Other things the spec asks consumers to keep in mind:

- **Never evaluate metadata, descriptions or type names.** They are advisory
  strings from the file. This package never interprets them.
- **Do not assume one line is one record.** Use the parsed rows, not a line
  count, for any sampling or truncation.
- **Spreadsheet formula injection** is a CSV-wide concern: a cell starting with
  `=`, `+`, `-` or `@` may be executed by a spreadsheet application. Apply the
  usual mitigations when re-exporting; this package writes cells verbatim.
- **Encoding.** Input must be UTF-8. A leading BOM is stripped; nothing else is
  transcoded.

---

## 15. Round-trip fidelity

`Parse` → `Document.Bytes()` → `Parse` is stable: the second document has the
same tables, names, metadata, descriptions, columns, types and rows as the
first. Writing is idempotent, and the result is valid CSV that a generic reader
can consume. Three normalizations happen on the first pass:

- **Line endings.** CR LF and lone CR become LF, including inside quoted fields,
  as in the spec's own reference algorithm.
- **Free comments are dropped.** Markers, metadata, descriptions and column
  comments survive; `# just a note` lines do not. They carry no data by
  definition, and preserving their position would mean modelling the file as a
  token stream rather than as tables.
- **Quoting is normalized.** A cell is quoted if and only if the writer's rules
  require it, so `"abc"` becomes `abc`. Both read back identically.

Column-comment alignment, blank-line runs and whitespace inside `#` lines are
also normalized. Cell content — the data — is never altered.

One asymmetry worth knowing: a column comment that binds to **duplicate** column
names is written once per column, so re-reading such a file gives each of those
columns the text twice. Duplicate column names are discouraged by the spec and
reported as a `duplicate-column` diagnostic.

---

## 16. Cookbook

### Read a file, tolerating problems

```go
data, err := os.ReadFile(path)
if err != nil {
	return err
}
doc, parseErr := mtcsv.Parse(data) // keep going even if parseErr != nil
var users []User
if t := doc.Table("users"); t != nil {
	if err := t.Decode(&users); err != nil {
		return err
	}
}
for _, d := range doc.Diagnostics {
	log.Printf("%s:%d: %s: %s", path, d.Line, d.Severity, d.Message)
}
```

### Enforce a schema strictly

```go
dec := mtcsv.NewDecoder(f)
dec.Strict()
dec.DisallowUnknownColumns()
dec.DisallowUnknownTables()

var file Export
if err := dec.Decode(&file); err != nil {
	return fmt.Errorf("%s: %w", path, err)
}
```

### Lint a file

```go
doc, _ := mtcsv.ParseWith(data, mtcsv.ParseOptions{Hints: true})
for _, d := range doc.Diagnostics {
	fmt.Printf("%s:%d: %s: %s [%s]\n", path, d.Line, d.Severity, d.Message, d.Code)
}
```

### Convert MTCSV to JSON

```go
doc, err := mtcsv.Parse(data)
if err != nil {
	return err
}
out := map[string][]map[string]string{}
for _, t := range doc.Merge().Tables {
	out[t.ID()] = t.Records()
}
return json.NewEncoder(w).Encode(out)
```

### Convert a folder of CSVs into one MTCSV

```go
enc := mtcsv.NewEncoder(out)
for _, path := range paths {
	records, err := csv.NewReader(mustOpen(path)).ReadAll()
	if err != nil {
		return err
	}
	name := strings.TrimSuffix(filepath.Base(path), ".csv")
	if err := enc.EncodeTable(name, records); err != nil { // [][]string: row 0 is the header
		return err
	}
}
```

### Append a table to an existing file

```go
doc, err := mtcsv.Parse(data)
if err != nil {
	return err
}
if err := doc.AddTable("events", newEvents); err != nil {
	return err
}
return os.WriteFile(path, doc.Bytes(), 0o644)
```

Appending a *section* with an existing name is legal too, and readers with
append semantics — including this one — will treat both as one table (see 8.3).

### Stream a large file table by table

```go
dec := mtcsv.NewDecoder(f)
for {
	t, err := dec.NextTable()
	if err == io.EOF {
		break
	} else if err != nil {
		return err
	}
	if t.Name != "events" {
		continue
	}
	var batch []Event
	if err := t.Decode(&batch); err != nil {
		return err
	}
	if err := process(batch); err != nil {
		return err
	}
}
```

### Normalize a file in place

```go
doc, err := mtcsv.Parse(data)
if err != nil {
	return err // refuse to rewrite a file with errors
}
return os.WriteFile(path, doc.Bytes(), 0o644)
```

### Document columns without struct tags

```go
t := doc.Table("users")
t.Column("email").Type = "email"
t.Column("email").Description = "primary contact\nbounces are retried for 72h"
t.Description = "Registered accounts."
t.Meta.Set("schema", "v2")
```

---

## 17. Performance

Measured on an AMD Ryzen AI 7 350 with Go 1.26, `go test -bench .`:

| Benchmark | Throughput | Allocations |
|---|---|---|
| `Parse`, 10k rows with quoting and comments | ~130 MB/s | 2 per row |
| `Parse`, plain CSV | ~130 MB/s | 1 per row |
| `Unmarshal` into structs | ~66 MB/s | 2 per row |
| `Unmarshal` into `[]map[string]string` | ~39 MB/s | 16 per row |
| `Marshal` from structs | ~72 MB/s | 3 per row |
| Write a parsed document | ~194 MB/s | — |
| `Table.Get` | ~8 ns | 0 |

Notes on the implementation, for anyone tuning around it:

- A cell that needs no unquoting is a **substring of the input**, not a copy, so
  a plain record allocates once for its `[]string` and nothing else. Keep the
  input alive only as long as you keep the parsed document.
- Struct layouts are cached per type, so repeated `Marshal`/`Unmarshal` of the
  same types does no tag parsing.
- Column comments bind through a name index, so a wide table with a comment per
  column stays linear.
- Decoding into structs is roughly twice as fast as into maps; prefer a struct
  when the shape is known.
- `Decoder` reads the whole stream before decoding. For files too large to hold
  in memory, split them upstream — the format's blank-line separator makes that
  safe as long as you do not cut inside a quoted field.

---

## 18. Conformance and limitations

This package is a **full reader** and a **conformant writer** in the spec's
sense:

- quote-aware line classification, so a `#` or a blank line inside a quoted
  field is data, not structure;
- markers with quoted names and `key=value` metadata;
- table descriptions, joined across several `#!` lines;
- column comments bound by name, with declared types, multi-line text and
  first-type-wins resolution;
- short-row padding and over-long-row retention;
- every diagnostic;
- optional same-name append semantics and the `Hints` diagnostics;
- the writer's quoting rules, including the `#` first-cell rule and the
  lone-empty-cell rule.

Deliberate limitations:

- **Type inference is not implemented.** Declared types are preserved and
  surfaced; nothing is guessed. Inference is a display concern, and guessing
  would make `Column.Type` mean two different things.
- **Types are not enforced.** A cell that contradicts its column's declared type
  is not an error, exactly as the spec requires. It only fails if you decode it
  into an incompatible Go type.
- **Free comments are not preserved** on rewrite (see 15).
- **Line breaks inside quoted fields are normalized to LF** (see 15).
- **Fields behind an embedded pointer to an unexported type are skipped** on
  decode; reflection cannot set them (see 5.6).
- A lenient tokenizer is used for a bare `"` inside an otherwise unquoted field,
  matching the spec's reference tokenizer rather than strict RFC 4180.

---

## 19. API index

**Top level** — `Marshal`, `Unmarshal`, `Valid`, `Parse`, `ParseString`,
`ParseReader`, `ParseWith`, `ParseMetadata`.

**Streaming** — `NewEncoder`, `Encoder.Encode`, `Encoder.EncodeTable`,
`Encoder.SetAlignComments`; `NewDecoder`, `Decoder.Decode`,
`Decoder.DecodeTable`, `Decoder.NextTable`, `Decoder.More`, `Decoder.Document`,
`Decoder.Diagnostics`, `Decoder.SetParseOptions`, `Decoder.Strict`,
`Decoder.DisallowUnknownColumns`, `Decoder.DisallowUnknownTables`.

**Document model** — `Document` (`Tables`, `Diagnostics`, `Table`,
`TablesNamed`, `At`, `Merge`, `Err`, `Decode`, `AddTable`, `Bytes`, `String`,
`WriteTo`), `Table` (`Name`, `Anonymous`, `Index`, `Description`, `Meta`,
`Columns`, `Rows`, `Line`, `ID`, `ColumnNames`, `Column`, `ColumnIndex`, `Get`,
`Records`, `SetColumns`, `AppendRow`, `Decode`, `String`), `Column`,
`Metadata`, `MetaEntry`.

**Extension points** — `Marshaler`, `Unmarshaler`, `TableDescriptor`,
`TableInfo`.

**Diagnostics and errors** — `Diagnostic`, `Severity` (`SeverityError`,
`SeverityWarning`, `SeverityHint`), the `Diag*` code constants,
`DiagnosticError`, `InvalidUnmarshalError`, `UnmarshalTypeError`,
`UnsupportedTypeError`, `UnknownColumnError`, `UnknownTableError`.

**Options** — `ParseOptions` (`Hints`, `MaxRecordBytes`).

Run `go doc github.com/Arena7664/mtcsv-go` for the generated reference, or
`go doc -all` for every symbol with its comment.
