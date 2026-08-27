// Package mtcsv reads and writes MTCSV ("Multi-Table CSV") as described in
// MTCSV-SPEC.md. It's plain RFC 4180 CSV with two extra rules: a blank line
// separates tables, and a line starting with '#' carries structure (a table
// marker, a table description, or a column comment) or is just commentary. A
// file with no '#' lines and no blank lines is ordinary CSV.
//
// The API follows encoding/json: Marshal turns a Go value into an MTCSV
// document and Unmarshal parses one back.
//
//	type User struct {
//		ID    int    `mtcsv:"id"    mtcsv.type:"int"   mtcsv.doc:"unique user id"`
//		Name  string `mtcsv:"name"`
//		Email string `mtcsv:"email" mtcsv.type:"email"`
//	}
//
//	type File struct {
//		Users []User `mtcsv:"users,schema=v2" mtcsv.doc:"Registered accounts."`
//	}
//
// Marshal(File{Users: []User{{1, "Alice", "a@example.com"}}}) produces
//
//	# users schema=v2
//	#! Registered accounts.
//	id,name,email
//	#: id    (int)   unique user id
//	#: email (email)
//	1,Alice,a@example.com
//
// A slice becomes one table, a struct becomes a document of tables (one per
// field), and a map[string][]T becomes one table per key. Fields of the row
// type become columns, in declaration order.
//
// # Struct tags
//
// The "mtcsv" tag names a column (on a row struct) or a table (on a container
// struct). Options go after the name, comma-separated:
//
//	`mtcsv:"id"`               // column/table named "id"
//	`mtcsv:"-"`                // never encoded or decoded
//	`mtcsv:"id,omitempty"`     // write an empty cell instead of a zero value
//	`mtcsv:"users,schema=v2"`  // table marker metadata (container fields only)
//	`mtcsv:"rows,anon"`        // table written without a marker
//
// Two companion tags document a column or table, and come out as '#:' and '#!'
// lines:
//
//	`mtcsv.type:"int"`           // declared column type (advisory)
//	`mtcsv.doc:"free text"`      // column comment / table description
//	`mtcsv.meta:"a=1 b=\"x y\""` // table metadata in marker syntax
//
// # Types
//
// Cells are text. Marshal and Unmarshal understand strings, booleans, every
// integer and float kind, []byte (verbatim, not base64), time.Time, pointers
// (nil is an empty cell), and any type implementing Marshaler, Unmarshaler,
// encoding.TextMarshaler or encoding.TextUnmarshaler. MTCSV is flat and
// rectangular, so other composite types are rejected rather than silently
// flattened.
//
// # Documents
//
// Parse hands you the document model directly — tables, metadata,
// descriptions, column types and diagnostics — for tooling that needs more
// than struct mapping:
//
//	doc, err := mtcsv.Parse(data)
//	for _, t := range doc.Tables {
//		fmt.Println(t.Name, len(t.Columns), len(t.Rows))
//	}
//
// Parse always returns a document. Malformed input is reported through
// Document.Diagnostics, and the returned error (also available as
// Document.Err) is non-nil only when an error-severity diagnostic was found.
//
// # Notes
//
// A few behaviours worth knowing before you lean on a round trip:
//
//   - Line breaks inside a quoted field are normalized to LF, matching the
//     spec's own reference algorithm.
//   - Free comments aren't preserved when a parsed document is written back.
//     Markers, metadata, table descriptions and column comments are.
//   - A field reached through an embedded pointer to an unexported type can't
//     be set via reflection, so it's skipped on decode.
//   - For untrusted input set ParseOptions.MaxRecordBytes — a quoted field may
//     span the whole file.
//
// DOCS.md is the long-form reference; MTCSV-SPEC.md is the format spec.
package mtcsv

import "bytes"

// Marshal writes v out as MTCSV.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Unmarshal parses the MTCSV document in data into the value v points at. If v
// is nil or not a pointer you get an InvalidUnmarshalError.
func Unmarshal(data []byte, v any) error {
	doc, _ := Parse(data)
	return doc.Decode(v)
}

// Valid reports whether data parses cleanly, i.e. with no error-severity
// diagnostics.
func Valid(data []byte) bool {
	_, err := Parse(data)
	return err == nil
}
