# The MTCSV Format - Specification

**Multi-Table CSV**
Version 1.0 · Status: Stable

---

## Abstract

MTCSV ("Multi-Table CSV") is a plain-text, human-readable data format that
stores **several CSV tables in one file**, each optionally **named**,
**described**, and **annotated per column**, while remaining readable - and
parseable - by an ordinary CSV reader. It adds exactly two conventions on top of
[RFC 4180](https://www.rfc-editor.org/rfc/rfc4180) CSV, both of which existing
CSV tooling already tolerates: **blank lines** separate tables, and lines
beginning with **`#`** carry structure or commentary. A file with no `#` lines
and no blank lines is just a CSV file.

This document defines the format precisely enough to write conformant readers,
writers, linters, and editor tooling.

---

## Table of contents

1. [Introduction](#1-introduction)
2. [Conformance and terminology](#2-conformance-and-terminology)
3. [File-level conventions](#3-file-level-conventions)
4. [Lexical model](#4-lexical-model)
5. [The CSV dialect](#5-the-csv-dialect)
6. [Line classification](#6-line-classification-the-core-algorithm)
7. [Structural elements](#7-structural-elements)
8. [Column types](#8-column-types)
9. [Table identity and addressing](#9-table-identity-and-addressing)
10. [Reference parsing algorithm](#10-reference-parsing-algorithm)
11. [Writing MTCSV](#11-writing-mtcsv)
12. [Diagnostics](#12-diagnostics)
13. [Grammar (ABNF)](#13-grammar-abnf)
14. [Conformance classes](#14-conformance-classes)
15. [Worked examples](#15-worked-examples)
16. [Security considerations](#16-security-considerations)
17. [Versioning and extensibility](#17-versioning-and-extensibility)
18. [Design rationale and FAQ](#18-design-rationale-and-faq)
19. [Appendix A: Quick reference](#appendix-a-quick-reference)
20. [Appendix B: Relationship to reference tooling](#appendix-b-relationship-to-reference-tooling)

---

## 1. Introduction

### 1.1 Motivation

CSV is the lowest-common-denominator tabular format: universally readable,
diff-friendly, and trivial to emit. Its limitations are equally well known - a
CSV file holds exactly one table, carries no place for column documentation, and
has no room for metadata. People routinely work around this by shipping many CSV
files in a folder, or by inventing ad-hoc "section" conventions inside one file
that generic tools cannot understand.

MTCSV standardizes that instinct. It lets a single file hold many tables, each
with a name, an optional prose description, optional per-column documentation,
optional per-column type hints, and optional key/value metadata - **without**
sacrificing the property that makes CSV valuable: any CSV reader can still open
the file and get sensible rows out of it.

### 1.2 Design goals

- **CSV-compatible.** Every MTCSV file is valid CSV. A generic RFC 4180 reader
  can consume it as one ragged table; structural lines simply appear as
  single-cell rows and blank lines as record breaks.
- **Human-first.** The format is meant to be read and edited by hand. Structure
  is expressed with visible line breaks and a single sigil (`#`), not with
  escape-heavy syntax.
- **Self-documenting.** Tables and columns can carry descriptions and type hints
  inline, next to the data they describe.
- **Trivially parseable.** A full reader can be built in a few dozen lines on top
  of any existing CSV tokenizer.
- **Losslessly degradable.** Tools that understand none of the extensions still
  get the data; tools that understand some get more; nothing breaks.

### 1.3 Non-goals

MTCSV is deliberately **rectangular and flat**. It does **not** provide:

- nested or hierarchical records,
- references or foreign keys between tables,
- a schema/validation language,
- typed value enforcement,
- binary encoding.

If a use case needs those, a different format (SQLite, Parquet, JSON Lines,
Protocol Buffers) is the right tool. MTCSV's value is being *dumb, textual, and
CSV-shaped*. Extensions that would compromise that property are out of scope.

---

## 2. Conformance and terminology

### 2.1 Requirement keywords

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this
document are to be interpreted as described in
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and
[RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they
appear in all capitals.

Passages explicitly marked *(informative)* are non-normative. Everything else is
normative.

### 2.2 Terminology

| Term | Definition |
|------|------------|
| **File** | A complete MTCSV document: a sequence of physical lines. |
| **Physical line** | A maximal run of characters terminated by a line break or by end-of-file. |
| **Line break** | `LF` (`U+000A`), `CR LF` (`U+000D U+000A`), or a lone `CR` (`U+000D`). |
| **Record** | One CSV row. A record occupies one or more physical lines (more than one only when a quoted field contains a line break). |
| **Field** | One comma-delimited value within a record. |
| **Cell** | The logical, unquoted value of a field. |
| **Section** | A maximal run of non-blank lines delimited by blank lines (subject to §6). A section describes one table (or one part of a table; see §9). |
| **Table** | The logical entity a section (or several same-named sections) represents. |
| **Header** | The first record in a section; its fields name the columns. |
| **Column** | A position in a table, defined by the header; has a name and index. |
| **Data row** | A record in a section other than the header. |
| **Marker** | A `#`-line that names a table: `# name`. |
| **Table description** | A `#!`-line providing prose about the whole table. |
| **Column comment** | A `#:`-line documenting one column. |
| **Free comment** | Any other `#`-line; ignored by data consumers. |
| **Reader** | Software that parses MTCSV into tables. |
| **Writer** | Software that emits MTCSV. |

---

## 3. File-level conventions

### 3.1 File extension

Files SHOULD use the extension **`.mtcsv`**.

### 3.2 Media type *(informative)*

No media type is registered with IANA. Tools MAY use `text/mtcsv` by convention
and SHOULD treat `text/csv` and `text/plain` as acceptable fallbacks. Because
every MTCSV file is also valid CSV, labeling a file `text/csv` is not incorrect,
merely lossy.

### 3.3 Character encoding

MTCSV files MUST be encoded in **UTF-8**. Readers:

- MUST accept UTF-8;
- SHOULD strip a leading UTF-8 byte-order mark (`U+FEFF`, bytes `EF BB BF`) if
  present, treating it as absent;
- MUST NOT require a BOM.

Writers SHOULD NOT emit a BOM.

### 3.4 Line endings

Readers MUST accept `LF`, `CR LF`, and lone `CR` as line breaks, and MAY accept a
mixture within one file. Writers SHOULD emit `LF` and SHOULD be consistent.

Note that line breaks **inside quoted fields** are data, not record separators
(see §5.3). Consequently the number of physical lines in a file is not
necessarily the number of records.

---

## 4. Lexical model

MTCSV is parsed in two layers.

1. **Physical layer.** The byte stream is decoded as UTF-8 and split into
   physical lines at line breaks.

2. **Logical layer.** Physical lines are grouped into *records*, *structural
   lines* (markers, descriptions, column comments, free comments), and *blank
   lines*. Grouping is **stateful**: whether a given physical line is a
   structural/blank line or part of a record depends on whether a quoted field
   is currently open (see §6). A line break encountered while a quote is open is
   ordinary field data and does not end the record.

This two-layer model is the single most important thing an implementer must get
right. A naive line-by-line split that ignores quote state will misinterpret any
file containing a quoted field with an embedded newline or an embedded `#` at
line start.

---

## 5. The CSV dialect

MTCSV records use the CSV dialect of RFC 4180 with the clarifications below. In
case of ambiguity in RFC 4180, this section governs.

### 5.1 Delimiter

The field delimiter is the comma, `U+002C` (`,`). It is fixed; MTCSV does not
support alternate delimiters.

### 5.2 Fields

A field is either **quoted** or **unquoted**.

- An **unquoted** field is a (possibly empty) run of characters containing
  neither a comma, a double quote, `CR`, nor `LF`.
- A **quoted** field begins with a double quote `U+0022` (`"`) and ends with the
  next unescaped double quote. Between them it MAY contain commas, `CR`, `LF`,
  and escaped double quotes.

### 5.3 Quoting and escaping

Within a quoted field, a literal double quote is written as **two double
quotes** (`""`). A quoted field MAY span multiple physical lines; every line
break between the opening and closing quote is part of the cell value.

### 5.4 Unquoting (reader)

To obtain a cell's logical value from a field's raw text:

- If the raw text begins and ends with `"` and has length ≥ 2, remove the outer
  quotes and replace every `""` with `"`.
- Otherwise, use the raw text verbatim.

Whitespace is **significant**. Readers MUST NOT trim spaces or tabs from cell
values. `a , b` is three characters in the first cell region only if written
literally; `"a"` and `a` both yield the cell `a`, but ` a ` yields `␠a␠`.

### 5.5 Empty fields and rows

An empty field is permitted and yields the empty-string cell. A record such as
`,,` has three empty cells.

> **Boundary case - the empty single-column row.** A record consisting of a
> single empty unquoted field would be written as an empty physical line, which
> is indistinguishable from a section separator (§6). Therefore a single-column
> table **cannot** represent a row whose only cell is the empty string as a bare
> empty line. Writers that must represent such a row MUST quote the cell (`""`),
> producing a non-blank line. Readers encountering a bare empty line MUST treat
> it as a separator, never as a data row.

### 5.6 Lenient quote handling *(informative)*

RFC 4180 does not define behavior for a `"` appearing inside an otherwise
unquoted field (e.g. `ab"cd`). Readers MAY handle such input leniently. The
reference tokenizer treats any `"` seen in the unquoted state as opening a quoted
span; this is a tolerant superset of RFC 4180 and is not required.

---

## 6. Line classification (the core algorithm)

At the logical layer, each physical line that is **not a continuation of an open
quoted field** is classified into exactly one category. Continuation lines (those
reached while a quote opened on an earlier line is still open) are never
classified; they are consumed as field data by the record in progress.

Let `L` be such a line, and let `rest` be the substring of `L` following the
first `#` character, if any. Classification proceeds in this order:

1. **Blank line.** If `L` consists solely of whitespace (spaces and/or tabs) or
   is empty → **BLANK** (a section separator; see §7.1).

2. Otherwise, if `L` matches `^[ \t]*#` (optional leading whitespace, then `#`):

   1. If `rest` begins with `:` → **COLUMN COMMENT** (`#:`; see §7.7).
   2. Else if `rest` begins with `!` → **TABLE DESCRIPTION** (`#!`; see §7.6).
   3. Else if `rest` begins with whitespace followed by a non-whitespace
      character, **and** the current section has not yet seen a marker **and**
      has not yet seen its header record → **MARKER** (`# name`; see §7.2).
   4. Else → **FREE COMMENT** (see §7.5).

3. Otherwise → the **start of a RECORD** (see §5). The record may consume
   subsequent physical lines if it opens a quote that does not close on `L`.

### 6.1 Classification notes (normative)

- Leading whitespace before `#` is permitted for every `#`-line category.
- `#:` and `#!` are recognized **anywhere** within a section, before or after the
  header, and interleaved with data rows.
- A **marker** is recognized **only** as the first qualifying `# name` line and
  **only** before the header record. A second `# name`-style line, or any
  `# name`-style line appearing after the header, is a **free comment**.
- `#` immediately followed by a non-whitespace, non-`:`, non-`!` character (e.g.
  `#foo`) is a **free comment**, never a marker. A marker REQUIRES whitespace
  between `#` and the name.
- A bare `#` with nothing after it is a **free comment**.
- The sequences `#:` and `#!` are **reserved**. A free comment MUST NOT be
  relied upon to render as prose if its first post-`#` character is `:` or `!`;
  such lines are always a column comment or description respectively.

### 6.2 Why order matters *(informative)*

Because `#:` and `#!` are tested before the marker and free-comment rules, they
are unambiguous regardless of position. Because the marker rule additionally
requires "no marker yet and no header yet," the very first `# something` line of
a section is its name and every later one is a comment - which is what lets you
annotate a table with ordinary `#` notes without accidentally renaming it.

---

## 7. Structural elements

### 7.1 Sections and separation

A **section** is a maximal run of consecutive non-BLANK lines. A **BLANK** line
(§6, step 1) ends the current section.

- One or more consecutive BLANK lines act as a single separator; empty sections
  are never produced.
- BLANK lines before the first section and after the last section are ignored.
- A BLANK line encountered while a quoted field is open is **not** a separator;
  it is field data (§4, §6).

Each section describes one table, except that multiple sections MAY share a name
(§9).

### 7.2 Table markers and metadata

A **marker** names a table and optionally attaches metadata:

```
# <name> [<key>=<value> ...]
```

- The marker MUST be the first line of its section that could qualify as a marker
  (§6.1). Sections MAY omit the marker entirely, in which case the table is
  **anonymous** (§9).
- **Name.** The name is the first whitespace-delimited token after `#`. It MAY
  be a quoted string (§5.3); readers SHOULD support quoted names so that a name
  may contain spaces or `=`. When unquoted, the name is a run of non-whitespace
  characters.
- **Metadata.** Zero or more whitespace-separated `key=value` pairs MAY follow
  the name. For each token containing `=`, the key is the text before the first
  `=` and the value is the text after it. A token whose `=` is at position 0
  (empty key) is ignored. Tokens without `=` are ignored. Values MAY be quoted to
  contain whitespace; readers SHOULD support quoted values.

Metadata is **advisory**. The format assigns no meaning to any key. Applications
define their own keys (e.g. `schema=v2`, `currency=AUD`, `source=export.json`).
Readers MUST ignore keys they do not recognize and MUST NOT fail on them.

> **Implementation note *(informative)*.** A minimal reader MAY split the marker
> on whitespace and read only the first token as the name, ignoring quoting. Such
> a reader will mis-handle names containing spaces. Writers that need portable
> names SHOULD avoid whitespace in names.

Examples:

```
# users
# users currency=AUD schema=v2
# "line items" source=orders.json
```

### 7.3 Header row

The **header** is the first **RECORD** in the section (i.e. the first line
classified as the start of a record per §6, step 3). Marker, description, column
comment, and free comment lines are not records and do not count.

- The header's fields, unquoted, are the **column names**, in order. The header
  therefore defines the column **count**, **order**, and **names**.
- Column names MAY be empty and MAY repeat (§7.7, §12).
- A well-formed section MUST contain a header record. A section with only
  `#`-lines and no record is degenerate; readers SHOULD tolerate it as a table
  with zero columns and zero rows, and MAY emit a diagnostic.

### 7.4 Data rows

Every record after the header, until the section ends, is a **data row**.

- **Short rows.** A data row with fewer fields than the header defines is valid;
  the missing trailing cells are treated as empty. Readers MUST accept short rows
  and pad them.
- **Long rows.** A data row with more fields than the header defines is
  malformed. Readers SHOULD emit a diagnostic (§12). Recovery is
  implementation-defined: a reader MAY ignore the surplus fields, MAY retain them
  as unnamed trailing cells, or MAY reject the file. Readers MUST NOT silently
  corrupt earlier cells.
- A `#`-line between two data rows is a comment; it does **not** split the
  section. Only a BLANK line ends a section.

### 7.5 Free comments

Any `#`-line not classified as a marker, description, or column comment is a
**free comment**. Free comments carry no data semantics and MUST be ignored by
data consumers. They MAY appear anywhere in a section and MAY be used freely for
human notes.

```
# This whole line is a free comment.
    # Leading whitespace is fine.
#________ visual rule _________
```

### 7.6 Table descriptions

A **table description** documents the whole table:

```
#! <free text>
```

- The text following `#!` (with surrounding whitespace trimmed) is the
  description.
- A section MAY contain multiple `#!` lines. Their texts are concatenated in
  file order, joined by a single `LF`, to form one multi-line description. This
  lets a description span several lines.
- Description lines MAY appear anywhere in the section; position does not affect
  meaning. By convention they SHOULD appear directly under the marker.
- The text is free text: it needs no escaping and MAY contain commas, colons,
  quotes, and other punctuation, because description lines are recognized before
  CSV tokenization (§4).

```
# orders
#! Completed checkouts only. Carts in progress live in a separate store.
#! Money columns are in the table's `currency` metadata unit.
id,total
```

### 7.7 Column comments

A **column comment** documents one column and MAY declare its type:

```
#: <column-name> [(<type>)] [<description>]
```

Parsing a column-comment line:

1. Skip the `#:` and any following whitespace.
2. Read the **column name**: either a quoted string (§5.3) or a run of
   non-whitespace characters.
3. Optionally, after whitespace, read a **type group**: a `(` … `)` pair. The
   text between the parentheses, trimmed, is the declared type (§8). The type
   group, if present, MUST be separated from the name by whitespace and MUST be
   the first token after the name.
4. The remainder of the line, trimmed, is the **description** (free text; no
   escaping; may contain any punctuation).

**Binding.** A column comment binds to **every** header column whose name equals
the resolved column name (exact string equality on unquoted names):

- If no column matches, the comment does not bind; readers SHOULD emit an
  `unknown-column` diagnostic (§12) and otherwise ignore it.
- If several columns match (duplicate names), the comment binds to all of them.

**Multi-line descriptions.** Multiple `#:` lines that resolve to the same column
name contribute successive lines of that column's description, concatenated in
file order and joined by `LF`. This is the column-level analogue of §7.6.

**Type resolution across multiple lines.** If more than one `#:` line for the
same column declares a type, the **first** declared type (in file order) wins;
later type declarations for that column are ignored.

**Position independence.** Column comments MAY appear before the header, between
the header and the data, or interleaved among data rows; binding is by name, not
by position.

```
# users
id,name,email,status
#: id      (int)    unique user id, never reused
#: email   (email)  primary contact; may be blank for SSO accounts
#: email            bounces are retried for 72h,
#: email            then the address is marked unverified
#: status  (enum)   one of: active, suspended, deleted
1,Alice,alice@example.com,active
```

Here `email` acquires the type `email` and a two-line description.

---

## 8. Column types

Types are **optional, advisory annotations** declared in column comments (§7.7).
The format does **not** enforce types, and a value that contradicts its column's
declared type is not an MTCSV error.

### 8.1 Declared types

A declared type is any string inside the `(` … `)` group of a column comment.
The format places no constraints on the vocabulary; applications choose their
own. The following names are **RECOMMENDED** for interoperability:

| Type | Meaning |
|------|---------|
| `string` | Text. The default when nothing is declared or inferred. |
| `int` | Integer. |
| `number` | Real number (also acceptable: `decimal`, `float`). |
| `bool` | Boolean (`true`/`false`, or an application's convention). |
| `date` | Calendar date, RECOMMENDED `YYYY-MM-DD`. |
| `datetime` | Date and time. |
| `enum` | One of a fixed set; the set MAY be described in prose. |

Applications MAY use any other type name (e.g. `email`, `uuid`, `currency`,
`json`). Readers that do not recognize a type name MUST treat the column as
untyped for validation purposes but SHOULD preserve and surface the declared
string.

### 8.2 Inferred types *(informative)*

When a column has no declared type, a reader MAY infer one from the column's data
for display purposes. A reasonable, spreadsheet-style heuristic over a column's
non-empty cells:

1. all cells match `^[+-]?\d+$` → `int`;
2. else all match a decimal/scientific numeric pattern → `number`;
3. else all match `true|false|yes|no` (case-insensitive) → `bool`;
4. else all match `YYYY-MM-DD[ T]HH:MM[:SS]` → `date`/`datetime`;
5. else → `string`;
6. a column with no non-empty cells → `empty`/unknown.

Inference is never authoritative and MUST NOT override a declared type.

---

## 9. Table identity and addressing

- A section that carries a marker (§7.2) defines a **named** table. A section
  without a marker defines an **anonymous** table.
- **Anonymous tables** are identified by their zero-based **position** among all
  tables in the file (the first table is `0`, the next `1`, and so on).
- **Named tables.** Two or more sections MAY share the same name. Such sections
  are parts of one logical table. A reader **MAY** concatenate their data rows in
  file order ("append semantics"), which lets a large table be split across the
  file or appended to later. If a reader concatenates same-named sections:
  - their headers SHOULD be identical; behavior on differing headers is
    implementation-defined (a reader MAY realign by name, MAY use the first
    header, or MAY emit a diagnostic);
  - the merged table's metadata is the union of the sections' metadata; on key
    conflict the first occurrence SHOULD win.
- A reader that does **not** implement append semantics MUST still parse each
  section successfully; it simply surfaces same-named sections as distinct
  tables. Writers that require append semantics SHOULD document that requirement,
  since it is OPTIONAL for readers.

Positional indices are assigned over **sections in file order**, counting both
named and anonymous tables, regardless of whether a reader later merges
same-named sections.

---

## 10. Reference parsing algorithm

The following pseudocode is a normative reference for the parsing model. Concrete
implementations need not follow its structure so long as they produce the same
classification and record boundaries.

```text
function parse(text):
    lines := split text on /\r\n | \r | \n/      # physical lines
    tables := []
    current := null                              # section under construction
    i := 0

    while i < len(lines):
        line := lines[i]

        # 1. Blank line → end the current section.
        if line is empty or all-whitespace:
            close(current); current := null; i += 1; continue

        # 2. A '#'-line (we are between records, so no quote is open).
        if line matches /^[ \t]*#/:
            current := current or newSection(startLine = i)
            rest := substring of line after first '#'
            if rest starts with ':':
                current.columnComments += parseColumnComment(line, i)
            else if rest starts with '!':
                current.description += trim(text after "#!")   # joined by LF
            else if rest matches /^\s+\S/ and current has no marker and no header:
                parseMarker(rest, current)                     # name + metadata
            # else: free comment → ignore
            i += 1; continue

        # 3. Otherwise, the start of a record (may span physical lines).
        current := current or newSection(startLine = i)
        (fields, endLine, unterminated) := readRecord(lines, i)
        if current has no header:
            current.header  := fields
            current.columns := names(fields)
        else:
            current.dataRows += fields
        if unterminated: emit diagnostic "unterminated-quote"
        i := endLine + 1

    close(current)
    return tables

# Read one CSV record starting at physical line `start`. Tracks quote state
# across physical lines; a newline while inQuotes is field data.
function readRecord(lines, start):
    fields := []; buf := ""; inQuotes := false; line := start; col := 0
    loop:
        s := lines[line]
        while col <= len(s):
            if col == len(s):                    # end of physical line
                if inQuotes:                     # newline is inside the field
                    buf += "\n"; line += 1; col := 0
                    if line >= len(lines): return (fields+[unquote(buf)], line-1, true)
                    break                        # continue with next physical line
                else:
                    fields += [unquote(buf)]; return (fields, line, false)
            c := s[col]
            if inQuotes:
                if c == '"' and s[col+1] == '"': buf += '"'; col += 2; continue
                if c == '"':                      inQuotes := false; col += 1; continue
                buf += c; col += 1
            else:
                if c == '"':  inQuotes := true;  col += 1
                elif c == ',': fields += [unquote(buf)]; buf := ""; col += 1
                else:          buf += c; col += 1

# After all sections are parsed, for each table:
#   - bind column comments to columns by name (accumulate descriptions,
#     first declared type wins);
#   - emit diagnostics for unknown-column, duplicate-column, too-many-fields.
```

Notes:

- `close(current)` finalizes the section: it binds column comments, runs
  post-checks, and appends the resulting table to `tables`.
- The `unquote` shown here is applied per field; an implementation that needs
  precise source spans (for editor tooling) should track field start/end offsets
  instead of accumulating into `buf`.

---

## 11. Writing MTCSV

A writer emits one section per table. This section is normative for writers.

### 11.1 Section layout

For each table, in output order, emit:

1. an OPTIONAL marker line `# name [key=value ...]`;
2. OPTIONAL `#!` description line(s);
3. the header record;
4. OPTIONAL `#:` column-comment line(s) (RECOMMENDED directly under the header);
5. the data rows.

Separate consecutive sections with **exactly one** BLANK line. A trailing line
break at end of file is RECOMMENDED. Emitting more than one blank line between
sections is permitted (readers coalesce them) but not RECOMMENDED.

### 11.2 Field quoting

A writer MUST quote a field (wrap in `"` and double any interior `"`) when the
cell value contains any of: a comma, a double quote, `CR`, or `LF`.

A writer MUST additionally quote the **first field of a record** when its value
begins with `#`, or with whitespace, because such a line would otherwise be
classified as a comment (`#…`) or, if all-whitespace, as a blank separator. (See
§5.5 and §6.)

A writer SHOULD quote any field that begins or ends with whitespace, to preserve
that whitespace unambiguously.

A writer MUST NOT quote fields unnecessarily beyond the above if byte-for-byte
minimality matters to it; however, over-quoting is always safe and readers treat
`"abc"` and `abc` identically.

### 11.3 Names, metadata, descriptions, comments

- If a table name contains whitespace or `=`, the writer SHOULD quote it in the
  marker and MUST accept that some minimal readers will not honor the quotes
  (§7.2 implementation note).
- Metadata values containing whitespace SHOULD be quoted.
- Description (`#!`) and column-comment (`#:`) text is free text and needs no
  escaping; a writer MUST NOT allow a `LF` into a single such line (split it into
  multiple `#!`/`#:` lines instead).
- A column comment's name, if it contains whitespace, MUST be quoted so the
  parser can delimit it.

### 11.4 Determinism *(informative)*

For diff-friendly output, writers SHOULD emit tables and columns in a stable
order and SHOULD be consistent about quoting, so that regenerating a file from
unchanged data produces an unchanged file.

---

## 12. Diagnostics

Readers, linters, and editors SHOULD detect the following conditions. Severities
are RECOMMENDED, not mandatory. None of these are fatal to parsing except where
noted.

| Code | Severity | Condition |
|------|----------|-----------|
| `too-many-fields` | error | A data row has more fields than the header defines. |
| `unterminated-quote` | error | A quoted field is still open at end of file. |
| `unknown-column` | warning | A `#:` comment names a column not present in the header. |
| `duplicate-column` | warning | Two or more header columns share a name. |
| `no-header` | warning | A section contains structural lines but no record. |
| `ragged-short-row` | hint (opt-in) | A data row has fewer fields than the header (legal; padded). |

Editors MAY surface additional affordances (column typing, hovers, navigation),
but those are tooling concerns, not part of this specification.

---

## 13. Grammar (ABNF)

The following ABNF ([RFC 5234](https://www.rfc-editor.org/rfc/rfc5234)) describes
the **context-free** structure of MTCSV: character classes, fields, records, and
the shapes of the `#`-line categories. Two rules are **context-sensitive** and
cannot be captured by ABNF alone; they are stated normatively in §6 and repeated
here as comments:

- a line break inside a `quoted` field is field data, not a record terminator;
- a `marker` is recognized only as the first candidate line of a section and only
  before the header.

```abnf
; ------------------------- terminals -------------------------
CR         = %x0D
LF         = %x0A
SP         = %x20
HTAB       = %x09
WSP        = SP / HTAB
DQUOTE     = %x22            ; "
COMMA      = %x2C            ; ,
HASH       = %x23            ; #
newline    = CR LF / LF / CR

; Any UTF-8 scalar value. UTF8-char is defined by RFC 3629.
CHAR       = UTF8-char

; TEXTDATA excludes the CSV-structural bytes.
TEXTDATA   = %x20-21 / %x23-2B / %x2D-7E / non-ascii   ; not " , CR LF
non-ascii  = %x80-10FFFF                               ; (as UTF-8)

; ------------------------- fields & records -------------------------
field      = quoted / unquoted
unquoted   = *TEXTDATA
quoted     = DQUOTE *( qtext / COMMA / CR / LF / dqesc ) DQUOTE
qtext      = %x20-21 / %x23-7E / non-ascii             ; any char except "
dqesc      = 2DQUOTE                                    ; "" → literal "
record     = field *( COMMA field )

; ------------------------- line categories -------------------------
; A physical line reached with no quote open is exactly one of:
line       = blank / colcomment / description / marker / freecomment / record

blank      = *WSP newline

marker     = *WSP HASH 1*WSP name *( 1*WSP metapair ) *WSP newline
name       = quoted / token
metapair   = key "=" value
key        = 1*( %x21-3C / %x3E-7E / non-ascii )        ; non-WSP, excludes '='
value      = quoted / *( %x21-7E / non-ascii )          ; non-WSP run, or quoted

description = *WSP HASH "!" *( WSP / TEXTDATA / DQUOTE / COMMA ) newline

colcomment = *WSP HASH ":" *WSP colname
             [ 1*WSP "(" *typechar ")" ]
             [ 1*WSP desctext ] newline
colname    = quoted / token
typechar   = %x20-28 / %x2A-7E / non-ascii              ; any char except ')'
desctext   = *( WSP / TEXTDATA / DQUOTE / COMMA )

freecomment = *WSP HASH *( WSP / TEXTDATA / DQUOTE / COMMA ) newline

token      = 1*( %x21-7E / non-ascii )                  ; a run of non-WSP chars

; ------------------------- file -------------------------
; A section is a run of non-blank lines; blank lines separate sections.
; (Record grouping across physical lines is governed by §6, not by ABNF.)
section    = *( marker / description / colcomment / freecomment / record )
file       = *blank [ section *( 1*blank section ) ] *blank
```

> The `record` production in `section` stands for a logical record, which MAY
> occupy several physical lines when it contains a `quoted` field with embedded
> `newline`s. ABNF cannot express that a `newline` inside `quoted` is not a line
> terminator; see §5.3 and §6.

---

## 14. Conformance classes

An implementation SHOULD document which class it targets.

### 14.1 CSV-compatible consumption *(informative)*

Any RFC 4180 reader can open an MTCSV file. It sees:

- structural lines (`#…`) as single-field rows whose first cell begins with `#`;
- blank lines as empty records or record separators (per that reader's rules);
- data rows as ordinary rows.

This yields a single ragged table. No MTCSV awareness is required to recover the
raw cell data - the basis for MTCSV's "losslessly degradable" goal.

### 14.2 Minimal reader

A **minimal MTCSV reader** MUST:

- split the file into sections on BLANK lines, honoring quote state (§4, §6);
- identify the marker and read the table name (§7.2), or assign a positional
  index when absent (§9);
- treat the first record as the header and the rest as data rows (§7.3–§7.4);
- pad short rows (§7.4);
- ignore `#!`, `#:`, and free-comment lines.

A minimal reader MAY ignore metadata, descriptions, column comments, and types.

### 14.3 Full reader

A **full MTCSV reader** MUST do everything a minimal reader does, and MUST also:

- parse marker metadata (§7.2);
- collect table descriptions (§7.6);
- parse and bind column comments, including multi-line descriptions and declared
  types (§7.7);
- produce the diagnostics of §12 (or a documented subset).

A full reader MAY implement same-name append semantics (§9) and type inference
(§8.2); both are OPTIONAL.

### 14.4 Writer

A **conformant writer** MUST emit files that a minimal reader parses back into
the intended tables, MUST follow the quoting rules of §11.2, and MUST separate
sections with a blank line.

---

## 15. Worked examples

### 15.1 A minimal file

```mtcsv
# users
id,name,email
1,Alice,alice@example.com
2,Bob,bob@example.com

# tags
order_id,tag
10,priority
```

Two named tables. A generic CSV reader sees a five-row ragged table; a minimal
MTCSV reader sees `users` (2 rows) and `tags` (1 row).

### 15.2 Metadata, description, column comments, and types

```mtcsv
# users currency=AUD schema=v2
#! Registered accounts. Deactivated users are kept, not deleted.
id,name,email,status,created
#: id       (int)    unique user id, never reused
#: email    (email)  primary contact; may be blank for SSO accounts
#: email             bounces are retried for 72h,
#: email             then the address is marked unverified
#: status   (enum)   one of: active, suspended, deleted
1,Alice,alice@example.com,active,2024-01-05
2,Bob,"bob, jr.@example.com",suspended,2024-03-11
```

- `users` carries metadata `currency=AUD`, `schema=v2`.
- The table description is one line.
- `id` is typed `int`; `status` is `enum`; `created` has no declared type (a
  reader MAY infer `date`).
- `email` has a declared type and a two-line description (two `#:` lines).
- Bob's email is quoted because it contains a comma.

### 15.3 Quoted fields, embedded newlines, and the `#` first-cell rule

```mtcsv
# notes
id,body
1,"a value with, a comma"
2,"a value
that spans two lines"
3,"#not-a-comment"
```

Row 2's `body` contains a real newline; the record spans two physical lines but
is one row. Row 3's first cell begins with `#`, so it MUST be quoted - otherwise
the line would be read as a free comment.

### 15.4 Anonymous tables and positional addressing

```mtcsv
a,b
1,2

x,y,z
3,4,5
```

Neither section has a marker. They are tables `0` (columns `a,b`) and `1`
(columns `x,y,z`).

### 15.5 Same-name append (OPTIONAL reader behavior)

```mtcsv
# events
id,kind
1,login

# events
id,kind
2,logout
```

A reader with append semantics yields one `events` table with two rows. A reader
without it yields two `events` sections. Both are conformant.

---

## 16. Security considerations

- **Untrusted input.** Treat MTCSV as untrusted text. Metadata, descriptions, and
  types are advisory strings; never execute or `eval` them.
- **Resource limits.** A quoted field may contain arbitrary bytes, including many
  newlines; a single record may therefore span the whole file. Readers SHOULD
  bound memory and reject pathologically large fields/records rather than buffer
  unboundedly.
- **Line-count assumptions.** Because quoted fields may contain newlines, tools
  MUST NOT assume "one physical line = one record." Security logic that samples or
  truncates by physical line can split a record mid-field.
- **Spreadsheet formula injection.** As with any CSV, a cell beginning with `=`,
  `+`, `-`, or `@` may be interpreted as a formula when opened in a spreadsheet
  application. Consumers that re-export to spreadsheets SHOULD apply the usual CSV
  injection mitigations; this is orthogonal to MTCSV.
- **The `#` sigil.** The requirement to quote a first cell beginning with `#`
  (§11.2) is a correctness rule, not merely cosmetic: failing to quote it causes
  a data row to vanish (reclassified as a comment). Writers MUST enforce it.
- **BOM and encoding.** Reject or normalize non-UTF-8 input rather than guessing
  encodings, to avoid mojibake-driven misclassification of the `#` sigil.

---

## 17. Versioning and extensibility

This document specifies **MTCSV 1.0**.

- **Metadata-carried version.** Applications that need to record a schema version
  SHOULD do so in table metadata (e.g. `schema=v2`), which is application-defined
  and ignored by generic readers.
- **Reserved sigils.** Within a section, the two-character sequences `#:` and
  `#!` are reserved by this specification. Future versions MAY assign meaning to
  additional `#`-punctuation sequences (e.g. `#?`, `#@`, `#=`). To remain
  forward-compatible, writers SHOULD begin free comments with whitespace or a
  letter/digit after `#` (e.g. `# note`), and SHOULD NOT rely on a free comment
  whose first post-`#` character is punctuation retaining "comment" semantics in
  future versions.
- **Compatibility invariant.** Any future version MUST preserve the two
  foundational properties: (a) every MTCSV file is valid RFC 4180 CSV, and (b)
  blank lines separate tables. Extensions that break either property are not
  MTCSV.
- **Unknown constructs.** Readers MUST ignore metadata keys, type names, and
  `#`-comment content they do not understand, rather than failing - this is what
  makes forward compatibility possible.

---

## 18. Design rationale and FAQ

*(Informative.)*

**Why `#` for structure?**
Most CSV readers already pass `#`-prefixed lines through as ordinary one-cell
rows, and humans read `#` as "comment/heading." Overloading one familiar sigil
keeps the format learnable and keeps generic tooling working.

**Why blank lines to separate tables, rather than an explicit end marker?**
Blank lines are the most natural visual separator and are already tolerated by
CSV readers as empty records. An explicit `#end` marker would add syntax without
adding clarity, and would be one more thing to forget.

**Why bind column comments by name instead of by position?**
Name binding survives column reordering and lets a comment sit anywhere in the
section. It also turns typos into detectable `unknown-column` diagnostics. The
cost - you cannot document two same-named columns differently - is acceptable,
because duplicate column names are themselves discouraged.

**Why put types in column comments instead of the header (`id:int`)?**
An earlier draft typed the header (`id:int,total:decimal`). Moving types into
`#:` comments keeps the header a clean, standard CSV row (so generic tools see
real column names, not `id:int`), and co-locates the type with the column's prose
documentation. Types are advisory, so hiding them from the data row costs
nothing.

**Why are types not enforced?**
Enforcement would make MTCSV a schema language and break the "dumb, CSV-shaped"
goal. Types are documentation and hints for tooling (display, inference,
editors), not constraints.

**Why can't a single-column table have an empty-string row?**
Such a row is a blank physical line, which is the table separator. This is the
one place the format's simplicity has a sharp edge; quote the cell (`""`) to
represent it. In practice, single-column tables with empty rows are rare.

**Is MTCSV a subset or superset of CSV?**
Superset in intent, subset in bytes: every MTCSV file *is* a CSV file, but MTCSV
assigns extra meaning to two things (blank lines, `#`-lines) that plain CSV
leaves unstructured.

**Can I stream-parse it?**
Yes. Parsing is single-pass and local: you can emit each table as its section
closes. Only same-name append semantics (§9) require buffering across sections,
and that behavior is optional.

---

## Appendix A: Quick reference

```
BLANK LINE            → separates tables (not when inside a quoted field)

# name key=value      → table marker: name + optional metadata
                        (only the first such line, only before the header)
#! text               → table description (multiple lines join with newline)
header,row,here       → first record in a section = column names
#: col (type) text    → column comment: doc + optional (type), binds by name
                        (multiple lines for one column join with newline)
# anything else       → free comment (ignored)
data,rows,follow      → records until the next blank line

Quoting (RFC 4180):   " … "  with  ""  for a literal quote; may span lines.
MUST quote a first cell beginning with '#'.
Short rows pad with empty cells; long rows are an error.
Types are advisory. Metadata is advisory. Names bind by exact match.
```

Sigil classification, by the first character(s) after `#`:

| Starts with | Category |
|-------------|----------|
| `:` | column comment |
| `!` | table description |
| whitespace + text, first line, before header | table marker |
| anything else | free comment |

---

## Appendix B: Relationship to reference tooling

*(Informative.)* Two reference implementations exist and may be useful when
building your own:

- **An editor extension** (VS Code) providing syntax highlighting, per-column
  "rainbow" colouring, non-destructive visual column alignment, collapsible
  columns, sticky table/header rows, hovers that show a column's type and
  comment, an outline of tables and columns, and the diagnostics of §12.
- **A converter** (Deno) that turns a folder of JSON files into one MTCSV file,
  one table per file, illustrating the writer rules of §11 (including quoting a
  first cell beginning with `#` and CSV-escaping embedded commas/quotes/newlines).

These tools are not part of the specification and impose no additional
requirements. Where a reference tool's behavior differs from this document (for
example, a minimal marker parser that does not honor quoted names, §7.2), **this
document governs** for the purpose of determining conformance.

---

*End of specification - MTCSV 1.0.*
