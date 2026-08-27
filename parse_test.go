package mtcsv

import (
	"reflect"
	"strings"
	"testing"
)

func mustParse(t *testing.T, s string) *Document {
	t.Helper()
	doc, err := ParseString(s)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc
}

func TestParseMinimalFile(t *testing.T) {
	// Spec §15.1
	doc := mustParse(t, `# users
id,name,email
1,Alice,alice@example.com
2,Bob,bob@example.com

# tags
order_id,tag
10,priority
`)
	if len(doc.Tables) != 2 {
		t.Fatalf("got %d tables, want 2", len(doc.Tables))
	}
	users := doc.Table("users")
	if users == nil || users.Anonymous {
		t.Fatalf("users table missing: %+v", doc.Tables[0])
	}
	if got := users.ColumnNames(); !reflect.DeepEqual(got, []string{"id", "name", "email"}) {
		t.Errorf("columns = %q", got)
	}
	if len(users.Rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(users.Rows))
	}
	if got := users.Get(1, "email"); got != "bob@example.com" {
		t.Errorf("Get = %q", got)
	}
	tags := doc.Table("tags")
	if tags.Index != 1 || len(tags.Rows) != 1 {
		t.Errorf("tags = %+v", tags)
	}
}

func TestParseMetadataDescriptionAndColumnComments(t *testing.T) {
	// Spec §15.2
	doc := mustParse(t, `# users currency=AUD schema=v2
#! Registered accounts. Deactivated users are kept, not deleted.
id,name,email,status,created
#: id       (int)    unique user id, never reused
#: email    (email)  primary contact; may be blank for SSO accounts
#: email             bounces are retried for 72h,
#: email             then the address is marked unverified
#: status   (enum)   one of: active, suspended, deleted
1,Alice,alice@example.com,active,2024-01-05
2,Bob,"bob, jr.@example.com",suspended,2024-03-11
`)
	users := doc.Table("users")
	if got := users.Meta.Get("currency"); got != "AUD" {
		t.Errorf("currency = %q", got)
	}
	if got := users.Meta.Get("schema"); got != "v2" {
		t.Errorf("schema = %q", got)
	}
	if want := "Registered accounts. Deactivated users are kept, not deleted."; users.Description != want {
		t.Errorf("description = %q", users.Description)
	}
	if got := users.Column("id").Type; got != "int" {
		t.Errorf("id type = %q", got)
	}
	email := users.Column("email")
	if email.Type != "email" {
		t.Errorf("email type = %q", email.Type)
	}
	wantDesc := "primary contact; may be blank for SSO accounts\nbounces are retried for 72h,\nthen the address is marked unverified"
	if email.Description != wantDesc {
		t.Errorf("email description = %q, want %q", email.Description, wantDesc)
	}
	if got := users.Column("created").Type; got != "" {
		t.Errorf("created type = %q, want empty", got)
	}
	if got := users.Get(1, "email"); got != "bob, jr.@example.com" {
		t.Errorf("quoted comma cell = %q", got)
	}
}

func TestParseQuotedFieldsAndHashRule(t *testing.T) {
	// Spec §15.3
	doc := mustParse(t, "# notes\nid,body\n1,\"a value with, a comma\"\n2,\"a value\nthat spans two lines\"\n3,\"#not-a-comment\"\n")
	notes := doc.Table("notes")
	if len(notes.Rows) != 3 {
		t.Fatalf("got %d rows, want 3:\n%#v", len(notes.Rows), notes.Rows)
	}
	if got := notes.Get(1, "body"); got != "a value\nthat spans two lines" {
		t.Errorf("embedded newline cell = %q", got)
	}
	if got := notes.Get(2, "body"); got != "#not-a-comment" {
		t.Errorf("hash cell = %q", got)
	}
}

func TestParseBlankLineInsideQuotedFieldIsNotASeparator(t *testing.T) {
	doc := mustParse(t, "# t\na,b\n1,\"line one\n\nline three\"\n2,x\n")
	tab := doc.Table("t")
	if len(doc.Tables) != 1 {
		t.Fatalf("got %d tables, want 1", len(doc.Tables))
	}
	if len(tab.Rows) != 2 {
		t.Fatalf("got %d rows, want 2: %#v", len(tab.Rows), tab.Rows)
	}
	if got := tab.Get(0, "b"); got != "line one\n\nline three" {
		t.Errorf("cell = %q", got)
	}
}

func TestParseHashInsideQuotedFieldAtLineStart(t *testing.T) {
	doc := mustParse(t, "# t\na,b\n1,\"x\n# not a marker\ny\"\n")
	if got := doc.Table("t").Get(0, "b"); got != "x\n# not a marker\ny" {
		t.Errorf("cell = %q", got)
	}
}

func TestParseAnonymousTables(t *testing.T) {
	// Spec §15.4
	doc := mustParse(t, "a,b\n1,2\n\nx,y,z\n3,4,5\n")
	if len(doc.Tables) != 2 {
		t.Fatalf("got %d tables", len(doc.Tables))
	}
	for i, tab := range doc.Tables {
		if !tab.Anonymous || tab.Index != i || tab.ID() != []string{"0", "1"}[i] {
			t.Errorf("table %d = %+v", i, tab)
		}
	}
	if got := doc.At(1).ColumnNames(); !reflect.DeepEqual(got, []string{"x", "y", "z"}) {
		t.Errorf("columns = %q", got)
	}
	if doc.Table("1") != doc.At(1) {
		t.Error("positional addressing failed")
	}
}

func TestParseSameNameSectionsAndMerge(t *testing.T) {
	// Spec §15.5
	doc := mustParse(t, "# events\nid,kind\n1,login\n\n# events\nid,kind\n2,logout\n")
	if len(doc.Tables) != 2 {
		t.Fatalf("got %d tables, want 2 before merge", len(doc.Tables))
	}
	merged := doc.Merge()
	if len(merged.Tables) != 1 {
		t.Fatalf("got %d tables after merge", len(merged.Tables))
	}
	ev := merged.Table("events")
	if len(ev.Rows) != 2 || ev.Get(1, "kind") != "logout" {
		t.Errorf("merged rows = %#v", ev.Rows)
	}
}

func TestMergeRealignsDifferingHeaders(t *testing.T) {
	doc := mustParse(t, "# t a=1\nid,name\n1,one\n\n# t a=2 b=3\nname,id,extra\ntwo,2,x\n")
	tab := doc.Merge().Table("t")
	if got := tab.ColumnNames(); !reflect.DeepEqual(got, []string{"id", "name", "extra"}) {
		t.Fatalf("columns = %q", got)
	}
	if got := tab.Rows; !reflect.DeepEqual(got, [][]string{{"1", "one", ""}, {"2", "two", "x"}}) {
		t.Errorf("rows = %#v", got)
	}
	if tab.Meta.Get("a") != "1" || tab.Meta.Get("b") != "3" {
		t.Errorf("meta = %v", tab.Meta) // first occurrence wins
	}
}

func TestLineClassification(t *testing.T) {
	doc := mustParse(t, `# first marker wins
#! description
#:col (t) doc
#not-a-marker
   # indented free comment after marker
#
a,col
1,2
# comment between rows does not split the section
3,4
`)
	tab := doc.At(0)
	if tab.Name != "first" {
		t.Errorf("name = %q", tab.Name)
	}
	if got := tab.Meta.Get("marker"); got != "" {
		t.Errorf("meta = %v, want no pairs (tokens without '=' are ignored)", tab.Meta)
	}
	if tab.Description != "description" {
		t.Errorf("description = %q", tab.Description)
	}
	if c := tab.Column("col"); c.Type != "t" || c.Description != "doc" {
		t.Errorf("col = %+v", c)
	}
	if len(tab.Rows) != 2 {
		t.Errorf("rows = %#v", tab.Rows)
	}
}

func TestMarkerOnlyBeforeHeader(t *testing.T) {
	doc := mustParse(t, "a,b\n# looks like a marker but comes after the header\n1,2\n")
	tab := doc.At(0)
	if !tab.Anonymous {
		t.Errorf("table should stay anonymous, got name %q", tab.Name)
	}
	if len(tab.Rows) != 1 {
		t.Errorf("rows = %#v", tab.Rows)
	}
}

func TestMarkerQuotedNameAndMetadata(t *testing.T) {
	doc := mustParse(t, `# "line items" source="orders export.json" =ignored novalue currency=AUD
id
1
`)
	tab := doc.At(0)
	if tab.Name != "line items" {
		t.Errorf("name = %q", tab.Name)
	}
	if got := tab.Meta.Get("source"); got != "orders export.json" {
		t.Errorf("source = %q", got)
	}
	if got := tab.Meta.Get("currency"); got != "AUD" {
		t.Errorf("currency = %q", got)
	}
	if len(tab.Meta) != 2 {
		t.Errorf("meta = %+v, want only the two valid pairs", tab.Meta)
	}
}

func TestColumnCommentQuotedNameAndTypeRules(t *testing.T) {
	doc := mustParse(t, `# t
"a b",c,d
#: "a b" (int) spaced name
#: c (first) one
#: c (second) two
#: d nontype (x) rest
1,2,3
`)
	tab := doc.At(0)
	if c := tab.Column("a b"); c.Type != "int" || c.Description != "spaced name" {
		t.Errorf("a b = %+v", c)
	}
	if c := tab.Column("c"); c.Type != "first" || c.Description != "one\ntwo" {
		t.Errorf("c = %+v", c) // first declared type wins
	}
	if c := tab.Column("d"); c.Type != "" || c.Description != "nontype (x) rest" {
		t.Errorf("d = %+v", c) // a type group must be the first token after the name
	}
}

func TestShortAndLongRows(t *testing.T) {
	doc, err := ParseString("a,b,c\n1\n1,2,3,4\n")
	if err == nil {
		t.Fatal("want an error for the over-long row")
	}
	tab := doc.At(0)
	if got := tab.Rows[0]; !reflect.DeepEqual(got, []string{"1", "", ""}) {
		t.Errorf("short row = %#v, want padding", got)
	}
	if got := tab.Rows[1]; !reflect.DeepEqual(got, []string{"1", "2", "3", "4"}) {
		t.Errorf("long row = %#v, want surplus retained", got)
	}
	if !hasDiagnostic(doc, DiagTooManyFields) {
		t.Errorf("diagnostics = %v", doc.Diagnostics)
	}
}

func TestDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		in   string
		code string
	}{
		{"unknown column", "# t\na\n#: b doc\n1\n", DiagUnknownColumn},
		{"duplicate column", "# t\na,a\n1,2\n", DiagDuplicateColumn},
		{"no header", "# t\n#! just a description\n", DiagNoHeader},
		{"unterminated quote", "# t\na\n\"open\n", DiagUnterminatedQuote},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, _ := ParseString(tt.in)
			if !hasDiagnostic(doc, tt.code) {
				t.Errorf("diagnostics = %v, want %s", doc.Diagnostics, tt.code)
			}
		})
	}
}

func TestRaggedShortRowHintIsOptIn(t *testing.T) {
	doc, _ := ParseString("a,b\n1\n")
	if hasDiagnostic(doc, DiagRaggedShortRow) {
		t.Error("hint should be off by default")
	}
	doc, _ = ParseWith([]byte("a,b\n1\n"), ParseOptions{Hints: true})
	if !hasDiagnostic(doc, DiagRaggedShortRow) {
		t.Errorf("diagnostics = %v", doc.Diagnostics)
	}
}

func TestMaxRecordBytes(t *testing.T) {
	in := "a\n\"" + strings.Repeat("x", 100) + "\"\n"
	doc, err := ParseWith([]byte(in), ParseOptions{MaxRecordBytes: 16})
	if err == nil {
		t.Fatal("want an error")
	}
	if !hasDiagnostic(doc, DiagRecordTooLarge) {
		t.Errorf("diagnostics = %v", doc.Diagnostics)
	}
	if got := len(doc.At(0).Rows[0][0]); got > 32 {
		t.Errorf("field kept %d bytes, want it truncated near the limit", got)
	}
}

func TestLineEndingsAndBOM(t *testing.T) {
	for _, in := range []string{
		"\ufeff# t\r\na,b\r\n1,2\r\n",
		"# t\ra,b\r1,2\r",
		"# t\na,b\n1,2",
	} {
		doc := mustParse(t, in)
		tab := doc.Table("t")
		if tab == nil {
			t.Fatalf("no table for %q: %+v", in, doc.Tables)
		}
		if got := tab.Rows; !reflect.DeepEqual(got, [][]string{{"1", "2"}}) {
			t.Errorf("%q: rows = %#v", in, got)
		}
	}
}

func TestWhitespaceIsSignificant(t *testing.T) {
	doc := mustParse(t, "a,b\n a , b \n")
	if got := doc.At(0).Rows[0]; !reflect.DeepEqual(got, []string{" a ", " b "}) {
		t.Errorf("row = %#v", got)
	}
}

func TestQuotedEmptyRowIsData(t *testing.T) {
	doc := mustParse(t, "# t\nonly\n\"\"\nx\n")
	tab := doc.Table("t")
	if len(tab.Rows) != 2 || tab.Rows[0][0] != "" {
		t.Errorf("rows = %#v", tab.Rows)
	}
}

func TestBlankLinesCoalesceAndAreTrimmed(t *testing.T) {
	doc := mustParse(t, "\n\n# a\nx\n\n\n\n# b\ny\n\n\n")
	if len(doc.Tables) != 2 {
		t.Fatalf("got %d tables: %+v", len(doc.Tables), doc.Tables)
	}
}

func TestEscapedQuotesInCells(t *testing.T) {
	doc := mustParse(t, "a\n\"say \"\"hi\"\"\"\n")
	if got := doc.At(0).Rows[0][0]; got != `say "hi"` {
		t.Errorf("cell = %q", got)
	}
}

func TestParseEmptyInput(t *testing.T) {
	doc := mustParse(t, "")
	if len(doc.Tables) != 0 {
		t.Errorf("tables = %+v", doc.Tables)
	}
}

func TestParseMetadataHelper(t *testing.T) {
	meta := ParseMetadata(`currency=AUD source="orders export.json"`)
	if meta.Get("currency") != "AUD" || meta.Get("source") != "orders export.json" {
		t.Errorf("meta = %+v", meta)
	}
	if meta.String() != `currency=AUD source="orders export.json"` {
		t.Errorf("String = %q", meta.String())
	}
}

func hasDiagnostic(doc *Document, code string) bool {
	for _, d := range doc.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}
