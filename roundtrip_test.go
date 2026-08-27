package mtcsv

import (
	"encoding/csv"
	"reflect"
	"strings"
	"testing"
)

// corpus holds documents that exercise the format's awkward corners.
var corpus = []string{
	"",
	"\n\n\n",
	"a,b\n1,2\n",
	"# users\nid,name\n1,Alice\n\n# tags\nid,tag\n1,x\n",
	"# users currency=AUD schema=v2\n#! Line one.\n#! Line two.\nid,name\n#: id (int) the id\n#: id      and more\n1,Alice\n",
	"# \"line items\" source=\"orders export.json\"\nid\n1\n",
	"# t\na,b\n1,\"multi\nline\"\n2,\"has \"\"quotes\"\" and, commas\"\n",
	"# t\nonly\n\"\"\nx\n",
	"# t\na\n\"#hash\"\n\" leading\"\n\"trailing \"\n",
	"a,a\n1,2\n",
	"# t\n#! description only\n",
	"# t\na,b,c\n1\n",
	"# t\n\"a b\",c\n#: \"a b\" (int) spaced\n1,2\n",
	"#: orphan (int) no header here\n\na,b\n1,2\n",
	"# t\nname\n#: name () (parenthesised) description\nx\n",
	"# a\nx\n1\n\n# a\nx\n2\n",
	"# 0 \"0=\n",         // a metadata key may contain a quote character
	"# n a\"b=c\"d e=\n", // and a value may too
}

func TestWriteRoundTrip(t *testing.T) {
	for _, in := range corpus {
		t.Run(strings.SplitN(in, "\n", 2)[0], func(t *testing.T) {
			first, _ := ParseString(in)
			out := first.String()
			second, _ := ParseString(out)
			assertSameData(t, first, second, out)

			// Writing is idempotent: a second pass changes nothing.
			if again := second.String(); again != out {
				t.Errorf("writer not idempotent:\n%q\nvs\n%q", out, again)
			}
		})
	}
}

// assertSameData compares the data-bearing parts of two documents. Free
// comments are not preserved by design, and column documentation may be
// duplicated when a table has duplicate column names, so that case is skipped.
func assertSameData(t *testing.T, a, b *Document, out string) {
	t.Helper()
	want := significantTables(a)
	got := significantTables(b)
	if len(want) != len(got) {
		t.Fatalf("table count %d != %d\noutput:\n%s", len(got), len(want), out)
	}
	for i := range want {
		x, y := want[i], got[i]
		if x.Name != y.Name || x.Anonymous != y.Anonymous {
			t.Errorf("table %d identity: %q/%v != %q/%v", i, x.Name, x.Anonymous, y.Name, y.Anonymous)
		}
		if x.Description != y.Description {
			t.Errorf("table %d description: %q != %q", i, x.Description, y.Description)
		}
		if !reflect.DeepEqual(x.Meta, y.Meta) {
			t.Errorf("table %d meta: %+v != %+v", i, x.Meta, y.Meta)
		}
		if !reflect.DeepEqual(x.Rows, y.Rows) {
			t.Errorf("table %d rows: %#v != %#v\noutput:\n%s", i, x.Rows, y.Rows, out)
		}
		if hasDuplicateNames(x) {
			continue
		}
		if !reflect.DeepEqual(x.Columns, y.Columns) {
			t.Errorf("table %d columns: %+v != %+v\noutput:\n%s", i, x.Columns, y.Columns, out)
		}
	}
}

// significantTables drops sections that carried nothing a writer can express,
// such as a section of free comments only.
func significantTables(d *Document) []*Table {
	var out []*Table
	for _, t := range d.Tables {
		if t.Anonymous && t.Name == "" && t.Description == "" &&
			len(t.Columns) == 0 && len(t.Rows) == 0 {
			continue
		}
		out = append(out, t)
	}
	return out
}

func hasDuplicateNames(t *Table) bool {
	seen := map[string]bool{}
	for _, c := range t.Columns {
		if seen[c.Name] {
			return true
		}
		seen[c.Name] = true
	}
	return false
}

// TestOutputIsValidCSV checks the format's foundational promise: a generic
// RFC 4180 reader can consume any MTCSV file as one ragged table, and in
// particular that every data row survives with its cells intact.
//
// LazyQuotes is required because a structural line may carry a bare double
// quote - a quoted table or column name - which RFC 4180 doesn't allow in an
// unquoted field. The spec's own grammar permits it, so the promise holds for
// lenient readers, which is what most CSV tooling is.
func TestOutputIsValidCSV(t *testing.T) {
	for _, in := range corpus {
		doc, _ := ParseString(in)
		out := doc.String()
		r := csv.NewReader(strings.NewReader(out))
		r.FieldsPerRecord = -1
		r.LazyQuotes = true
		records, err := r.ReadAll()
		if err != nil {
			t.Fatalf("encoding/csv rejected our output for %q: %v\n%s", in, err, out)
		}
		// Every data row must appear verbatim among the CSV records.
		for _, tab := range doc.Tables {
			for _, row := range tab.Rows {
				if !containsRecord(records, row) {
					t.Errorf("row %#v lost to a generic CSV reader\n%s", row, out)
				}
			}
		}
	}
}

func containsRecord(records [][]string, row []string) bool {
	for _, r := range records {
		if reflect.DeepEqual(r, row) {
			return true
		}
	}
	return false
}

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	type row struct {
		Name  string  `mtcsv:"name"`
		Score float64 `mtcsv:"score" mtcsv.type:"number"`
		OK    bool    `mtcsv:"ok"`
		Note  string  `mtcsv:"note" mtcsv.doc:"free text"`
	}
	type file struct {
		Rows  []row    `mtcsv:"rows,schema=v1" mtcsv.doc:"Every awkward cell."`
		Extra []string `mtcsv:"extra"`
	}
	in := file{
		Rows: []row{
			{"plain", 1.5, true, "ok"},
			{"#hash", -0.25, false, "with, comma"},
			{" spaced ", 0, true, "with \"quotes\""},
			{"", 1e21, false, "multi\nline"},
		},
		Extra: []string{"", "x"},
	}
	data, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var got file
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	if !reflect.DeepEqual(in, got) {
		t.Errorf("round trip differs:\ngot  %+v\nwant %+v\n%s", got, in, data)
	}

	// The documentation survives too.
	doc := mustParse(t, string(data))
	rows := doc.Table("rows")
	if rows.Description != "Every awkward cell." || rows.Meta.Get("schema") != "v1" {
		t.Errorf("table metadata lost: %+v", rows)
	}
	if c := rows.Column("score"); c.Type != "number" {
		t.Errorf("column type lost: %+v", c)
	}
	if c := rows.Column("note"); c.Description != "free text" {
		t.Errorf("column doc lost: %+v", c)
	}
}

func FuzzParse(f *testing.F) {
	for _, in := range corpus {
		f.Add(in)
	}
	f.Add("#:\n#!\n#\n,\n\"\n")
	f.Fuzz(func(t *testing.T, in string) {
		first, _ := ParseString(in)
		out := first.String()
		second, _ := ParseString(out)
		assertSameData(t, first, second, out)
	})
}
