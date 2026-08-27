package mtcsv

import "strings"

// This file implements the physical layer of the format: the split into
// physical lines, the quote-aware record reader, and the small token scanner
// shared by marker and column-comment parsing.

// splitLines splits text into physical lines at LF, CR LF or a lone CR. Line
// terminators aren't included in the returned lines, and a single trailing
// terminator doesn't produce a final empty line.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := make([]string, 0, strings.Count(text, "\n")+1)
	start := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\n':
			lines = append(lines, text[start:i])
			start = i + 1
		case '\r':
			lines = append(lines, text[start:i])
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

// readRecord reads one CSV record beginning at physical line start. Quote
// state is tracked across physical lines, so a line break inside a quoted
// field is field data, not a record terminator.
//
// It returns the record's cells (already unquoted), the index of the last
// physical line the record occupies, and whether the record ended with a quote
// still open. Line breaks inside a quoted field are normalized to LF.
//
// A '"' in the unquoted state opens a quoted span — the same lenient rule the
// spec's reference tokenizer uses.
//
// A positive limit bounds the number of bytes the record may occupy: once it's
// exceeded the record is abandoned, truncated is set, and reading resumes on
// the next physical line.
func readRecord(lines []string, start, limit int) (fields []string, end int, unterminated, truncated bool) {
	// Most records are one physical line, so its comma count is an exact
	// capacity for all but the multi-line case.
	fields = make([]string, 0, strings.Count(lines[start], ",")+1)
	var buf []byte // used only for fields that need unquoting
	buffered := false
	inQuotes := false
	size := 0
	line, col := start, 0
	fieldStart := 0

	for {
		s := lines[line]
		cur := col - fieldStart
		if buffered {
			cur = len(buf)
		}
		if limit > 0 && size+cur > limit {
			return append(fields, take(s, buf, buffered, fieldStart, col)), line, false, true
		}
		if col >= len(s) {
			if !inQuotes {
				return append(fields, take(s, buf, buffered, fieldStart, col)), line, false, false
			}
			// The line break is inside the field.
			buf = append(buf, '\n')
			line++
			col = 0
			if line >= len(lines) {
				return append(fields, string(buf)), line - 1, true, false
			}
			continue
		}
		c := s[col]
		if inQuotes {
			switch {
			case c == '"' && col+1 < len(s) && s[col+1] == '"':
				buf = append(buf, '"')
				col += 2
			case c == '"':
				inQuotes = false
				col++
			default:
				buf = append(buf, c)
				col++
			}
			continue
		}
		switch c {
		case '"':
			if !buffered { // copy the plain prefix, then keep accumulating
				buf = append(buf[:0], s[fieldStart:col]...)
				buffered = true
			}
			inQuotes = true
			col++
		case ',':
			fields = append(fields, take(s, buf, buffered, fieldStart, col))
			size += cur + 1
			col++
			fieldStart = col
			buffered = false
		default:
			if buffered {
				buf = append(buf, c)
			}
			col++
		}
	}
}

// take returns the current field: a slice of the line when no unquoting was
// needed, and a copy of the accumulated bytes otherwise.
func take(s string, buf []byte, buffered bool, fieldStart, col int) string {
	if buffered {
		return string(buf)
	}
	return s[fieldStart:col]
}

// isBlank reports whether a line is a section separator: empty or entirely
// spaces and tabs.
func isBlank(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' {
			return false
		}
	}
	return true
}

// hashRest reports whether the line matches ^[ \t]*# and, if so, returns the
// substring following that '#'.
func hashRest(s string) (rest string, ok bool) {
	i := skipWS(s, 0)
	if i < len(s) && s[i] == '#' {
		return s[i+1:], true
	}
	return "", false
}

func skipWS(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

// startsMarkerName reports whether rest (the text after '#') is whitespace
// followed by a non-whitespace character — the shape a marker needs.
func startsMarkerName(rest string) bool {
	i := skipWS(rest, 0)
	return i > 0 && i < len(rest)
}

// scanToken reads one token starting at i: either a quoted string (with ""
// escapes) or a run of non-whitespace characters. It returns the token's
// logical value and the index just past it.
func scanToken(s string, i int) (value string, next int) {
	if i < len(s) && s[i] == '"' {
		return scanQuoted(s, i)
	}
	start := i
	for i < len(s) && s[i] != ' ' && s[i] != '\t' {
		i++
	}
	return s[start:i], i
}

// scanQuoted reads a quoted string starting at s[i] == '"'. An unterminated
// quote consumes the rest of the line.
func scanQuoted(s string, i int) (value string, next int) {
	var buf strings.Builder
	i++ // opening quote
	for i < len(s) {
		if s[i] == '"' {
			if i+1 < len(s) && s[i+1] == '"' {
				buf.WriteByte('"')
				i += 2
				continue
			}
			return buf.String(), i + 1
		}
		buf.WriteByte(s[i])
		i++
	}
	return buf.String(), i
}
