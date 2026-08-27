package mtcsv

import (
	"reflect"
	"strconv"
	"strings"
)

// An InvalidUnmarshalError describes an invalid argument passed to Unmarshal.
// The argument must be a non-nil pointer.
type InvalidUnmarshalError struct {
	Type reflect.Type
}

func (e *InvalidUnmarshalError) Error() string {
	if e.Type == nil {
		return "mtcsv: Unmarshal(nil)"
	}
	if e.Type.Kind() != reflect.Pointer {
		return "mtcsv: Unmarshal(non-pointer " + e.Type.String() + ")"
	}
	return "mtcsv: Unmarshal(nil " + e.Type.String() + ")"
}

// An UnmarshalTypeError describes a cell that could not be stored in a value
// of the target Go type.
type UnmarshalTypeError struct {
	Value  string       // the cell text
	Type   reflect.Type // the target type
	Table  string       // name or position of the table
	Column string       // column name
	Row    int          // zero-based data row index
	Err    error        // the underlying conversion error, if any
}

func (e *UnmarshalTypeError) Error() string {
	var b strings.Builder
	b.WriteString("mtcsv: cannot unmarshal ")
	b.WriteString(strconv.Quote(e.Value))
	b.WriteString(" into Go value of type ")
	if e.Type != nil {
		b.WriteString(e.Type.String())
	} else {
		b.WriteString("<nil>")
	}
	if e.Table != "" || e.Column != "" {
		b.WriteString(" (table ")
		b.WriteString(e.Table)
		b.WriteString(", column ")
		b.WriteString(strconv.Quote(e.Column))
		b.WriteString(", row ")
		b.WriteString(strconv.Itoa(e.Row))
		b.WriteString(")")
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

func (e *UnmarshalTypeError) Unwrap() error { return e.Err }

// An UnsupportedTypeError reports an attempt to encode a Go type that has no
// MTCSV representation. MTCSV is flat and rectangular, so nested composites
// are rejected rather than flattened.
type UnsupportedTypeError struct {
	Type reflect.Type
	Msg  string
}

func (e *UnsupportedTypeError) Error() string {
	name := "<nil>"
	if e.Type != nil {
		name = e.Type.String()
	}
	if e.Msg != "" {
		return "mtcsv: unsupported type: " + name + ": " + e.Msg
	}
	return "mtcsv: unsupported type: " + name
}

// An UnknownColumnError reports a column present in the document that has no
// corresponding struct field. It is only returned when
// Decoder.DisallowUnknownColumns has been called.
type UnknownColumnError struct {
	Table  string
	Column string
	Type   reflect.Type
}

func (e *UnknownColumnError) Error() string {
	return "mtcsv: unknown column " + strconv.Quote(e.Column) +
		" in table " + e.Table + " has no field in " + e.Type.String()
}

// An UnknownTableError reports a table present in the document that has no
// corresponding struct field or map entry. It is only returned when
// Decoder.DisallowUnknownTables has been called.
type UnknownTableError struct {
	Table string
	Type  reflect.Type
}

func (e *UnknownTableError) Error() string {
	return "mtcsv: unknown table " + strconv.Quote(e.Table) +
		" has no field in " + e.Type.String()
}
