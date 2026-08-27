package mtcsv_test

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/Arena7664/mtcsv-go"
)

type User struct {
	ID     int    `mtcsv:"id"     mtcsv.type:"int"   mtcsv.doc:"unique user id, never reused"`
	Name   string `mtcsv:"name"`
	Email  string `mtcsv:"email"  mtcsv.type:"email" mtcsv.doc:"primary contact"`
	Status string `mtcsv:"status" mtcsv.type:"enum"`
}

type Tag struct {
	OrderID int    `mtcsv:"order_id"`
	Tag     string `mtcsv:"tag"`
}

func ExampleMarshal() {
	type File struct {
		Users []User `mtcsv:"users,currency=AUD,schema=v2" mtcsv.doc:"Registered accounts."`
		Tags  []Tag  `mtcsv:"tags"`
	}
	out, err := mtcsv.Marshal(File{
		Users: []User{
			{1, "Alice", "alice@example.com", "active"},
			{2, "Bob", "bob, jr.@example.com", "suspended"},
		},
		Tags: []Tag{{10, "priority"}},
	})
	if err != nil {
		log.Fatal(err)
	}
	os.Stdout.Write(out)
	// Output:
	// # users currency=AUD schema=v2
	// #! Registered accounts.
	// id,name,email,status
	// #: id     (int)   unique user id, never reused
	// #: email  (email) primary contact
	// #: status (enum)
	// 1,Alice,alice@example.com,active
	// 2,Bob,"bob, jr.@example.com",suspended
	//
	// # tags
	// order_id,tag
	// 10,priority
}

func ExampleUnmarshal() {
	data := []byte(`# users
#! Registered accounts.
id,name,email,status
#: id (int) unique user id
1,Alice,alice@example.com,active
2,Bob,"bob, jr.@example.com",suspended
`)
	var users []User
	if err := mtcsv.Unmarshal(data, &users); err != nil {
		log.Fatal(err)
	}
	for _, u := range users {
		fmt.Printf("%d %s <%s> %s\n", u.ID, u.Name, u.Email, u.Status)
	}
	// Output:
	// 1 Alice <alice@example.com> active
	// 2 Bob <bob, jr.@example.com> suspended
}

func ExampleUnmarshal_container() {
	data := []byte("# users\nid,name\n1,Alice\n\n# tags\norder_id,tag\n10,priority\n")
	var file struct {
		Users []User `mtcsv:"users"`
		Tags  []Tag  `mtcsv:"tags"`
	}
	if err := mtcsv.Unmarshal(data, &file); err != nil {
		log.Fatal(err)
	}
	fmt.Println(file.Users[0].Name, file.Tags[0].Tag)
	// Output: Alice priority
}

func ExampleParse() {
	doc, err := mtcsv.Parse([]byte(`# users currency=AUD
#! Registered accounts.
id,email
#: id    (int)   unique user id
#: email (email) primary contact
1,alice@example.com
`))
	if err != nil {
		log.Fatal(err)
	}
	t := doc.Table("users")
	fmt.Println(t.Name, t.Meta.Get("currency"), t.Description)
	for _, c := range t.Columns {
		fmt.Printf("%s (%s): %s\n", c.Name, c.Type, c.Description)
	}
	fmt.Println(t.Get(0, "email"))
	// Output:
	// users AUD Registered accounts.
	// id (int): unique user id
	// email (email): primary contact
	// alice@example.com
}

// Parse reports problems as diagnostics and still returns the data it could
// recover.
func ExampleDocument_Diagnostics() {
	doc, err := mtcsv.Parse([]byte("# t\na,b\n1,2,3\n#: c not a column\n"))
	fmt.Println("err:", err != nil)
	for _, d := range doc.Diagnostics {
		fmt.Printf("%s: %s (line %d)\n", d.Severity, d.Code, d.Line)
	}
	fmt.Println("rows:", doc.Table("t").Rows)
	// Output:
	// err: true
	// error: too-many-fields (line 3)
	// warning: unknown-column (line 4)
	// rows: [[1 2 3]]
}

// Sections that share a name are parts of one logical table; Merge
// concatenates them.
func ExampleDocument_Merge() {
	doc, _ := mtcsv.Parse([]byte("# events\nid,kind\n1,login\n\n# events\nid,kind\n2,logout\n"))
	fmt.Println(len(doc.Tables), "sections")
	merged := doc.Merge()
	fmt.Println(len(merged.Tables), "table,", len(merged.Table("events").Rows), "rows")
	// Output:
	// 2 sections
	// 1 table, 2 rows
}

// A Decoder can walk a document one table at a time.
func ExampleDecoder_DecodeTable() {
	dec := mtcsv.NewDecoder(strings.NewReader("# users\nid,name\n1,Alice\n\n# tags\norder_id,tag\n10,priority\n"))
	for {
		t, err := dec.NextTable()
		if err == io.EOF {
			break
		} else if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s: %d rows, columns %v\n", t.Name, len(t.Rows), t.ColumnNames())
	}
	// Output:
	// users: 1 rows, columns [id name]
	// tags: 1 rows, columns [order_id tag]
}

// An Encoder appends tables to one document, separating them with a blank
// line.
func ExampleEncoder_EncodeTable() {
	enc := mtcsv.NewEncoder(os.Stdout)
	if err := enc.EncodeTable("users", []User{{ID: 1, Name: "Alice"}}); err != nil {
		log.Fatal(err)
	}
	if err := enc.EncodeTable("tags", []Tag{{10, "priority"}}); err != nil {
		log.Fatal(err)
	}
	// Output:
	// # users
	// id,name,email,status
	// #: id     (int)   unique user id, never reused
	// #: email  (email) primary contact
	// #: status (enum)
	// 1,Alice,,
	//
	// # tags
	// order_id,tag
	// 10,priority
}

// A document of several tables maps to a container struct with one field per
// table.
func ExampleMarshal_multipleTables() {
	type Order struct {
		ID    int     `mtcsv:"id"    mtcsv.type:"int"`
		Total float64 `mtcsv:"total" mtcsv.type:"number"`
	}
	type LineItem struct {
		OrderID int    `mtcsv:"order_id" mtcsv.type:"int"`
		SKU     string `mtcsv:"sku"`
		Qty     int    `mtcsv:"qty"      mtcsv.type:"int"`
	}
	type Export struct {
		Orders []Order    `mtcsv:"orders,schema=v2" mtcsv.doc:"Completed checkouts only."`
		Items  []LineItem `mtcsv:"line_items"`
	}

	out, err := mtcsv.Marshal(Export{
		Orders: []Order{{1, 9.99}},
		Items:  []LineItem{{1, "ABC", 2}},
	})
	if err != nil {
		log.Fatal(err)
	}
	os.Stdout.Write(out)

	// Decoding the same bytes restores both slices, matching tables to fields
	// by name in whatever order they appear.
	var back Export
	if err := mtcsv.Unmarshal(out, &back); err != nil {
		log.Fatal(err)
	}
	fmt.Println(back.Orders[0].Total, back.Items[0].SKU)

	// Output:
	// # orders schema=v2
	// #! Completed checkouts only.
	// id,total
	// #: id    (int)
	// #: total (number)
	// 1,9.99
	//
	// # line_items
	// order_id,sku,qty
	// #: order_id (int)
	// #: qty      (int)
	// 1,ABC,2
	// 9.99 ABC
}

// Sections that share a name are parts of one table, so a single field
// collects them all.
func ExampleUnmarshal_appendedSections() {
	data := []byte("# events\nid,kind\n1,login\n\n# events\nid,kind\n2,logout\n")
	var file struct {
		Events []struct {
			ID   int    `mtcsv:"id"`
			Kind string `mtcsv:"kind"`
		} `mtcsv:"events"`
	}
	if err := mtcsv.Unmarshal(data, &file); err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(file.Events), file.Events[1].Kind)
	// Output: 2 logout
}

// A row type can name its own table, and both directions honour it.
func ExampleTableDescriptor() {
	out, err := mtcsv.Marshal(struct {
		Entries []AuditEntry // no tag: the descriptor names the table
	}{Entries: []AuditEntry{{"login"}}})
	if err != nil {
		log.Fatal(err)
	}
	os.Stdout.Write(out)
	// Output:
	// # audit_log schema=v3
	// #! Every action taken.
	// action
	// login
}

type AuditEntry struct {
	Action string `mtcsv:"action"`
}

func (AuditEntry) MTCSVTable() mtcsv.TableInfo {
	return mtcsv.TableInfo{
		Name:        "audit_log",
		Description: "Every action taken.",
		Meta:        mtcsv.Metadata{{Key: "schema", Value: "v3"}},
	}
}

// Cells convert through Marshaler and Unmarshaler, so a domain type keeps its
// own spelling in the file.
func ExampleMarshaler() {
	type Line struct {
		Total Money `mtcsv:"total" mtcsv.type:"currency"`
	}
	out, err := mtcsv.Marshal([]Line{{Total: 1999}})
	if err != nil {
		log.Fatal(err)
	}
	os.Stdout.Write(out)

	var back []Line
	if err := mtcsv.Unmarshal(out, &back); err != nil {
		log.Fatal(err)
	}
	fmt.Println(int64(back[0].Total))

	// Output:
	// total
	// #: total (currency)
	// 19.99
	// 1999
}

// Money is an amount in cents.
type Money int64

func (m Money) MarshalMTCSV() (string, error) {
	return fmt.Sprintf("%d.%02d", m/100, m%100), nil
}

func (m *Money) UnmarshalMTCSV(cell string) error {
	if cell == "" {
		*m = 0
		return nil
	}
	var dollars, cents int64
	if _, err := fmt.Sscanf(cell, "%d.%02d", &dollars, &cents); err != nil {
		return err
	}
	*m = Money(dollars*100 + cents)
	return nil
}
