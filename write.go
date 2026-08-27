package mtcsv

import (
	"bufio"
	"bytes"
	"io"
	"strings"
)

// This file implements the writer rules.

// sectionWriter is the write surface the helpers need. Both *bufio.Writer and
// *bytes.Buffer satisfy it, so the caller decides whether to buffer.
type sectionWriter interface {
	io.Writer
	io.StringWriter
	io.ByteWriter
}

// writeDocument writes every table, separated by exactly one blank line.
// A table with nothing to write - no marker, header, rows or documentation -
// is skipped, so it does not leave a stray separator behind.
func writeDocument(w io.Writer, d *Document, align bool) error {
	bw := bufio.NewWriter(w)
	wrote := false
	for _, t := range d.Tables {
		var section bytes.Buffer
		writeTableTo(&section, t, align)
		if section.Len() == 0 {
			continue
		}
		if wrote {
			bw.WriteByte('\n')
		}
		bw.Write(section.Bytes())
		wrote = true
	}
	return bw.Flush()
}

// writeTableTo emits one section: marker, descriptions, header, column
// comments, then data rows.
func writeTableTo(w sectionWriter, t *Table, align bool) {
	if !t.Anonymous {
		w.WriteString("# ")
		w.WriteString(quoteToken(t.Name))
		if meta := t.Meta.String(); meta != "" {
			w.WriteByte(' ')
			w.WriteString(meta)
		}
		w.WriteByte('\n')
	}
	for _, line := range splitDoc(t.Description) {
		writeCommentLine(w, "#!", line)
	}
	if len(t.Columns) > 0 {
		writeRecord(w, t.ColumnNames())
	}
	writeColumnComments(w, t, align)
	for _, row := range t.Rows {
		writeRecord(w, row)
	}
}

// writeColumnComments emits the '#:' lines for every documented or typed
// column. When align is set, names and type groups are padded into columns so
// the block reads as a table.
func writeColumnComments(w sectionWriter, t *Table, align bool) {
	nameW, typeW := 0, 0
	if align {
		for _, c := range t.Columns {
			if c.Type == "" && c.Description == "" {
				continue
			}
			if n := tokenLen(c.Name); n > nameW {
				nameW = n
			}
			if c.Type != "" {
				if n := len(c.Type) + 2; n > typeW {
					typeW = n
				}
			}
		}
	}
	for _, c := range t.Columns {
		if c.Type == "" && c.Description == "" {
			continue
		}
		lines := splitDoc(c.Description)
		if len(lines) == 0 {
			lines = []string{""}
		}
		for i, line := range lines {
			var b strings.Builder
			b.WriteString("#: ")
			b.WriteString(pad(quoteToken(c.Name), nameW))
			b.WriteByte(' ')
			// The type group belongs on the first line only. An empty group is
			// written where the text would otherwise be read as one.
			group := ""
			switch {
			case i == 0 && c.Type != "":
				group = "(" + c.Type + ")"
			case strings.HasPrefix(line, "("):
				group = "()"
			}
			b.WriteString(pad(group, typeW))
			b.WriteByte(' ')
			b.WriteString(line)
			w.WriteString(strings.TrimRight(b.String(), " \t"))
			w.WriteByte('\n')
		}
	}
}

func writeCommentLine(w sectionWriter, sigil, text string) {
	w.WriteString(sigil)
	if text != "" {
		w.WriteByte(' ')
		w.WriteString(text)
	}
	w.WriteByte('\n')
}

// splitDoc splits documentation text into lines. A '#!' or '#:' line can't
// carry a line break, so multi-line text becomes several lines.
func splitDoc(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(strings.ReplaceAll(s, "\r", "\n"), "\n")
}

func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// tokenLen returns the length quoteToken would produce, without allocating the
// quoted form. It mirrors quoteToken's rules.
func tokenLen(s string) int {
	if s == "" {
		return 2 // `""`
	}
	if !strings.ContainsAny(s, " \t\"=") {
		return len(s)
	}
	n := len(s) + 2
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			n++
		}
	}
	return n
}

// writeRecord writes one CSV record, quoting fields as the format requires.
func writeRecord(w sectionWriter, fields []string) {
	// A lone empty cell would look like a blank line, which separates
	// sections, so it has to be quoted.
	if len(fields) == 1 && fields[0] == "" {
		w.WriteString("\"\"\n")
		return
	}
	for i, f := range fields {
		if i > 0 {
			w.WriteByte(',')
		}
		w.WriteString(quoteField(f, i == 0))
	}
	w.WriteByte('\n')
}

// quoteField quotes a cell if the format requires it, or if quoting is needed
// to preserve leading or trailing whitespace.
func quoteField(s string, first bool) string {
	need := strings.ContainsAny(s, ",\"\r\n")
	if !need && first && strings.HasPrefix(s, "#") {
		need = true // otherwise the row would be read as a comment
	}
	if !need && s != "" && (isWS(s[0]) || isWS(s[len(s)-1])) {
		need = true
	}
	if !need {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// quoteToken quotes a marker name, metadata key/value or column-comment name
// when it wouldn't otherwise survive a round trip.
func quoteToken(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"=") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func isWS(c byte) bool { return c == ' ' || c == '\t' }
