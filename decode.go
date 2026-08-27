package mtcsv

import (
	"io"
	"reflect"
	"strings"
)

// A Decoder reads and decodes MTCSV from an input stream.
//
// The whole stream is read and parsed on first use, because a record may span
// physical lines and a section is only complete at a blank line.
type Decoder struct {
	r    io.Reader
	opts ParseOptions
	doc  *Document
	err  error
	next int // index of the next table for DecodeTable

	dopts decodeOptions
}

type decodeOptions struct {
	unknownColumns bool // error on a column with no field
	unknownTables  bool // error on a table with no field
	strict         bool // error-severity diagnostics become errors
}

// NewDecoder returns a Decoder that reads from r.
func NewDecoder(r io.Reader) *Decoder { return &Decoder{r: r} }

// SetParseOptions sets the reader options used when the stream is parsed. It
// must be called before the first Decode.
func (d *Decoder) SetParseOptions(opts ParseOptions) { d.opts = opts }

// DisallowUnknownColumns makes the Decoder return an UnknownColumnError when a
// document contains a column with no corresponding struct field.
func (d *Decoder) DisallowUnknownColumns() { d.dopts.unknownColumns = true }

// DisallowUnknownTables makes the Decoder return an UnknownTableError when a
// document contains a table with no corresponding struct field or map entry.
func (d *Decoder) DisallowUnknownTables() { d.dopts.unknownTables = true }

// Strict makes the Decoder fail on error-severity diagnostics, such as a row
// with more fields than the header defines. By default such documents decode
// on a best-effort basis and the diagnostics are available from Diagnostics.
func (d *Decoder) Strict() { d.dopts.strict = true }

// Document parses the stream, if it has not been parsed already, and returns
// the whole document, including tables already consumed by DecodeTable.
func (d *Decoder) Document() (*Document, error) {
	if d.doc == nil && d.err == nil {
		data, err := io.ReadAll(d.r)
		if err != nil {
			d.err = err
			d.doc = &Document{}
		} else {
			d.doc, _ = ParseWith(data, d.opts)
		}
	}
	return d.doc, d.err
}

// Diagnostics returns the diagnostics produced while parsing the stream.
func (d *Decoder) Diagnostics() []Diagnostic {
	doc, err := d.Document()
	if err != nil {
		return nil
	}
	return doc.Diagnostics
}

// More reports whether there is another table to decode.
func (d *Decoder) More() bool {
	doc, err := d.Document()
	return err == nil && d.next < len(doc.Tables)
}

// Decode reads the remaining tables of the stream into v. See the package
// documentation for the mapping from tables to Go values.
func (d *Decoder) Decode(v any) error {
	doc, err := d.Document()
	if err != nil {
		return err
	}
	rest := &Document{Tables: doc.Tables[d.next:], Diagnostics: doc.Diagnostics}
	d.next = len(doc.Tables)
	return decodeDocument(rest, v, d.dopts)
}

// DecodeTable reads the next table of the stream into v, which is normally a
// pointer to a slice of rows. It returns io.EOF when no tables remain.
func (d *Decoder) DecodeTable(v any) error {
	doc, err := d.Document()
	if err != nil {
		return err
	}
	if d.next >= len(doc.Tables) {
		return io.EOF
	}
	t := doc.Tables[d.next]
	d.next++
	if d.dopts.strict {
		if err := (&Document{Tables: []*Table{t}, Diagnostics: diagnosticsFor(doc, t)}).Err(); err != nil {
			return err
		}
	}
	return decodeTables([]*Table{t}, v, d.dopts)
}

// NextTable returns the next table of the stream without decoding it. It
// returns io.EOF when no tables remain.
func (d *Decoder) NextTable() (*Table, error) {
	doc, err := d.Document()
	if err != nil {
		return nil, err
	}
	if d.next >= len(doc.Tables) {
		return nil, io.EOF
	}
	t := doc.Tables[d.next]
	d.next++
	return t, nil
}

func diagnosticsFor(doc *Document, t *Table) []Diagnostic {
	var out []Diagnostic
	for _, diag := range doc.Diagnostics {
		if diag.Table == t.ID() {
			out = append(out, diag)
		}
	}
	return out
}

// Decode stores the document in the value pointed to by v.
func (d *Document) Decode(v any) error { return decodeDocument(d, v, decodeOptions{}) }

// Decode stores the table's rows in the value pointed to by v, normally a
// pointer to a slice of rows.
func (t *Table) Decode(v any) error { return decodeTables([]*Table{t}, v, decodeOptions{}) }

func decodeDocument(doc *Document, v any, opts decodeOptions) error {
	if opts.strict {
		if err := doc.Err(); err != nil {
			return err
		}
	}
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Kind() != reflect.Pointer || rv.IsNil() {
		return &InvalidUnmarshalError{Type: reflect.TypeOf(v)}
	}
	return decodeIntoDocumentTarget(doc, rv.Elem(), opts)
}

func decodeIntoDocumentTarget(doc *Document, rv reflect.Value, opts decodeOptions) error {
	// Allocate through pointers.
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		rv = rv.Elem()
	}
	if rv.Kind() == reflect.Interface && rv.NumMethod() == 0 {
		// Decode into a fresh document for an empty interface target.
		rv.Set(reflect.ValueOf(doc))
		return nil
	}

	switch rv.Type() {
	case reflect.TypeOf(Document{}):
		rv.Set(reflect.ValueOf(*doc))
		return nil
	case reflect.TypeOf([]*Table(nil)):
		rv.Set(reflect.ValueOf(doc.Tables))
		return nil
	}

	switch rv.Kind() {
	case reflect.Struct:
		return decodeContainer(doc, rv, opts)
	case reflect.Map:
		return decodeMapOfTables(doc, rv, opts)
	case reflect.Slice, reflect.Array:
		// A bare slice target takes the first table, plus any later table
		// sharing its name.
		if len(doc.Tables) == 0 {
			if rv.Kind() == reflect.Slice {
				rv.SetZero()
			}
			return nil
		}
		first := doc.Tables[0]
		tables := []*Table{first}
		if !first.Anonymous && first.Name != "" {
			tables = doc.TablesNamed(first.Name)
		}
		return decodeTablesInto(tables, rv, opts)
	}
	return &UnsupportedTypeError{
		Type: rv.Type(),
		Msg:  "a document decodes into a struct, map or slice",
	}
}

// decodeContainer matches tables to the fields of a container struct: by name
// first (exactly, then case-insensitively), then remaining tables to remaining
// fields in declaration order.
func decodeContainer(doc *Document, rv reflect.Value, opts decodeOptions) error {
	fields := cachedTableFields(rv.Type())
	assigned := make([][]*Table, len(fields))
	taken := make([]bool, len(doc.Tables))
	done := make([]bool, len(fields))

	match := func(fold bool) {
		for fi, tf := range fields {
			if done[fi] || tf.anon {
				continue
			}
			for i, t := range doc.Tables {
				if taken[i] || t.Anonymous || t.Name == "" {
					continue
				}
				if t.Name == tf.name || (fold && strings.EqualFold(t.Name, tf.name)) {
					taken[i] = true
					assigned[fi] = append(assigned[fi], t)
					done[fi] = true
				}
			}
		}
	}
	match(false)
	match(true)

	// Anonymous and unmatched tables fill the remaining fields in order.
	ti := 0
	for fi := range fields {
		if done[fi] {
			continue
		}
		for ti < len(doc.Tables) && taken[ti] {
			ti++
		}
		if ti >= len(doc.Tables) {
			break
		}
		taken[ti] = true
		assigned[fi] = []*Table{doc.Tables[ti]}
		done[fi] = true
	}

	if opts.unknownTables {
		for i, t := range doc.Tables {
			if !taken[i] {
				return &UnknownTableError{Table: t.ID(), Type: rv.Type()}
			}
		}
	}

	for fi, tables := range assigned {
		if len(tables) == 0 {
			continue
		}
		fv := fieldByIndex(rv, fields[fi].index, true)
		if !fv.IsValid() || !fv.CanSet() {
			continue
		}
		if err := decodeTablesInto(tables, fv, opts); err != nil {
			return err
		}
	}
	return nil
}

// decodeMapOfTables fills a map keyed by table identity (name, or decimal
// position for an anonymous table). Same-named tables are appended.
func decodeMapOfTables(doc *Document, rv reflect.Value, opts decodeOptions) error {
	mt := rv.Type()
	if mt.Key().Kind() != reflect.String {
		return &UnsupportedTypeError{Type: mt, Msg: "map keys must be strings"}
	}
	if rv.IsNil() {
		rv.Set(reflect.MakeMap(mt))
	}
	grouped := map[string][]*Table{}
	var order []string
	for _, t := range doc.Tables {
		id := t.ID()
		if _, ok := grouped[id]; !ok {
			order = append(order, id)
		}
		grouped[id] = append(grouped[id], t)
	}
	for _, id := range order {
		elem := reflect.New(mt.Elem()).Elem()
		if err := decodeTablesInto(grouped[id], elem, opts); err != nil {
			return err
		}
		rv.SetMapIndex(reflect.ValueOf(id).Convert(mt.Key()), elem)
	}
	return nil
}

func decodeTables(tables []*Table, v any, opts decodeOptions) error {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Kind() != reflect.Pointer || rv.IsNil() {
		return &InvalidUnmarshalError{Type: reflect.TypeOf(v)}
	}
	return decodeTablesInto(tables, rv.Elem(), opts)
}

// decodeTablesInto stores one or more tables (already known to belong
// together) in rv.
func decodeTablesInto(tables []*Table, rv reflect.Value, opts decodeOptions) error {
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			if !rv.CanSet() {
				return &InvalidUnmarshalError{Type: rv.Type()}
			}
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		rv = rv.Elem()
	}

	// Direct table targets.
	switch rv.Type() {
	case reflect.TypeOf(Table{}):
		rv.Set(reflect.ValueOf(*tables[0]))
		return nil
	case reflect.TypeOf([]*Table(nil)):
		rv.Set(reflect.ValueOf(tables))
		return nil
	case reflect.TypeOf(Document{}):
		rv.Set(reflect.ValueOf(Document{Tables: tables}))
		return nil
	}

	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		return decodeRows(tables, rv, opts)
	case reflect.Struct:
		// A single-row table decodes into a struct directly.
		if len(tables[0].Rows) == 0 {
			return nil
		}
		return decodeRow(tables[0], 0, rv, opts)
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return &UnsupportedTypeError{Type: rv.Type(), Msg: "map keys must be strings"}
		}
		if len(tables[0].Rows) == 0 {
			return nil
		}
		return decodeRow(tables[0], 0, rv, opts)
	case reflect.Interface:
		if rv.NumMethod() == 0 {
			records := make([]map[string]string, 0)
			for _, t := range tables {
				records = append(records, t.Records()...)
			}
			rv.Set(reflect.ValueOf(records))
			return nil
		}
	}
	return &UnsupportedTypeError{Type: rv.Type(), Msg: "a table decodes into a slice of rows"}
}

// decodeRows stores every table's rows in the slice or array rv.
func decodeRows(tables []*Table, rv reflect.Value, opts decodeOptions) error {
	elemType := rv.Type().Elem()
	raw := isStringSlice(elemType) // [][]string carries the header as row 0

	// Struct rows share one element type, so the column->field mapping is
	// computed once per table instead of per cell.
	structType := elemType
	for structType.Kind() == reflect.Pointer {
		structType = structType.Elem()
	}
	isStruct := structType.Kind() == reflect.Struct && !isCellType(structType)

	n := 0
	for _, t := range tables {
		n += len(t.Rows)
		if raw && len(t.Columns) > 0 {
			n++
		}
	}

	out := rv
	if rv.Kind() == reflect.Slice {
		out = reflect.MakeSlice(rv.Type(), n, n)
	}
	i := 0
	// setRow decodes into the element in place, skipping rows that overflow a
	// fixed-size array, as encoding/json does.
	setRow := func(fn func(reflect.Value) error) error {
		if i >= out.Len() {
			i++
			return nil
		}
		if err := fn(out.Index(i)); err != nil {
			return err
		}
		i++
		return nil
	}

	for _, t := range tables {
		if raw && len(t.Columns) > 0 {
			names := t.ColumnNames()
			if err := setRow(func(ev reflect.Value) error {
				return setStringSlice(ev, names)
			}); err != nil {
				return err
			}
		}
		var mapping []*field
		if isStruct {
			mapping = columnFieldMap(t, structType)
		}
		for r := range t.Rows {
			if err := setRow(func(ev reflect.Value) error {
				if isStruct {
					return decodeStructRow(t, r, ev, mapping, opts)
				}
				return decodeRow(t, r, ev, opts)
			}); err != nil {
				return err
			}
		}
	}

	if rv.Kind() == reflect.Slice {
		rv.Set(out)
	} else {
		// Zero the tail of a partially filled array.
		for ; i < rv.Len(); i++ {
			rv.Index(i).SetZero()
		}
	}
	return nil
}

// decodeRow stores one data row in rv.
func decodeRow(t *Table, row int, rv reflect.Value, opts decodeOptions) error {
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		rv = rv.Elem()
	}
	cells := t.Rows[row]

	switch {
	case rv.Kind() == reflect.Struct && !isCellType(rv.Type()):
		return decodeRowStruct(t, row, rv, opts)
	case rv.Kind() == reflect.Map && rv.Type().Key().Kind() == reflect.String:
		return decodeRowMap(t, row, cells, rv)
	case isStringSlice(rv.Type()):
		return setStringSlice(rv, cells)
	case rv.Kind() == reflect.Interface && rv.NumMethod() == 0:
		m := map[string]string{}
		for i, c := range t.Columns {
			if i < len(cells) {
				m[c.Name] = cells[i]
			} else {
				m[c.Name] = ""
			}
		}
		rv.Set(reflect.ValueOf(m))
		return nil
	default:
		// A single-column table decodes into a slice of cell values.
		cell := ""
		if len(cells) > 0 {
			cell = cells[0]
		}
		if err := decodeCell(cell, rv); err != nil {
			return cellError(t, row, columnName(t, 0), cell, rv.Type(), err)
		}
		return nil
	}
}

func decodeRowStruct(t *Table, row int, rv reflect.Value, opts decodeOptions) error {
	return decodeStructRow(t, row, rv, columnFieldMap(t, rv.Type()), opts)
}

// columnFieldMap resolves a table's columns to struct fields, so it's done
// once per table rather than per cell. The result is parallel to t.Columns; a
// nil entry means the column has no field.
func columnFieldMap(t *Table, typ reflect.Type) []*field {
	fields := cachedFields(typ)
	mapping := make([]*field, len(t.Columns))
	for i, col := range t.Columns {
		mapping[i] = fields.lookup(col.Name)
	}
	return mapping
}

// decodeStructRow is decodeRowStruct with a precomputed column->field mapping.
// rv may be a pointer to the row struct.
func decodeStructRow(t *Table, row int, rv reflect.Value, mapping []*field, opts decodeOptions) error {
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		rv = rv.Elem()
	}
	cells := t.Rows[row]
	for i, col := range t.Columns {
		f := mapping[i]
		if f == nil {
			if opts.unknownColumns {
				return &UnknownColumnError{Table: t.ID(), Column: col.Name, Type: rv.Type()}
			}
			continue
		}
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		fv := fieldByIndex(rv, f.index, true)
		if !fv.IsValid() || !fv.CanSet() {
			continue
		}
		if err := decodeCell(cell, fv); err != nil {
			return cellError(t, row, col.Name, cell, fv.Type(), err)
		}
	}
	return nil
}

func decodeRowMap(t *Table, row int, cells []string, rv reflect.Value) error {
	mt := rv.Type()
	if rv.IsNil() {
		rv.Set(reflect.MakeMap(mt))
	}
	keyType := mt.Key()
	// Reuse one scratch element across the row; SetMapIndex copies it.
	ev := reflect.New(mt.Elem()).Elem()
	for i, col := range t.Columns {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		ev.SetZero()
		if err := decodeCell(cell, ev); err != nil {
			return cellError(t, row, col.Name, cell, mt.Elem(), err)
		}
		rv.SetMapIndex(reflect.ValueOf(col.Name).Convert(keyType), ev)
	}
	return nil
}

func cellError(t *Table, row int, column, cell string, typ reflect.Type, err error) error {
	return &UnmarshalTypeError{
		Value: cell, Type: typ, Table: t.ID(), Column: column, Row: row, Err: err,
	}
}

func columnName(t *Table, i int) string {
	if i < len(t.Columns) {
		return t.Columns[i].Name
	}
	return ""
}

func isStringSlice(t reflect.Type) bool {
	return (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) &&
		t.Elem().Kind() == reflect.String && !isCellType(t)
}

func setStringSlice(rv reflect.Value, cells []string) error {
	if rv.Kind() == reflect.Array {
		for i := 0; i < rv.Len(); i++ {
			if i < len(cells) {
				rv.Index(i).SetString(cells[i])
			} else {
				rv.Index(i).SetZero()
			}
		}
		return nil
	}
	out := reflect.MakeSlice(rv.Type(), len(cells), len(cells))
	for i, c := range cells {
		out.Index(i).SetString(c)
	}
	rv.Set(out)
	return nil
}
