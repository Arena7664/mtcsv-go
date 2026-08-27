package mtcsv

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
)

// benchRow is the row type used by the marshal and unmarshal benchmarks.
type benchRow struct {
	ID      int     `mtcsv:"id"      mtcsv.type:"int"`
	Name    string  `mtcsv:"name"`
	Email   string  `mtcsv:"email"   mtcsv.type:"email" mtcsv.doc:"primary contact"`
	Status  string  `mtcsv:"status"  mtcsv.type:"enum"`
	Balance float64 `mtcsv:"balance" mtcsv.type:"number"`
	Active  bool    `mtcsv:"active"  mtcsv.type:"bool"`
}

// benchDoc builds a document of tables x rows, with a share of cells that need
// quoting, so the benchmarks exercise the slow paths as well as the fast ones.
func benchDoc(tables, rows int) string {
	var b strings.Builder
	for t := 0; t < tables; t++ {
		if t > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "# users%d currency=AUD schema=v2\n", t)
		b.WriteString("#! Registered accounts. Deactivated users are kept.\n")
		b.WriteString("id,name,email,status,balance,active\n")
		b.WriteString("#: id      (int)    unique user id, never reused\n")
		b.WriteString("#: email   (email)  primary contact\n")
		b.WriteString("#: email            bounces are retried for 72h\n")
		b.WriteString("#: status  (enum)   one of: active, suspended, deleted\n")
		for r := 0; r < rows; r++ {
			switch r % 8 {
			case 3: // a quoted cell with a comma
				fmt.Fprintf(&b, "%d,\"Doe, Jane\",jane%d@example.com,active,12.50,true\n", r, r)
			case 5: // a quoted cell spanning two physical lines
				fmt.Fprintf(&b, "%d,\"multi\nline\",m%d@example.com,suspended,0,false\n", r, r)
			case 7: // a free comment between rows
				b.WriteString("# a note about the row below\n")
				fmt.Fprintf(&b, "%d,Plain,p%d@example.com,active,3.25,true\n", r, r)
			default:
				fmt.Fprintf(&b, "%d,Name%d,n%d@example.com,active,99.99,true\n", r, r, r)
			}
		}
	}
	return b.String()
}

func benchRows(n int) []benchRow {
	rows := make([]benchRow, n)
	for i := range rows {
		rows[i] = benchRow{
			ID: i, Name: fmt.Sprintf("Name%d", i), Email: fmt.Sprintf("n%d@example.com", i),
			Status: "active", Balance: 99.99, Active: true,
		}
		if i%8 == 3 {
			rows[i].Name = "Doe, Jane"
		}
	}
	return rows
}

func BenchmarkParse(b *testing.B) {
	for _, size := range []struct {
		name         string
		tables, rows int
	}{
		{"1x100", 1, 100},
		{"1x10000", 1, 10000},
		{"100x100", 100, 100},
	} {
		data := []byte(benchDoc(size.tables, size.rows))
		b.Run(size.name, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Parse(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkParsePlainCSV measures the floor: no structural lines, no quoting.
func BenchmarkParsePlainCSV(b *testing.B) {
	var sb strings.Builder
	sb.WriteString("id,name,email\n")
	for i := 0; i < 10000; i++ {
		fmt.Fprintf(&sb, "%d,Name%d,n%d@example.com\n", i, i, i)
	}
	data := []byte(sb.String())
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Parse(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalStructs(b *testing.B) {
	data := []byte(benchDoc(1, 10000))
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		var rows []benchRow
		if err := Unmarshal(data, &rows); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalMaps(b *testing.B) {
	data := []byte(benchDoc(1, 10000))
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		var rows []map[string]string
		if err := Unmarshal(data, &rows); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalRawRows(b *testing.B) {
	data := []byte(benchDoc(1, 10000))
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		var rows [][]string
		if err := Unmarshal(data, &rows); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalContainer(b *testing.B) {
	type file struct {
		A []benchRow `mtcsv:"users0"`
		B []benchRow `mtcsv:"users1"`
		C []benchRow `mtcsv:"users2"`
	}
	data := []byte(benchDoc(3, 1000))
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		var f file
		if err := Unmarshal(data, &f); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalStructs(b *testing.B) {
	rows := benchRows(10000)
	out, err := Marshal(rows)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(out)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Marshal(rows); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalRawRows(b *testing.B) {
	rows := make([][]string, 0, 10001)
	rows = append(rows, []string{"id", "name", "email"})
	for i := 0; i < 10000; i++ {
		rows = append(rows, []string{fmt.Sprint(i), "Name", "n@example.com"})
	}
	out, _ := Marshal(rows)
	b.SetBytes(int64(len(out)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Marshal(rows); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWriteDocument isolates the writer from the reflection layer.
func BenchmarkWriteDocument(b *testing.B) {
	doc, err := Parse([]byte(benchDoc(10, 1000)))
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(doc.Bytes())))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := doc.WriteTo(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecoderStreaming decodes one table at a time, the shape a large
// multi-table file would use.
func BenchmarkDecoderStreaming(b *testing.B) {
	data := benchDoc(100, 100)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		dec := NewDecoder(strings.NewReader(data))
		for dec.More() {
			var rows []benchRow
			if err := dec.DecodeTable(&rows); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkTableAccess measures the document API's lookup helpers.
func BenchmarkTableAccess(b *testing.B) {
	doc, _ := Parse([]byte(benchDoc(1, 1000)))
	t := doc.Table("users0")
	b.ReportAllocs()
	for b.Loop() {
		if t.Get(500, "email") == "" {
			b.Fatal("empty cell")
		}
	}
}

func BenchmarkMerge(b *testing.B) {
	doc, _ := Parse([]byte(strings.Repeat("# events\nid,kind\n1,login\n2,logout\n\n", 500)))
	b.ReportAllocs()
	for b.Loop() {
		if len(doc.Merge().Tables) != 1 {
			b.Fatal("merge failed")
		}
	}
}

// BenchmarkFieldCache measures repeated struct-layout lookups, which are
// cached per type after the first call.
func BenchmarkFieldCache(b *testing.B) {
	typ := reflect.TypeOf(benchRow{})
	b.ReportAllocs()
	for b.Loop() {
		if len(cachedFields(typ).list) != 6 {
			b.Fatal("unexpected field count")
		}
	}
}

// BenchmarkParseWideTable exercises the column-comment binding path, which
// must stay linear in the number of columns.
func BenchmarkParseWideTable(b *testing.B) {
	const n = 2000
	var sb strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "c%d", i)
	}
	sb.WriteByte('\n')
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "#: c%d (int) column %d\n", i, i)
	}
	data := []byte(sb.String())
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Parse(data); err != nil {
			b.Fatal(err)
		}
	}
}
