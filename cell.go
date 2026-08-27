package mtcsv

import (
	"encoding"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Marshaler is implemented by types that encode themselves as an MTCSV cell.
type Marshaler interface {
	MarshalMTCSV() (string, error)
}

// Unmarshaler is implemented by types that decode themselves from an MTCSV
// cell. UnmarshalMTCSV may be called with the empty string.
type Unmarshaler interface {
	UnmarshalMTCSV(cell string) error
}

// TableInfo describes the marker line of a table.
type TableInfo struct {
	Name        string
	Description string
	Meta        Metadata
	// Anonymous suppresses the marker line entirely.
	Anonymous bool
}

// TableDescriptor is implemented by row types that know their own table's
// name, description and metadata. A struct tag on the containing field takes
// precedence over the values returned here.
type TableDescriptor interface {
	MTCSVTable() TableInfo
}

var (
	marshalerType     = reflect.TypeOf((*Marshaler)(nil)).Elem()
	unmarshalerType   = reflect.TypeOf((*Unmarshaler)(nil)).Elem()
	textMarshalerType = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	textUnmarshalType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
	tableDescType     = reflect.TypeOf((*TableDescriptor)(nil)).Elem()
	timeType          = reflect.TypeOf(time.Time{})
	byteSliceType     = reflect.TypeOf([]byte(nil))
)

// cellTypeCache memoizes isCellType, which sits on the decode hot path (it runs
// once per row).
var cellTypeCache sync.Map // reflect.Type -> bool

// isCellType reports whether values of type t encode to a single cell by
// themselves rather than being treated as a composite.
func isCellType(t reflect.Type) bool {
	if v, ok := cellTypeCache.Load(t); ok {
		return v.(bool)
	}
	v := isCellTypeSlow(t)
	cellTypeCache.Store(t, v)
	return v
}

func isCellTypeSlow(t reflect.Type) bool {
	if t == timeType {
		return true
	}
	pt := reflect.PointerTo(t)
	return t.Implements(marshalerType) || pt.Implements(marshalerType) ||
		t.Implements(textMarshalerType) || pt.Implements(textMarshalerType) ||
		pt.Implements(unmarshalerType) || pt.Implements(textUnmarshalType)
}

// timeLayouts are tried in order when decoding into a time.Time. The spec
// recommends YYYY-MM-DD for dates but doesn't pin down a datetime syntax, so
// we accept the common shapes.
var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"15:04:05",
	"15:04",
}

// encodeCell renders a single value as cell text. colType is the column's
// declared type, which selects the layout for time values.
func encodeCell(v reflect.Value, colType string) (string, error) {
	t := v.Type()

	// Interfaces and pointers: nil is the empty cell.
	switch t.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return "", nil
		}
	}

	if s, ok, err := marshalerCell(v, colType); ok {
		return s, err
	}

	switch t.Kind() {
	case reflect.Pointer, reflect.Interface:
		return encodeCell(v.Elem(), colType)
	case reflect.String:
		return v.String(), nil
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(v.Uint(), 10), nil
	case reflect.Float32:
		return formatFloat(v.Float(), 32), nil
	case reflect.Float64:
		return formatFloat(v.Float(), 64), nil
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 && t.Elem().PkgPath() == "" {
			return string(v.Bytes()), nil
		}
	}
	return "", &UnsupportedTypeError{Type: t, Msg: "MTCSV cells are flat text"}
}

// marshalerCell handles the types that render themselves: Marshaler,
// encoding.TextMarshaler and time.Time.
func marshalerCell(v reflect.Value, colType string) (string, bool, error) {
	t := v.Type()
	if t == timeType {
		return formatTime(v.Interface().(time.Time), colType), true, nil
	}
	if t.Implements(marshalerType) {
		s, err := v.Interface().(Marshaler).MarshalMTCSV()
		return s, true, err
	}
	if t.Implements(textMarshalerType) {
		b, err := v.Interface().(encoding.TextMarshaler).MarshalText()
		return string(b), true, err
	}
	// Methods declared on the pointer receiver need an addressable value.
	if t.Kind() != reflect.Pointer {
		pt := reflect.PointerTo(t)
		if pt.Implements(marshalerType) || pt.Implements(textMarshalerType) {
			p := reflect.New(t)
			p.Elem().Set(v)
			return marshalerCell(p, colType)
		}
	}
	return "", false, nil
}

func formatTime(tm time.Time, colType string) string {
	switch {
	case strings.EqualFold(colType, "date"):
		return tm.Format("2006-01-02")
	case strings.EqualFold(colType, "time"):
		return tm.Format("15:04:05")
	default:
		return tm.Format(time.RFC3339Nano)
	}
}

// formatFloat keeps small magnitudes in plain decimal form, as a human editing
// a CSV would expect, and falls back to exponent form at the extremes.
func formatFloat(f float64, bits int) string {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		switch {
		case math.IsNaN(f):
			return "NaN"
		case f > 0:
			return "Inf"
		default:
			return "-Inf"
		}
	}
	abs := math.Abs(f)
	fmtByte := byte('f')
	if abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		fmtByte = 'e'
	}
	return strconv.FormatFloat(f, fmtByte, -1, bits)
}

// decodeCell stores cell text in v, which must be settable.
func decodeCell(cell string, v reflect.Value) error {
	t := v.Type()

	// Pointers: an empty cell is the nil pointer.
	if t.Kind() == reflect.Pointer {
		if cell == "" {
			v.SetZero()
			return nil
		}
		if v.IsNil() {
			v.Set(reflect.New(t.Elem()))
		}
		return decodeCell(cell, v.Elem())
	}

	if ok, err := unmarshalerCell(cell, v); ok {
		return err
	}

	switch t.Kind() {
	case reflect.String:
		v.SetString(cell)
		return nil
	case reflect.Bool:
		b, err := parseBool(cell)
		if err != nil {
			return err
		}
		v.SetBool(b)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if cell == "" {
			v.SetZero()
			return nil
		}
		n, err := strconv.ParseInt(strings.TrimSpace(cell), 10, t.Bits())
		if err != nil {
			return unwrapNumError(err)
		}
		v.SetInt(n)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if cell == "" {
			v.SetZero()
			return nil
		}
		n, err := strconv.ParseUint(strings.TrimSpace(cell), 10, t.Bits())
		if err != nil {
			return unwrapNumError(err)
		}
		v.SetUint(n)
		return nil
	case reflect.Float32, reflect.Float64:
		if cell == "" {
			v.SetZero()
			return nil
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(cell), t.Bits())
		if err != nil {
			return unwrapNumError(err)
		}
		v.SetFloat(f)
		return nil
	case reflect.Interface:
		if t.NumMethod() == 0 {
			v.Set(reflect.ValueOf(cell))
			return nil
		}
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 && t.Elem().PkgPath() == "" {
			v.SetBytes([]byte(cell))
			return nil
		}
	}
	return fmt.Errorf("no MTCSV representation for %s", t)
}

// unmarshalerCell handles the types that decode themselves: Unmarshaler,
// encoding.TextUnmarshaler and time.Time.
func unmarshalerCell(cell string, v reflect.Value) (bool, error) {
	t := v.Type()
	if t == timeType {
		if cell == "" {
			v.SetZero()
			return true, nil
		}
		tm, err := parseTime(cell)
		if err != nil {
			return true, err
		}
		v.Set(reflect.ValueOf(tm))
		return true, nil
	}
	if !v.CanAddr() {
		return false, nil
	}
	p := v.Addr()
	if u, ok := p.Interface().(Unmarshaler); ok {
		return true, u.UnmarshalMTCSV(cell)
	}
	if u, ok := p.Interface().(encoding.TextUnmarshaler); ok {
		if cell == "" {
			v.SetZero()
			return true, nil
		}
		return true, u.UnmarshalText([]byte(cell))
	}
	return false, nil
}

func parseTime(cell string) (time.Time, error) {
	for _, layout := range timeLayouts {
		if tm, err := time.Parse(layout, cell); err == nil {
			return tm, nil
		}
	}
	return time.Time{}, errors.New("not a recognized date or time")
}

// parseBool accepts strconv's spellings plus the yes/no forms you see in the
// wild.
func parseBool(cell string) (bool, error) {
	if cell == "" {
		return false, nil
	}
	s := strings.TrimSpace(cell)
	switch s {
	case "1", "t", "true", "y", "yes", "on":
		return true, nil
	case "0", "f", "false", "n", "no", "off":
		return false, nil
	}
	// Non-canonical casing: fold it without allocating a lowercased copy on
	// the common path.
	switch strings.ToLower(s) {
	case "1", "t", "true", "y", "yes", "on":
		return true, nil
	case "0", "f", "false", "n", "no", "off":
		return false, nil
	}
	return false, errors.New("not a boolean")
}

func unwrapNumError(err error) error {
	var ne *strconv.NumError
	if errors.As(err, &ne) {
		return ne.Err
	}
	return err
}

// isEmptyValue reports whether a value should be written as an empty cell
// under the omitempty option.
func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String, reflect.Array, reflect.Map, reflect.Slice:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	case reflect.Struct:
		if v.Type() == timeType {
			return v.Interface().(time.Time).IsZero()
		}
		return v.IsZero()
	}
	return false
}
