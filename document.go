package mtcsv

import (
	"bytes"
	"io"
	"strconv"
	"strings"
)

// A Document is a parsed MTCSV file: an ordered list of tables plus any
// diagnostics produced while reading them.
type Document struct {
	Tables      []*Table
	Diagnostics []Diagnostic
}

// A Table is one section of a document. A table is either named by a marker
// line or anonymous, in which case it's addressed by Index.
type Table struct {
	// Name is the table's name, from its marker line. It is empty for an
	// anonymous table.
	Name string
	// Anonymous reports whether the section carried no marker.
	Anonymous bool
	// Index is the table's zero-based position among all sections in file
	// order, counting named and anonymous tables alike.
	Index int
	// Description is the joined text of the table's '#!' lines.
	Description string
	// Meta holds the marker's key=value pairs, in file order. It is advisory;
	// the format assigns no meaning to any key.
	Meta Metadata
	// Columns are the header's fields, in order.
	Columns []Column
	// Rows are the data rows. Short rows have been padded to len(Columns);
	// surplus cells of an over-long row are kept.
	Rows [][]string
	// Line is the 1-based physical line on which the section starts.
	Line int
}

// A Column is one position in a table, named by the header and optionally
// documented and typed by '#:' lines.
type Column struct {
	Name  string
	Index int
	// Type is the declared type from a column comment, e.g. "int". It is
	// advisory and may be any string; it is empty when undeclared.
	Type string
	// Description is the joined text of the column's '#:' lines.
	Description string
}

// A MetaEntry is one key=value pair from a table marker.
type MetaEntry struct {
	Key   string
	Value string
}

// Metadata is a table's marker metadata, kept in file order. Duplicate keys
// are preserved; lookups return the first occurrence.
type Metadata []MetaEntry

// Get returns the value of the first entry with the given key, or "".
func (m Metadata) Get(key string) string {
	for _, e := range m {
		if e.Key == key {
			return e.Value
		}
	}
	return ""
}

// Has reports whether the key is present.
func (m Metadata) Has(key string) bool {
	for _, e := range m {
		if e.Key == key {
			return true
		}
	}
	return false
}

// Set replaces the first entry with the given key, or appends a new one.
func (m *Metadata) Set(key, value string) {
	for i := range *m {
		if (*m)[i].Key == key {
			(*m)[i].Value = value
			return
		}
	}
	*m = append(*m, MetaEntry{key, value})
}

// Map returns the metadata as a map. On duplicate keys the first wins.
func (m Metadata) Map() map[string]string {
	out := make(map[string]string, len(m))
	for _, e := range m {
		if _, ok := out[e.Key]; !ok {
			out[e.Key] = e.Value
		}
	}
	return out
}

// String renders the metadata in marker syntax, quoting values where needed.
//
// Keys are written verbatim: a reader scans a key as the raw run of characters
// before the first '=', so it would read any quotes as part of the key. An
// entry whose key is empty or contains whitespace or '=' has no representation
// in the format and is skipped — no reader can produce such a key anyway.
func (m Metadata) String() string {
	var b strings.Builder
	for _, e := range m {
		if !representableKey(e.Key) {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(e.Key)
		b.WriteByte('=')
		b.WriteString(quoteToken(e.Value))
	}
	return b.String()
}

func representableKey(key string) bool {
	return key != "" && !strings.ContainsAny(key, " \t=")
}

// ParseMetadata parses metadata written in marker syntax, e.g.
// `currency=AUD source="orders export.json"`. Tokens without '=' and tokens
// with an empty key are ignored.
func ParseMetadata(s string) Metadata {
	_, meta := parseMarker(" x " + s)
	return meta
}

// ID returns the table's addressable identity: its name, or its decimal
// position if it's anonymous.
func (t *Table) ID() string {
	if t.Anonymous || t.Name == "" {
		return strconv.Itoa(t.Index)
	}
	return t.Name
}

// ColumnNames returns the header, in order.
func (t *Table) ColumnNames() []string {
	names := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		names[i] = c.Name
	}
	return names
}

// ColumnIndex returns the index of the first column with the given name, or -1.
func (t *Table) ColumnIndex(name string) int {
	for i, c := range t.Columns {
		if c.Name == name {
			return i
		}
	}
	return -1
}

// Column returns the first column with the given name, or nil.
func (t *Table) Column(name string) *Column {
	if i := t.ColumnIndex(name); i >= 0 {
		return &t.Columns[i]
	}
	return nil
}

// Get returns the cell at the given row index in the named column, or "" if
// either is out of range.
func (t *Table) Get(row int, column string) string {
	if row < 0 || row >= len(t.Rows) {
		return ""
	}
	col := t.ColumnIndex(column)
	if col < 0 || col >= len(t.Rows[row]) {
		return ""
	}
	return t.Rows[row][col]
}

// Records returns the data rows as maps keyed by column name. Where a name
// repeats, the last column with that name wins.
func (t *Table) Records() []map[string]string {
	out := make([]map[string]string, len(t.Rows))
	for i, row := range t.Rows {
		m := make(map[string]string, len(t.Columns))
		for j, c := range t.Columns {
			if j < len(row) {
				m[c.Name] = row[j]
			} else {
				m[c.Name] = ""
			}
		}
		out[i] = m
	}
	return out
}

// AppendRow appends a data row, padding it to the column count.
func (t *Table) AppendRow(cells ...string) {
	for len(cells) < len(t.Columns) {
		cells = append(cells, "")
	}
	t.Rows = append(t.Rows, cells)
}

// SetColumns replaces the header with the given names.
func (t *Table) SetColumns(names ...string) {
	t.Columns = t.Columns[:0]
	for i, n := range names {
		t.Columns = append(t.Columns, Column{Name: n, Index: i})
	}
}

// String renders the table as a single MTCSV section.
func (t *Table) String() string {
	var buf bytes.Buffer
	writeTableTo(&buf, t, true)
	return buf.String()
}

// Table returns the first table with the given name, or nil. A table may also
// be addressed by its decimal position, e.g. Table("2") finds the third table
// when it is anonymous.
func (d *Document) Table(name string) *Table {
	for _, t := range d.Tables {
		if !t.Anonymous && t.Name == name {
			return t
		}
	}
	if i, err := strconv.Atoi(name); err == nil {
		return d.At(i)
	}
	return nil
}

// TablesNamed returns every table with the given name, in file order. Several
// sections may share a name; see Document.Merge.
func (d *Document) TablesNamed(name string) []*Table {
	var out []*Table
	for _, t := range d.Tables {
		if !t.Anonymous && t.Name == name {
			out = append(out, t)
		}
	}
	return out
}

// At returns the table at the given position, or nil if out of range.
func (d *Document) At(i int) *Table {
	if i < 0 || i >= len(d.Tables) {
		return nil
	}
	return d.Tables[i]
}

// Err reports the document's error-severity diagnostics, if any, as a
// DiagnosticError. It returns nil when the document parsed cleanly. Warnings
// and hints never produce an error.
func (d *Document) Err() error {
	var bad []Diagnostic
	for _, diag := range d.Diagnostics {
		if diag.Severity == SeverityError {
			bad = append(bad, diag)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	return &DiagnosticError{Diagnostics: bad}
}

// Merge concatenates same-named sections, in file order, into one table (the
// spec's optional append semantics). The first section's header, description
// and metadata win; later sections' rows are realigned by column name, and
// columns they introduce are appended to the header. Anonymous tables are left
// alone. Merge returns a new Document and doesn't touch the receiver.
func (d *Document) Merge() *Document {
	out := &Document{Diagnostics: d.Diagnostics}
	byName := make(map[string]*Table)
	for _, t := range d.Tables {
		if t.Anonymous || t.Name == "" {
			out.Tables = append(out.Tables, t.clone())
			continue
		}
		dst, ok := byName[t.Name]
		if !ok {
			dst = t.clone()
			byName[t.Name] = dst
			out.Tables = append(out.Tables, dst)
			continue
		}
		dst.appendRows(t)
	}
	for i, t := range out.Tables {
		t.Index = i
	}
	return out
}

func (t *Table) clone() *Table {
	c := *t
	c.Meta = append(Metadata(nil), t.Meta...)
	c.Columns = append([]Column(nil), t.Columns...)
	c.Rows = make([][]string, len(t.Rows))
	for i, r := range t.Rows {
		c.Rows[i] = append([]string(nil), r...)
	}
	return &c
}

// appendRows merges src's rows into t, realigning cells by column name.
func (t *Table) appendRows(src *Table) {
	if src.Description != "" {
		if t.Description == "" {
			t.Description = src.Description
		} else {
			t.Description += "\n" + src.Description
		}
	}
	for _, e := range src.Meta {
		if !t.Meta.Has(e.Key) {
			t.Meta = append(t.Meta, e)
		}
	}
	// Map each source column onto a destination column, extending the header
	// with any column the destination does not have yet.
	before := len(t.Columns)
	mapping := make([]int, len(src.Columns))
	used := make([]bool, len(t.Columns))
	for i, sc := range src.Columns {
		mapping[i] = -1
		for j, dc := range t.Columns {
			if used[j] || dc.Name != sc.Name {
				continue
			}
			used[j] = true
			mapping[i] = j
			if t.Columns[j].Type == "" {
				t.Columns[j].Type = sc.Type
			}
			break
		}
		if mapping[i] < 0 {
			mapping[i] = len(t.Columns)
			t.Columns = append(t.Columns, Column{
				Name: sc.Name, Index: len(t.Columns),
				Type: sc.Type, Description: sc.Description,
			})
			used = append(used, true)
		}
	}
	for _, row := range src.Rows {
		dst := make([]string, len(t.Columns))
		for i, cell := range row {
			if i < len(mapping) {
				dst[mapping[i]] = cell
			} else {
				dst = append(dst, cell) // surplus cells of an over-long row
			}
		}
		t.Rows = append(t.Rows, dst)
	}
	// Pad rows added before the header grew. Identical headers, the common
	// case, need no pass at all.
	if len(t.Columns) > before {
		for i, row := range t.Rows {
			for len(row) < len(t.Columns) {
				row = append(row, "")
			}
			t.Rows[i] = row
		}
	}
}

// WriteTo writes the document in MTCSV form, implementing io.WriterTo.
func (d *Document) WriteTo(w io.Writer) (int64, error) {
	cw := &countingWriter{w: w}
	err := writeDocument(cw, d, true)
	return cw.n, err
}

// Bytes returns the document encoded as MTCSV.
func (d *Document) Bytes() []byte {
	var buf bytes.Buffer
	writeDocument(&buf, d, true)
	return buf.Bytes()
}

// String returns the document encoded as MTCSV.
func (d *Document) String() string { return string(d.Bytes()) }

// AddTable appends a table named name built from v, which must be a slice,
// array, or map of rows in the form described in the package documentation.
// An empty name produces an anonymous table.
func (d *Document) AddTable(name string, v any) error {
	tables, err := tablesOf(v, tableTag{name: name, named: name != "", anon: name == ""})
	if err != nil {
		return err
	}
	for _, t := range tables {
		t.Index = len(d.Tables)
		d.Tables = append(d.Tables, t)
	}
	return nil
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
