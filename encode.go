package mtcsv

import (
	"bufio"
	"fmt"
	"io"
	"reflect"
	"sort"
)

// An Encoder writes MTCSV to an output stream.
type Encoder struct {
	w     io.Writer
	bw    *bufio.Writer
	align bool
	wrote bool
}

// NewEncoder returns an Encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w, align: true}
}

// buffer returns the encoder's single buffered writer, created lazily.
func (e *Encoder) buffer() *bufio.Writer {
	if e.bw == nil {
		e.bw = bufio.NewWriter(e.w)
	}
	return e.bw
}

// SetAlignComments controls whether column comments are padded into aligned
// columns. It is on by default.
func (e *Encoder) SetAlignComments(on bool) { e.align = on }

// Encode writes the MTCSV encoding of v, appending to whatever the Encoder has
// already written. Successive calls are separated by a blank line, so a
// document may be built up table by table.
func (e *Encoder) Encode(v any) error {
	tables, err := tablesOf(v, tableTag{})
	if err != nil {
		return err
	}
	return e.writeTables(tables)
}

// EncodeTable writes v as a single table with the given name. An empty name
// writes an anonymous table.
func (e *Encoder) EncodeTable(name string, v any) error {
	tables, err := tablesOf(v, tableTag{name: name, named: name != "", anon: name == ""})
	if err != nil {
		return err
	}
	return e.writeTables(tables)
}

func (e *Encoder) writeTables(tables []*Table) error {
	bw := e.buffer()
	for _, t := range tables {
		if e.wrote {
			if err := bw.WriteByte('\n'); err != nil {
				return err
			}
		}
		writeTableTo(bw, t, e.align)
		e.wrote = true
	}
	return bw.Flush()
}

// tablesOf builds the tables for a top-level value. A slice or array becomes
// one table, a map becomes one table per key, and a struct is a container
// whose fields are tables.
func tablesOf(v any, tag tableTag) ([]*Table, error) {
	if v == nil {
		return nil, nil
	}
	return tablesOfValue(reflect.ValueOf(v), tag, true)
}

func tablesOfValue(rv reflect.Value, tag tableTag, top bool) ([]*Table, error) {
	rv, ok := indirect(rv)
	if !ok {
		return nil, nil // nil pointer or interface: nothing to write
	}

	switch v := rv.Interface().(type) {
	case Document:
		return v.Tables, nil
	case Table:
		return []*Table{&v}, nil
	}

	switch rv.Kind() {
	case reflect.Struct:
		if top && !isCellType(rv.Type()) {
			return tablesOfContainer(rv)
		}
		// A struct in a container field is a one-row table.
		return tablesOfValue(oneElemSlice(rv), tag, false)

	case reflect.Slice, reflect.Array:
		if isTableSlice(rv.Type()) {
			return tableSlice(rv), nil
		}
		t, err := buildTable(rv, tag)
		if err != nil {
			return nil, err
		}
		return []*Table{t}, nil

	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil, &UnsupportedTypeError{Type: rv.Type(), Msg: "map keys must be strings"}
		}
		var out []*Table
		for _, k := range sortedKeys(rv) {
			sub := tag
			sub.name = k.String()
			sub.anon = false
			tables, err := tablesOfValue(rv.MapIndex(k), sub, false)
			if err != nil {
				return nil, err
			}
			out = append(out, tables...)
		}
		return out, nil
	}

	return nil, &UnsupportedTypeError{
		Type: rv.Type(),
		Msg:  "a table must be a slice, array, map or struct",
	}
}

// tablesOfContainer builds one table per exported field of a container struct.
func tablesOfContainer(rv reflect.Value) ([]*Table, error) {
	var out []*Table
	for _, tf := range cachedTableFields(rv.Type()) {
		fv := fieldByIndex(rv, tf.index, false)
		if !fv.IsValid() {
			continue
		}
		switch fv.Kind() {
		case reflect.Slice, reflect.Array, reflect.Map, reflect.Struct, reflect.Pointer, reflect.Interface:
		default:
			return nil, &UnsupportedTypeError{
				Type: fv.Type(),
				Msg: fmt.Sprintf("field %s of %s is not a table; a struct passed to Marshal "+
					"is a container whose fields are tables", tf.name, rv.Type()),
			}
		}
		tables, err := tablesOfValue(fv, tf.tableTag, false)
		if err != nil {
			return nil, err
		}
		out = append(out, tables...)
	}
	for i, t := range out {
		t.Index = i
	}
	return out, nil
}

// buildTable turns a slice or array of rows into a table.
func buildTable(rv reflect.Value, tag tableTag) (*Table, error) {
	elem := rv.Type().Elem()
	for elem.Kind() == reflect.Pointer {
		elem = elem.Elem()
	}

	t := &Table{Name: tag.name, Anonymous: tag.anon || tag.name == "", Description: tag.doc, Meta: tag.meta}
	applyDescriptor(t, elem, tag)

	switch {
	case elem.Kind() == reflect.Struct && !isCellType(elem):
		return t, buildStructRows(t, rv, elem)
	case elem.Kind() == reflect.Map && elem.Key().Kind() == reflect.String:
		return t, buildMapRows(t, rv)
	case (elem.Kind() == reflect.Slice || elem.Kind() == reflect.Array) && elem.Elem().Kind() == reflect.String:
		return t, buildRawRows(t, rv)
	case elem.Kind() == reflect.Interface && elem.NumMethod() == 0:
		return t, buildAnyRows(t, rv)
	default:
		return t, buildScalarRows(t, rv)
	}
}

// applyDescriptor fills in name, description and metadata from the row type's
// TableDescriptor implementation. Struct tags take precedence.
func applyDescriptor(t *Table, elem reflect.Type, tag tableTag) {
	var info TableInfo
	switch {
	case elem.Implements(tableDescType):
		info = reflect.New(elem).Elem().Interface().(TableDescriptor).MTCSVTable()
	case reflect.PointerTo(elem).Implements(tableDescType):
		info = reflect.New(elem).Interface().(TableDescriptor).MTCSVTable()
	default:
		return
	}
	if !tag.named && info.Name != "" {
		t.Name = info.Name
		if !tag.anon {
			t.Anonymous = info.Anonymous
		}
	}
	if t.Description == "" {
		t.Description = info.Description
	}
	if len(t.Meta) == 0 {
		t.Meta = info.Meta
	}
}

func buildStructRows(t *Table, rv reflect.Value, elem reflect.Type) error {
	fields := cachedFields(elem).list
	t.Columns = make([]Column, len(fields))
	for i, f := range fields {
		t.Columns[i] = Column{Name: f.name, Index: i, Type: f.colType, Description: f.doc}
	}
	for i := 0; i < rv.Len(); i++ {
		row := make([]string, len(fields))
		item, ok := indirect(rv.Index(i))
		if !ok {
			t.Rows = append(t.Rows, row) // nil row pointer: all cells empty
			continue
		}
		for j, f := range fields {
			fv := fieldByIndex(item, f.index, false)
			if !fv.IsValid() {
				continue
			}
			if f.omitEmpty && isEmptyValue(fv) {
				continue
			}
			cell, err := encodeCell(fv, f.colType)
			if err != nil {
				return fmt.Errorf("mtcsv: table %s, column %q, row %d: %w", t.ID(), f.name, i, err)
			}
			row[j] = cell
		}
		t.Rows = append(t.Rows, row)
	}
	return nil
}

func buildMapRows(t *Table, rv reflect.Value) error {
	// The header is the union of all rows' keys, in sorted order.
	seen := map[string]bool{}
	var names []string
	for i := 0; i < rv.Len(); i++ {
		item, ok := indirect(rv.Index(i))
		if !ok {
			continue
		}
		for _, k := range item.MapKeys() {
			if name := k.String(); !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	t.SetColumns(names...)

	// Convert the sorted keys once and reuse them across rows.
	var keyType reflect.Type
	var keyVals []reflect.Value
	for i := 0; i < rv.Len(); i++ {
		if item, ok := indirect(rv.Index(i)); ok {
			keyType = item.Type().Key()
			keyVals = make([]reflect.Value, len(names))
			for j, name := range names {
				keyVals[j] = reflect.ValueOf(name).Convert(keyType)
			}
			break
		}
	}

	for i := 0; i < rv.Len(); i++ {
		row := make([]string, len(names))
		item, ok := indirect(rv.Index(i))
		if ok {
			for j, name := range names {
				key := keyVals[j]
				// A heterogeneous []any can mix key types; convert for the odd
				// one out.
				if item.Type().Key() != keyType {
					key = reflect.ValueOf(name).Convert(item.Type().Key())
				}
				mv := item.MapIndex(key)
				if !mv.IsValid() {
					continue
				}
				cell, err := encodeCell(mv, "")
				if err != nil {
					return fmt.Errorf("mtcsv: table %s, column %q, row %d: %w", t.ID(), name, i, err)
				}
				row[j] = cell
			}
		}
		t.Rows = append(t.Rows, row)
	}
	return nil
}

// buildRawRows encodes [][]string, where the first row is the header.
func buildRawRows(t *Table, rv reflect.Value) error {
	for i := 0; i < rv.Len(); i++ {
		item, ok := indirect(rv.Index(i))
		if !ok {
			item = reflect.Zero(reflect.TypeOf([]string(nil)))
		}
		cells := make([]string, item.Len())
		for j := range cells {
			cells[j] = item.Index(j).String()
		}
		if i == 0 {
			t.SetColumns(cells...)
			continue
		}
		t.AppendRow(cells...)
	}
	return nil
}

// buildScalarRows encodes a slice of cell values as a single-column table.
func buildScalarRows(t *Table, rv reflect.Value) error {
	t.SetColumns("value")
	for i := 0; i < rv.Len(); i++ {
		cell, err := encodeCell(rv.Index(i), "")
		if err != nil {
			return fmt.Errorf("mtcsv: table %s, row %d: %w", t.ID(), i, err)
		}
		t.Rows = append(t.Rows, []string{cell})
	}
	return nil
}

// buildAnyRows encodes []any by inspecting the first element's dynamic type.
func buildAnyRows(t *Table, rv reflect.Value) error {
	if rv.Len() == 0 {
		t.SetColumns("value")
		return nil
	}
	first, ok := indirect(rv.Index(0))
	if !ok {
		return buildScalarRows(t, rv)
	}
	switch {
	case first.Kind() == reflect.Map && first.Type().Key().Kind() == reflect.String:
		return buildMapRows(t, rv)
	case first.Kind() == reflect.Slice && first.Type().Elem().Kind() == reflect.String:
		return buildRawRows(t, rv)
	case first.Kind() == reflect.Struct && !isCellType(first.Type()):
		return buildStructRows(t, rv, first.Type())
	}
	return buildScalarRows(t, rv)
}

// indirect follows pointers and interfaces. It reports false for a nil value.
func indirect(v reflect.Value) (reflect.Value, bool) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Value{}, false
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return v, false
	}
	return v, true
}

func oneElemSlice(v reflect.Value) reflect.Value {
	s := reflect.MakeSlice(reflect.SliceOf(v.Type()), 1, 1)
	s.Index(0).Set(v)
	return s
}

func isTableSlice(t reflect.Type) bool {
	e := t.Elem()
	if e.Kind() == reflect.Pointer {
		e = e.Elem()
	}
	return e == reflect.TypeOf(Table{})
}

func tableSlice(rv reflect.Value) []*Table {
	var out []*Table
	for i := 0; i < rv.Len(); i++ {
		v, ok := indirect(rv.Index(i))
		if !ok {
			continue
		}
		t := v.Interface().(Table)
		out = append(out, &t)
	}
	return out
}
