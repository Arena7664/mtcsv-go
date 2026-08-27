package mtcsv

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestUnmarshalSlice(t *testing.T) {
	var got []user
	in := "# users\nid,name,email,status\n1,Alice,alice@example.com,active\n2,Bob,\"bob, jr.\",suspended\n"
	if err := Unmarshal([]byte(in), &got); err != nil {
		t.Fatal(err)
	}
	want := []user{
		{ID: 1, Name: "Alice", Email: "alice@example.com", Status: "active"},
		{ID: 2, Name: "Bob", Email: "bob, jr.", Status: "suspended"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestUnmarshalContainer(t *testing.T) {
	type tag struct {
		OrderID int    `mtcsv:"order_id"`
		Tag     string `mtcsv:"tag"`
	}
	type file struct {
		Users []user `mtcsv:"users"`
		Tags  []tag  `mtcsv:"tags"`
	}
	in := "# tags\norder_id,tag\n10,priority\n\n# users\nid,name\n1,Alice\n"
	var got file
	if err := Unmarshal([]byte(in), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Users) != 1 || got.Users[0].Name != "Alice" {
		t.Errorf("users = %+v", got.Users)
	}
	if len(got.Tags) != 1 || got.Tags[0].OrderID != 10 {
		t.Errorf("tags = %+v", got.Tags)
	}
}

func TestUnmarshalContainerMatchingRules(t *testing.T) {
	type file struct {
		Users []map[string]string `mtcsv:"users"`
		Other []map[string]string // matched case-insensitively by field name
		Extra []map[string]string // takes the leftover anonymous table
	}
	in := "# users\na\n1\n\n# OTHER\nb\n2\n\nc\n3\n"
	var got file
	if err := Unmarshal([]byte(in), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Users) != 1 || got.Users[0]["a"] != "1" {
		t.Errorf("users = %+v", got.Users)
	}
	if len(got.Other) != 1 || got.Other[0]["b"] != "2" {
		t.Errorf("other = %+v", got.Other)
	}
	if len(got.Extra) != 1 || got.Extra[0]["c"] != "3" {
		t.Errorf("extra = %+v", got.Extra)
	}
}

func TestUnmarshalAppendsSameNamedTables(t *testing.T) {
	type file struct {
		Events []event `mtcsv:"events"`
	}
	in := "# events\nid,kind\n1,login\n\n# events\nid,kind\n2,logout\n"
	var got file
	if err := Unmarshal([]byte(in), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 2 || got.Events[1].Kind != "logout" {
		t.Errorf("events = %+v", got.Events)
	}

	// The same holds for a bare slice target.
	var flat []event
	if err := Unmarshal([]byte(in), &flat); err != nil {
		t.Fatal(err)
	}
	if len(flat) != 2 {
		t.Errorf("flat = %+v", flat)
	}
}

func TestUnmarshalTargets(t *testing.T) {
	in := "# t\na,b\n1,2\n3,4\n"

	var maps []map[string]string
	if err := Unmarshal([]byte(in), &maps); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(maps, []map[string]string{{"a": "1", "b": "2"}, {"a": "3", "b": "4"}}) {
		t.Errorf("maps = %+v", maps)
	}

	var ints []map[string]int
	if err := Unmarshal([]byte(in), &ints); err != nil {
		t.Fatal(err)
	}
	if ints[1]["b"] != 4 {
		t.Errorf("ints = %+v", ints)
	}

	var raw [][]string
	if err := Unmarshal([]byte(in), &raw); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(raw, [][]string{{"a", "b"}, {"1", "2"}, {"3", "4"}}) {
		t.Errorf("raw = %+v", raw) // the header is row 0, as when encoding
	}

	var byName map[string][]map[string]string
	if err := Unmarshal([]byte(in), &byName); err != nil {
		t.Fatal(err)
	}
	if len(byName["t"]) != 2 {
		t.Errorf("byName = %+v", byName)
	}

	var doc Document
	if err := Unmarshal([]byte(in), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Tables) != 1 || doc.Tables[0].Name != "t" {
		t.Errorf("doc = %+v", doc.Tables)
	}

	var single []int
	if err := Unmarshal([]byte("n\n1\n2\n"), &single); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(single, []int{1, 2}) {
		t.Errorf("single = %+v", single)
	}

	var arr [1]map[string]string
	if err := Unmarshal([]byte(in), &arr); err != nil {
		t.Fatal(err)
	}
	if arr[0]["a"] != "1" {
		t.Errorf("array = %+v", arr)
	}
}

func TestUnmarshalCellTypes(t *testing.T) {
	type row struct {
		S    string    `mtcsv:"s"`
		B    bool      `mtcsv:"b"`
		Yes  bool      `mtcsv:"yes"`
		I    int       `mtcsv:"i"`
		U    uint      `mtcsv:"u"`
		F    float64   `mtcsv:"f"`
		By   []byte    `mtcsv:"by"`
		Date time.Time `mtcsv:"date"`
		When time.Time `mtcsv:"when"`
		Ptr  *int      `mtcsv:"ptr"`
		Nil  *int      `mtcsv:"nil"`
		Miss string    `mtcsv:"missing"`
	}
	in := "s,b,yes,i,u,f,by,date,when,ptr,nil\n" +
		"x,true,YES,-3,7,1.5,raw,2024-01-05,2024-03-11T15:04:05Z,9,\n"
	var got []row
	if err := Unmarshal([]byte(in), &got); err != nil {
		t.Fatal(err)
	}
	nine := 9
	want := row{
		S: "x", B: true, Yes: true, I: -3, U: 7, F: 1.5, By: []byte("raw"),
		Date: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
		When: time.Date(2024, 3, 11, 15, 4, 5, 0, time.UTC),
		Ptr:  &nine,
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("got %+v, want %+v", got[0], want)
	}
}

func TestUnmarshalEmptyCellsAreZero(t *testing.T) {
	type row struct {
		I int       `mtcsv:"i"`
		F float64   `mtcsv:"f"`
		B bool      `mtcsv:"b"`
		T time.Time `mtcsv:"t"`
		P *int      `mtcsv:"p"`
	}
	var got []row
	if err := Unmarshal([]byte("i,f,b,t,p\n,,,,\n"), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []row{{}}) {
		t.Errorf("got %+v", got)
	}
}

type upper string

func (u *upper) UnmarshalMTCSV(cell string) error {
	*u = upper(strings.ToUpper(cell))
	return nil
}

func TestUnmarshalUnmarshaler(t *testing.T) {
	type row struct {
		U upper `mtcsv:"u"`
	}
	var got []row
	if err := Unmarshal([]byte("u\nabc\n"), &got); err != nil {
		t.Fatal(err)
	}
	if got[0].U != "ABC" {
		t.Errorf("u = %q", got[0].U)
	}
}

func TestUnmarshalEmbeddedStructs(t *testing.T) {
	type base struct {
		ID int `mtcsv:"id"`
	}
	type row struct {
		base
		Name string `mtcsv:"name"`
	}
	var got []row
	if err := Unmarshal([]byte("id,name\n5,x\n"), &got); err != nil {
		t.Fatal(err)
	}
	if got[0].ID != 5 || got[0].Name != "x" {
		t.Errorf("got %+v", got)
	}
	out, err := Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "id,name\n5,x\n" {
		t.Errorf("marshal = %q", out)
	}
}

func TestUnmarshalErrors(t *testing.T) {
	var rows []struct {
		N int `mtcsv:"n"`
	}
	err := Unmarshal([]byte("# t\nn\nnope\n"), &rows)
	var ute *UnmarshalTypeError
	if !errors.As(err, &ute) {
		t.Fatalf("err = %v (%T), want *UnmarshalTypeError", err, err)
	}
	if ute.Column != "n" || ute.Row != 0 || ute.Table != "t" || ute.Value != "nope" {
		t.Errorf("err = %+v", ute)
	}
	if !strings.Contains(ute.Error(), "invalid syntax") {
		t.Errorf("message = %q", ute.Error())
	}

	var iue *InvalidUnmarshalError
	if err := Unmarshal([]byte("a\n1\n"), nil); !errors.As(err, &iue) {
		t.Errorf("nil target: %v", err)
	}
	if err := Unmarshal([]byte("a\n1\n"), []user{}); !errors.As(err, &iue) {
		t.Errorf("non-pointer target: %v", err)
	}
}

func TestDecoderStrictAndDisallow(t *testing.T) {
	type row struct {
		A string `mtcsv:"a"`
	}
	in := "a,b\n1,2\n"

	var ok []row
	if err := Unmarshal([]byte(in), &ok); err != nil {
		t.Fatalf("unknown columns are ignored by default: %v", err)
	}

	dec := NewDecoder(strings.NewReader(in))
	dec.DisallowUnknownColumns()
	var strictRows []row
	var uce *UnknownColumnError
	if err := dec.Decode(&strictRows); !errors.As(err, &uce) || uce.Column != "b" {
		t.Errorf("err = %v", err)
	}

	// too-many-fields is an error-severity diagnostic.
	dec = NewDecoder(strings.NewReader("a\n1,2\n"))
	dec.Strict()
	var de *DiagnosticError
	if err := dec.Decode(&strictRows); !errors.As(err, &de) {
		t.Errorf("strict err = %v", err)
	}
	if err := Unmarshal([]byte("a\n1,2\n"), &strictRows); err != nil {
		t.Errorf("non-strict should tolerate surplus fields: %v", err)
	}

	type onlyUsers struct {
		Users []row `mtcsv:"users"`
	}
	dec = NewDecoder(strings.NewReader("# users\na\n1\n\n# extra\na\n2\n"))
	dec.DisallowUnknownTables()
	var ute *UnknownTableError
	if err := dec.Decode(&onlyUsers{}); !errors.As(err, &ute) {
		t.Errorf("err = %v", err)
	}
}

func TestDecoderStreaming(t *testing.T) {
	in := "# a\nx\n1\n\n# b\ny\n2\n"
	dec := NewDecoder(strings.NewReader(in))

	var first []map[string]string
	if err := dec.DecodeTable(&first); err != nil {
		t.Fatal(err)
	}
	if first[0]["x"] != "1" {
		t.Errorf("first = %+v", first)
	}
	if !dec.More() {
		t.Fatal("More = false, want another table")
	}
	tab, err := dec.NextTable()
	if err != nil || tab.Name != "b" {
		t.Fatalf("NextTable = %v, %v", tab, err)
	}
	if dec.More() {
		t.Error("More = true after the last table")
	}
	var none []map[string]string
	if err := dec.DecodeTable(&none); err != io.EOF {
		t.Errorf("err = %v, want io.EOF", err)
	}
}

func TestDecodeIntoStructAndTable(t *testing.T) {
	doc := mustParse(t, "# config\nkey,value\nk1,v1\n")

	var one struct {
		Key   string `mtcsv:"key"`
		Value string `mtcsv:"value"`
	}
	if err := doc.Table("config").Decode(&one); err != nil {
		t.Fatal(err)
	}
	if one.Key != "k1" || one.Value != "v1" {
		t.Errorf("got %+v", one)
	}

	var tab Table
	if err := doc.Table("config").Decode(&tab); err != nil {
		t.Fatal(err)
	}
	if tab.Name != "config" {
		t.Errorf("table = %+v", tab)
	}
}

func TestDocumentDecodeSkipsMissingTables(t *testing.T) {
	type file struct {
		Users   []user `mtcsv:"users"`
		Missing []user `mtcsv:"missing"`
	}
	var got file
	if err := Unmarshal([]byte("# users\nid\n1\n"), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Users) != 1 || got.Missing != nil {
		t.Errorf("got %+v", got)
	}
}

func TestValid(t *testing.T) {
	if !Valid([]byte("# t\na,b\n1,2\n")) {
		t.Error("valid document reported invalid")
	}
	if Valid([]byte("a\n1,2\n")) {
		t.Error("over-long row should be invalid")
	}
}
