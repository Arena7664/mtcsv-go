package mtcsv

import (
	"fmt"
	"strings"
)

// Severity classifies a Diagnostic. Only SeverityError conditions make a
// document invalid; warnings and hints are advisory.
type Severity int

const (
	SeverityError Severity = iota
	SeverityWarning
	SeverityHint
)

func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	case SeverityHint:
		return "hint"
	}
	return "unknown"
}

// Diagnostic codes.
const (
	// DiagTooManyFields reports a data row with more fields than the header
	// defines. The surplus cells are retained on the row.
	DiagTooManyFields = "too-many-fields"
	// DiagUnterminatedQuote reports a quoted field still open at end of file.
	DiagUnterminatedQuote = "unterminated-quote"
	// DiagUnknownColumn reports a '#:' comment naming a column that the header
	// does not contain.
	DiagUnknownColumn = "unknown-column"
	// DiagDuplicateColumn reports two or more header columns sharing a name.
	DiagDuplicateColumn = "duplicate-column"
	// DiagNoHeader reports a section with structural lines but no record.
	DiagNoHeader = "no-header"
	// DiagRaggedShortRow reports a legal, padded short row. It is opt-in via
	// ParseOptions.Hints.
	DiagRaggedShortRow = "ragged-short-row"
	// DiagRecordTooLarge reports a record abandoned because it exceeded
	// ParseOptions.MaxRecordBytes. This one's our own addition, not in the
	// spec.
	DiagRecordTooLarge = "record-too-large"
)

// A Diagnostic is a condition detected while reading a document.
type Diagnostic struct {
	Code     string
	Severity Severity
	Message  string
	// Line is the 1-based physical line the diagnostic refers to.
	Line int
	// Table is the name or position of the table involved, if any.
	Table string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("line %d: %s: %s (%s)", d.Line, d.Severity, d.Message, d.Code)
}

// A DiagnosticError reports one or more error-severity diagnostics found while
// parsing. The document is still usable; see Document.Err.
type DiagnosticError struct {
	Diagnostics []Diagnostic
}

func (e *DiagnosticError) Error() string {
	if len(e.Diagnostics) == 1 {
		return "mtcsv: " + e.Diagnostics[0].String()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "mtcsv: %d errors:", len(e.Diagnostics))
	for _, d := range e.Diagnostics {
		b.WriteString("\n\t")
		b.WriteString(d.String())
	}
	return b.String()
}
