package mtcsv

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ParseOptions controls optional reader behaviour.
type ParseOptions struct {
	// Hints turns on the opt-in hint-severity diagnostics, like
	// ragged-short-row.
	Hints bool
	// MaxRecordBytes bounds the size of a single record. A record can span the
	// whole file when a quoted field contains newlines, so set a limit for
	// untrusted input. Zero means unlimited.
	MaxRecordBytes int
}

// Parse reads an MTCSV document.
//
// A document is always returned, even for malformed input: every problem is
// reported through Document.Diagnostics. The error is non-nil only when an
// error-severity diagnostic was produced (see Document.Err), so a caller that
// wants best-effort data may ignore it.
func Parse(data []byte) (*Document, error) {
	return ParseWith(data, ParseOptions{})
}

// ParseString is Parse for a string.
func ParseString(s string) (*Document, error) {
	return ParseWith([]byte(s), ParseOptions{})
}

// ParseReader reads all of r and parses it. It returns any read error in
// preference to parse diagnostics.
func ParseReader(r io.Reader) (*Document, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return &Document{}, err
	}
	return Parse(data)
}

// ParseWith is Parse with explicit options.
func ParseWith(data []byte, opts ParseOptions) (*Document, error) {
	text := strings.TrimPrefix(string(data), "\ufeff") // strip a BOM
	p := &parser{lines: splitLines(text), opts: opts, doc: &Document{}}
	p.run()
	return p.doc, p.doc.Err()
}

type parser struct {
	lines []string
	opts  ParseOptions
	doc   *Document
	cur   *section
}

type section struct {
	table       *Table
	hasMarker   bool
	hasHeader   bool
	descLines   []string
	colComments []colComment
}

type colComment struct {
	name string
	typ  string
	desc string
	line int
}

func (p *parser) run() {
	for i := 0; i < len(p.lines); {
		line := p.lines[i]

		// 1. A blank line ends the current section.
		if isBlank(line) {
			p.closeSection()
			i++
			continue
		}

		// 2. A '#'-line. We are between records here, so no quote is open.
		if rest, ok := hashRest(line); ok {
			s := p.section(i)
			switch {
			case strings.HasPrefix(rest, ":"):
				s.colComments = append(s.colComments, parseColumnComment(rest[1:], i+1))
			case strings.HasPrefix(rest, "!"):
				s.descLines = append(s.descLines, strings.TrimSpace(rest[1:]))
			case !s.hasMarker && !s.hasHeader && startsMarkerName(rest):
				s.table.Name, s.table.Meta = parseMarker(rest)
				s.table.Anonymous = false
				s.hasMarker = true
			default:
				// Free comment: ignored.
			}
			i++
			continue
		}

		// 3. The start of a record, which may span physical lines.
		s := p.section(i)
		fields, end, unterminated, truncated := readRecord(p.lines, i, p.opts.MaxRecordBytes)
		if unterminated {
			p.diag(Diagnostic{
				Code: DiagUnterminatedQuote, Severity: SeverityError, Line: i + 1,
				Message: "quoted field is still open at end of file",
			})
		}
		if truncated {
			p.diag(Diagnostic{
				Code: DiagRecordTooLarge, Severity: SeverityError, Line: i + 1,
				Message: fmt.Sprintf("record exceeds the %d byte limit; truncated",
					p.opts.MaxRecordBytes),
			})
		}
		if !s.hasHeader {
			s.hasHeader = true
			s.table.SetColumns(fields...)
			p.checkDuplicateColumns(s, i+1)
		} else {
			p.addRow(s, fields, i+1)
		}
		i = end + 1
	}
	p.closeSection()
}

// section returns the section under construction, starting one if needed.
func (p *parser) section(line int) *section {
	if p.cur == nil {
		p.cur = &section{table: &Table{
			Anonymous: true,
			Index:     len(p.doc.Tables),
			Line:      line + 1,
		}}
	}
	return p.cur
}

func (p *parser) diag(d Diagnostic) {
	if d.Table == "" && p.cur != nil {
		d.Table = p.cur.table.ID()
	}
	p.doc.Diagnostics = append(p.doc.Diagnostics, d)
}

func (p *parser) addRow(s *section, fields []string, line int) {
	switch {
	case len(fields) > len(s.table.Columns):
		p.diag(Diagnostic{
			Code: DiagTooManyFields, Severity: SeverityError, Line: line,
			Message: fmt.Sprintf("row has %d fields but the header defines %d",
				len(fields), len(s.table.Columns)),
		})
	case len(fields) < len(s.table.Columns):
		if p.opts.Hints {
			p.diag(Diagnostic{
				Code: DiagRaggedShortRow, Severity: SeverityHint, Line: line,
				Message: fmt.Sprintf("row has %d fields but the header defines %d; padded",
					len(fields), len(s.table.Columns)),
			})
		}
		for len(fields) < len(s.table.Columns) {
			fields = append(fields, "")
		}
	}
	s.table.Rows = append(s.table.Rows, fields)
}

func (p *parser) checkDuplicateColumns(s *section, line int) {
	seen := make(map[string]bool, len(s.table.Columns))
	reported := make(map[string]bool)
	for _, c := range s.table.Columns {
		if seen[c.Name] && !reported[c.Name] {
			reported[c.Name] = true
			p.diag(Diagnostic{
				Code: DiagDuplicateColumn, Severity: SeverityWarning, Line: line,
				Message: "duplicate column name " + strconv.Quote(c.Name),
			})
		}
		seen[c.Name] = true
	}
}

// closeSection finalizes the section under construction: it binds column
// comments to columns by name and appends the table to the document.
func (p *parser) closeSection() {
	s := p.cur
	if s == nil {
		return
	}
	p.cur = nil

	s.table.Description = strings.Join(s.descLines, "\n")

	if !s.hasHeader {
		p.doc.Diagnostics = append(p.doc.Diagnostics, Diagnostic{
			Code: DiagNoHeader, Severity: SeverityWarning, Line: s.table.Line,
			Table:   s.table.ID(),
			Message: "section has structural lines but no header record",
		})
	}

	// Column comments bind by name, so index the header once rather than
	// scanning it per comment.
	byName := make(map[string][]int, len(s.table.Columns))
	for i, col := range s.table.Columns {
		byName[col.Name] = append(byName[col.Name], i)
	}

	descs := make([]([]string), len(s.table.Columns))
	for _, cc := range s.colComments {
		bound := len(byName[cc.name]) > 0
		for _, i := range byName[cc.name] {
			col := &s.table.Columns[i]
			if col.Type == "" { // first declared type wins
				col.Type = cc.typ
			}
			if cc.desc != "" {
				descs[i] = append(descs[i], cc.desc)
			}
		}
		if !bound {
			p.doc.Diagnostics = append(p.doc.Diagnostics, Diagnostic{
				Code: DiagUnknownColumn, Severity: SeverityWarning, Line: cc.line,
				Table:   s.table.ID(),
				Message: "column comment names unknown column " + strconv.Quote(cc.name),
			})
		}
	}
	for i := range s.table.Columns {
		s.table.Columns[i].Description = strings.Join(descs[i], "\n")
	}

	p.doc.Tables = append(p.doc.Tables, s.table)
}

// parseMarker parses the text after '#' on a marker line: a name, optionally
// quoted, followed by zero or more key=value pairs.
func parseMarker(rest string) (name string, meta Metadata) {
	i := skipWS(rest, 0)
	name, i = scanToken(rest, i)
	for {
		i = skipWS(rest, i)
		if i >= len(rest) {
			return name, meta
		}
		start := i
		// A key runs up to '=' and may not contain whitespace.
		for i < len(rest) && rest[i] != '=' && rest[i] != ' ' && rest[i] != '\t' {
			i++
		}
		if i >= len(rest) || rest[i] != '=' {
			continue // token without '=': ignored
		}
		key := rest[start:i]
		i++ // '='
		var value string
		value, i = scanToken(rest, i)
		if key == "" {
			continue // empty key: ignored
		}
		meta = append(meta, MetaEntry{Key: key, Value: value})
	}
}

// parseColumnComment parses the text after '#:'.
func parseColumnComment(rest string, line int) colComment {
	cc := colComment{line: line}
	i := skipWS(rest, 0)
	if i >= len(rest) {
		return cc
	}
	cc.name, i = scanToken(rest, i)

	// An optional type group must be separated from the name by whitespace and
	// must be the first token after it.
	if j := skipWS(rest, i); j > i && j < len(rest) && rest[j] == '(' {
		if k := strings.IndexByte(rest[j:], ')'); k >= 0 {
			cc.typ = strings.TrimSpace(rest[j+1 : j+k])
			i = j + k + 1
		}
	}
	cc.desc = strings.TrimSpace(rest[i:])
	return cc
}
