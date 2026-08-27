package mtcsv

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

type user struct {
	ID     int    `mtcsv:"id"     mtcsv.type:"int"   mtcsv.doc:"unique user id, never reused"`
	Name   string `mtcsv:"name"`
	Email  string `mtcsv:"email"  mtcsv.type:"email" mtcsv.doc:"primary contact\nmay be blank for SSO accounts"`
	Status string `mtcsv:"status" mtcsv.type:"enum"`
	secret string
	Skip   string `mtcsv:"-"`
}

func TestMarshalSlice(t *testing.T) {
	got, err := Marshal([]user{
		{ID: 1, Name: "Alice", Email: "alice@example.com", Status: "active"},
		{ID: 2, Name: "Bob", Email: "bob, jr.@example.com", Status: "suspended"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `id,name,email,status
#: id     (int)   unique user id, never reused
#: email  (email) primary contact
#: email          may be blank for SSO accounts
#: status (enum)
1,Alice,alice@example.com,active
2,Bob,"bob, jr.@example.com",suspended
`
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarshalContainer(t *testing.T) {
	type tag struct {
		OrderID int    `mtcsv:"order_id"`
		Tag     string `mtcsv:"tag"`
	}
	type file struct {
		Users []user `mtcsv:"users,currency=AUD,schema=v2" mtcsv.doc:"Registered accounts.\nDeactivated users are kept."`
		Tags  []tag  `mtcsv:"tags"`
		Skip  []tag  `mtcsv:"-"`
	}
	got, err := Marshal(file{
		Users: []user{{ID: 1, Name: "Alice", Email: "a@example.com", Status: "active"}},
		Tags:  []tag{{10, "priority"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `# users currency=AUD schema=v2
#! Registered accounts.
#! Deactivated users are kept.
id,name,email,status
#: id     (int)   unique user id, never reused
#: email  (email) primary contact
#: email          may be blank for SSO accounts
#: status (enum)
1,Alice,a@example.com,active

# tags
order_id,tag
10,priority
`
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarshalQuotingRules(t *testing.T) {
	type row struct {
		A string `mtcsv:"a"`
		B string `mtcsv:"b"`
	}
	got, err := Marshal([]row{
		{"#hash", "plain"},
		{" leading", "trailing "},
		{"has,comma", `has"quote`},
		{"has\nnewline", ""},
		{"", ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "a,b\n" +
		"\"#hash\",plain\n" +
		"\" leading\",\"trailing \"\n" +
		"\"has,comma\",\"has\"\"quote\"\n" +
		"\"has\nnewline\",\n" +
		",\n"
	if string(got) != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
	// The written file must read back as one table with the same cells.
	doc := mustParse(t, string(got))
	if len(doc.Tables) != 1 || len(doc.At(0).Rows) != 5 {
		t.Fatalf("round trip lost rows: %#v", doc.Tables)
	}
	if got := doc.At(0).Get(0, "a"); got != "#hash" {
		t.Errorf("first cell = %q", got)
	}
}

func TestMarshalSingleColumnEmptyRowIsQuoted(t *testing.T) {
	got, err := Marshal([]string{"", "x"})
	if err != nil {
		t.Fatal(err)
	}
	want := "value\n\"\"\nx\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
	doc := mustParse(t, string(got))
	if len(doc.Tables) != 1 || len(doc.At(0).Rows) != 2 {
		t.Errorf("empty row was lost: %#v", doc.Tables)
	}
}

func TestMarshalOmitEmpty(t *testing.T) {
	type row struct {
		N   int     `mtcsv:"n,omitempty"`
		M   int     `mtcsv:"m"`
		P   *int    `mtcsv:"p"`
		F   float64 `mtcsv:"f,omitempty"`
		Str string  `mtcsv:"s,omitempty"`
	}
	got, err := Marshal([]row{{}})
	if err != nil {
		t.Fatal(err)
	}
	want := "n,m,p,f,s\n,0,,,\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarshalTypes(t *testing.T) {
	type row struct {
		S    string    `mtcsv:"s"`
		B    bool      `mtcsv:"b"`
		I    int8      `mtcsv:"i"`
		U    uint64    `mtcsv:"u"`
		F    float64   `mtcsv:"f"`
		Tiny float64   `mtcsv:"tiny"`
		By   []byte    `mtcsv:"by"`
		When time.Time `mtcsv:"when" mtcsv.type:"date"`
		At   time.Time `mtcsv:"at"`
		Ptr  *string   `mtcsv:"ptr"`
	}
	s := "here"
	when := time.Date(2024, 3, 11, 15, 4, 5, 0, time.UTC)
	got, err := Marshal([]row{{
		S: "x", B: true, I: -8, U: 42, F: 1.5, Tiny: 1e-9,
		By: []byte("raw"), When: when, At: when, Ptr: &s,
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := "s,b,i,u,f,tiny,by,when,at,ptr\n" +
		"#: when (date)\n" +
		"x,true,-8,42,1.5,1e-09,raw,2024-03-11,2024-03-11T15:04:05Z,here\n"
	if string(got) != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

type celsius float64

func (c celsius) MarshalMTCSV() (string, error) { return string(rune('0'+int(c))) + "C", nil }

func TestMarshalMarshaler(t *testing.T) {
	type row struct {
		T celsius `mtcsv:"t"`
	}
	got, err := Marshal([]row{{3}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "t\n3C\n"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarshalUnsupportedType(t *testing.T) {
	type row struct {
		Nested []int `mtcsv:"nested"`
	}
	if _, err := Marshal([]row{{[]int{1}}}); err == nil {
		t.Fatal("want an error for a nested slice")
	}
	if _, err := Marshal(user{}); err == nil {
		t.Fatal("want an error: a top-level struct is a container of tables")
	}
}

func TestMarshalMapOfTables(t *testing.T) {
	got, err := Marshal(map[string][]map[string]string{
		"b": {{"x": "1", "y": "2"}},
		"a": {{"k": "v"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "# a\nk\nv\n\n# b\nx,y\n1,2\n"
	if string(got) != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestMarshalRawRows(t *testing.T) {
	got, err := Marshal([][]string{{"a", "b"}, {"1", "2"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "a,b\n1,2\n"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

type event struct {
	ID   int    `mtcsv:"id"`
	Kind string `mtcsv:"kind"`
}

func (event) MTCSVTable() TableInfo {
	return TableInfo{Name: "events", Description: "Audit log.", Meta: Metadata{{"schema", "v1"}}}
}

func TestMarshalTableDescriptor(t *testing.T) {
	got, err := Marshal([]event{{1, "login"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "# events schema=v1\n#! Audit log.\nid,kind\n1,login\n"
	if string(got) != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestEncoderStreaming(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.EncodeTable("first", []map[string]string{{"a": "1"}}); err != nil {
		t.Fatal(err)
	}
	if err := enc.EncodeTable("second", []map[string]string{{"b": "2"}}); err != nil {
		t.Fatal(err)
	}
	want := "# first\na\n1\n\n# second\nb\n2\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

func TestEncoderAlignmentOff(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.SetAlignComments(false)
	if err := enc.Encode([]user{{ID: 1}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "#: id (int) unique user id, never reused\n") {
		t.Errorf("got:\n%s", buf.String())
	}
}

func TestDocumentBuilding(t *testing.T) {
	doc := &Document{}
	if err := doc.AddTable("users", []user{{ID: 7, Name: "G"}}); err != nil {
		t.Fatal(err)
	}
	if err := doc.AddTable("", [][]string{{"a"}, {"1"}}); err != nil {
		t.Fatal(err)
	}
	if len(doc.Tables) != 2 || doc.Tables[1].Index != 1 {
		t.Fatalf("tables = %+v", doc.Tables)
	}
	out := doc.String()
	if !strings.HasPrefix(out, "# users\n") || !strings.HasSuffix(out, "\na\n1\n") {
		t.Errorf("document:\n%s", out)
	}
	var n bytes.Buffer
	if _, err := doc.WriteTo(&n); err != nil || n.String() != out {
		t.Errorf("WriteTo = %q, %v", n.String(), err)
	}
}
