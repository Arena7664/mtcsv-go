package mtcsv

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestMetadataHelpers(t *testing.T) {
	m := Metadata{{"a", "1"}, {"a", "2"}, {"b", "3"}}
	if m.Get("a") != "1" || m.Get("missing") != "" {
		t.Errorf("Get = %q", m.Get("a"))
	}
	if !m.Has("b") || m.Has("c") {
		t.Error("Has")
	}
	if got := m.Map(); !reflect.DeepEqual(got, map[string]string{"a": "1", "b": "3"}) {
		t.Errorf("Map = %v", got) // first occurrence wins
	}
	m.Set("a", "9")
	m.Set("c", "4")
	if m.Get("a") != "9" || m.Get("c") != "4" || len(m) != 4 {
		t.Errorf("Set = %+v", m)
	}
	// A key that cannot be written is dropped rather than corrupting the line.
	bad := Metadata{{"", "x"}, {"a b", "x"}, {"ok", "y"}}
	if got := bad.String(); got != "ok=y" {
		t.Errorf("String = %q", got)
	}
}

func TestTableHelpers(t *testing.T) {
	tab := &Table{Name: "t"}
	tab.SetColumns("a", "b")
	tab.AppendRow("1")
	if got := tab.Rows[0]; !reflect.DeepEqual(got, []string{"1", ""}) {
		t.Errorf("AppendRow = %#v, want padding", got)
	}
	if got := tab.Records(); !reflect.DeepEqual(got, []map[string]string{{"a": "1", "b": ""}}) {
		t.Errorf("Records = %v", got)
	}
	if got := tab.String(); got != "# t\na,b\n1,\n" {
		t.Errorf("String = %q", got)
	}
	if tab.Get(9, "a") != "" || tab.Get(0, "nope") != "" || tab.Column("nope") != nil {
		t.Error("out-of-range accessors should be empty")
	}
	if tab.ID() != "t" {
		t.Errorf("ID = %q", tab.ID())
	}
}

func TestParseReader(t *testing.T) {
	doc, err := ParseReader(strings.NewReader("# t\na\n1\n"))
	if err != nil || doc.Table("t") == nil {
		t.Fatalf("doc = %+v, err = %v", doc, err)
	}
	if _, err := ParseReader(errReader{}); err == nil {
		t.Error("want the read error")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestDecoderOptionsAndDiagnostics(t *testing.T) {
	dec := NewDecoder(strings.NewReader("a,b\n1\n"))
	dec.SetParseOptions(ParseOptions{Hints: true})
	var rows []map[string]string
	if err := dec.Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(dec.Diagnostics()) != 1 || dec.Diagnostics()[0].Code != DiagRaggedShortRow {
		t.Errorf("diagnostics = %v", dec.Diagnostics())
	}
	if _, err := dec.Document(); err != nil {
		t.Errorf("Document = %v", err)
	}

	dec = NewDecoder(errReader{})
	if err := dec.Decode(&rows); err == nil {
		t.Error("want the read error")
	}
}

func TestDecoderStrictPerTable(t *testing.T) {
	dec := NewDecoder(strings.NewReader("# ok\na\n1\n\n# bad\na\n1,2\n"))
	dec.Strict()
	var rows []map[string]string
	if err := dec.DecodeTable(&rows); err != nil {
		t.Fatalf("clean table: %v", err)
	}
	var de *DiagnosticError
	if err := dec.DecodeTable(&rows); !errors.As(err, &de) {
		t.Errorf("err = %v, want a DiagnosticError for the second table", err)
	}
}

func TestErrorMessages(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&InvalidUnmarshalError{}, "mtcsv: Unmarshal(nil)"},
		{&InvalidUnmarshalError{Type: reflect.TypeOf(0)}, "mtcsv: Unmarshal(non-pointer int)"},
		{&InvalidUnmarshalError{Type: reflect.TypeOf((*int)(nil))}, "mtcsv: Unmarshal(nil *int)"},
		{&UnsupportedTypeError{Type: reflect.TypeOf([]int{}), Msg: "flat"}, "mtcsv: unsupported type: []int: flat"},
		{&UnknownColumnError{Table: "t", Column: "c", Type: reflect.TypeOf(0)}, `mtcsv: unknown column "c" in table t has no field in int`},
		{&UnknownTableError{Table: "t", Type: reflect.TypeOf(0)}, `mtcsv: unknown table "t" has no field in int`},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}

	inner := errors.New("inner")
	ute := &UnmarshalTypeError{Value: "x", Type: reflect.TypeOf(0), Table: "t", Column: "c", Err: inner}
	if !strings.Contains(ute.Error(), `cannot unmarshal "x" into Go value of type int`) {
		t.Errorf("message = %q", ute.Error())
	}
	if !errors.Is(ute, inner) {
		t.Error("Unwrap")
	}

	de := &DiagnosticError{Diagnostics: []Diagnostic{
		{Code: DiagTooManyFields, Severity: SeverityError, Message: "m", Line: 2},
		{Code: DiagUnterminatedQuote, Severity: SeverityError, Message: "n", Line: 5},
	}}
	if !strings.Contains(de.Error(), "2 errors:") || !strings.Contains(de.Error(), "line 5: error: n (unterminated-quote)") {
		t.Errorf("message = %q", de.Error())
	}
	if SeverityHint.String() != "hint" || Severity(9).String() != "unknown" {
		t.Error("Severity.String")
	}
}

func TestEncodeAnySlices(t *testing.T) {
	out, err := Marshal([]any{
		map[string]string{"a": "1", "b": "2"},
		map[string]string{"a": "3", "b": "4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "a,b\n1,2\n3,4\n"; string(out) != want {
		t.Errorf("maps: got %q, want %q", out, want)
	}

	if out, err = Marshal([]any{[]string{"a"}, []string{"1"}}); err != nil {
		t.Fatal(err)
	}
	if want := "a\n1\n"; string(out) != want {
		t.Errorf("rows: got %q, want %q", out, want)
	}

	if out, err = Marshal([]any{1, 2}); err != nil {
		t.Fatal(err)
	}
	if want := "value\n1\n2\n"; string(out) != want {
		t.Errorf("scalars: got %q, want %q", out, want)
	}

	if out, err = Marshal([]any{}); err != nil || string(out) != "value\n" {
		t.Errorf("empty: %q, %v", out, err)
	}
}

func TestEncodeStructFieldIsOneRowTable(t *testing.T) {
	type config struct {
		Key   string `mtcsv:"key"`
		Value string `mtcsv:"value"`
	}
	type file struct {
		Config config  `mtcsv:"config"`
		Tables []Table `mtcsv:"extra"`
	}
	out, err := Marshal(file{
		Config: config{"k", "v"},
		Tables: []Table{{Name: "extra", Columns: []Column{{Name: "a"}}, Rows: [][]string{{"1"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "# config\nkey,value\nk,v\n\n# extra\na\n1\n"
	if string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestEncodeDocumentValue(t *testing.T) {
	doc, _ := ParseString("# t\na\n1\n")
	out, err := Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "# t\na\n1\n" {
		t.Errorf("got %q", out)
	}
	if out, err = Marshal(*doc.At(0)); err != nil || string(out) != "# t\na\n1\n" {
		t.Errorf("table value: %q, %v", out, err)
	}
	if out, err = Marshal(nil); err != nil || len(out) != 0 {
		t.Errorf("nil: %q, %v", out, err)
	}
}

func TestEncodeNilElements(t *testing.T) {
	type row struct {
		A string `mtcsv:"a"`
	}
	out, err := Marshal([]*row{{A: "x"}, nil})
	if err != nil {
		t.Fatal(err)
	}
	// The nil row's single empty cell must be quoted, or it would read back
	// as a blank line, which separates tables.
	if want := "a\nx\n\"\"\n"; string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestEmbeddedFieldConflictsAndPointers(t *testing.T) {
	type Inner struct {
		Name string `mtcsv:"name"`
		Both string `mtcsv:"both"`
	}
	type outer struct {
		*Inner
		Both string `mtcsv:"both"` // shallower field wins
	}
	var rows []outer
	if err := Unmarshal([]byte("name,both\nn,b\n"), &rows); err != nil {
		t.Fatal(err)
	}
	if rows[0].Inner == nil || rows[0].Name != "n" {
		t.Fatalf("embedded pointer not allocated: %+v", rows[0])
	}
	if rows[0].Both != "b" || rows[0].Inner.Both != "" {
		t.Errorf("conflict resolution: %+v", rows[0])
	}
	out, err := Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if want := "name,both\nn,b\n"; string(out) != want {
		t.Errorf("marshal = %q, want %q", out, want)
	}
}

// A field reached through an embedded pointer to an unexported type cannot be
// set through reflection, so it is skipped rather than failing the decode.
// encoding/json rejects the same shape outright.
func TestEmbeddedUnexportedPointerIsSkipped(t *testing.T) {
	var rows []outerUnexported
	if err := Unmarshal([]byte("name,kept\nn,k\n"), &rows); err != nil {
		t.Fatal(err)
	}
	if rows[0].Kept != "k" {
		t.Errorf("ordinary field lost: %+v", rows[0])
	}
	if rows[0].hidden != nil {
		t.Errorf("hidden = %+v, want nil", rows[0].hidden)
	}
}

type hidden struct {
	Name string `mtcsv:"name"`
}

type outerUnexported struct {
	*hidden
	Kept string `mtcsv:"kept"`
}

func TestDecodeAnonymousTableTag(t *testing.T) {
	type file struct {
		Rows []map[string]string `mtcsv:"rows,anon"`
	}
	out, err := Marshal(file{Rows: []map[string]string{{"a": "1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "a\n1\n" {
		t.Errorf("anon table: %q", out)
	}
	var back file
	if err := Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Rows) != 1 || back.Rows[0]["a"] != "1" {
		t.Errorf("back = %+v", back)
	}
}

func TestDecodeIntoAny(t *testing.T) {
	doc, _ := ParseString("# t\na\n1\n")
	var v any
	if err := doc.Decode(&v); err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(*Document); !ok {
		t.Errorf("v = %T, want *Document", v)
	}

	var rows []any
	if err := doc.Table("t").Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rows, []any{map[string]string{"a": "1"}}) {
		t.Errorf("rows = %#v", rows)
	}
}

func TestDecodeUnsupportedTargets(t *testing.T) {
	doc, _ := ParseString("# t\na\n1\n")
	var n int
	var ute *UnsupportedTypeError
	if err := doc.Decode(&n); !errors.As(err, &ute) {
		t.Errorf("err = %v", err)
	}
	var bad map[int][]string
	if err := doc.Decode(&bad); !errors.As(err, &ute) {
		t.Errorf("err = %v", err)
	}
}

func TestDiagnosticString(t *testing.T) {
	d := Diagnostic{Code: DiagNoHeader, Severity: SeverityWarning, Message: "m", Line: 3}
	if got := d.String(); got != "line 3: warning: m (no-header)" {
		t.Errorf("got %q", got)
	}
}

// A row type that describes its own table keeps that identity inside a
// container struct, unless the field's tag names the table explicitly.
type auditEntry struct {
	Action string `mtcsv:"action"`
}

func (auditEntry) MTCSVTable() TableInfo {
	return TableInfo{Name: "audit_log", Description: "Every action.", Meta: Metadata{{"schema", "v3"}}}
}

func TestTableDescriptorInContainer(t *testing.T) {
	type file struct {
		Entries  []auditEntry `mtcsv.doc:"-"`                    // untagged: descriptor names it
		Override []auditEntry `mtcsv:"custom" mtcsv.doc:"Mine."` // tag wins
	}
	out, err := Marshal(file{
		Entries:  []auditEntry{{"login"}},
		Override: []auditEntry{{"logout"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "# audit_log schema=v3\n#! -\naction\nlogin\n\n" +
		"# custom schema=v3\n#! Mine.\naction\nlogout\n"
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}

	// The decoder uses the same names, so the container round trips.
	var back file
	if err := Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Entries) != 1 || back.Entries[0].Action != "login" {
		t.Errorf("entries = %+v", back.Entries)
	}
	if len(back.Override) != 1 || back.Override[0].Action != "logout" {
		t.Errorf("override = %+v", back.Override)
	}
}

func TestTableDescriptorAnonymous(t *testing.T) {
	out, err := Marshal([]anonRow{{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "a\nx\n" {
		t.Errorf("got %q, want an anonymous table", out)
	}
}

type anonRow struct {
	A string `mtcsv:"a"`
}

func (anonRow) MTCSVTable() TableInfo { return TableInfo{Anonymous: true} }
