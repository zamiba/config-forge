package configfile

import (
	"fmt"
	"strconv"
	"strings"
)

// TOML, in the subset a settings file is written in: `key = value` lines under
// optional [table] headers, with # comments.
//
// It is not a general TOML parser and does not try to be. What it guarantees is
// the same thing every grammar here guarantees — that it knows exactly where a
// value begins and ends — and for TOML that means measuring the constructs it
// does not interpret as carefully as the ones it does. An array, an inline
// table, a multi-line string and a date are all located, spanned across however
// many lines they occupy, reported as KindOpaque, and refused by Set. Getting
// their extent wrong would be worse than not reading them: a line-oriented
// parser that mistook the middle of a multi-line array for a key would write a
// setting into the middle of someone's list.
//
// An array of tables, [[thing]], is refused outright, and the whole file with
// it. Every other construct here has one place a given path can be; a repeated
// table does not, and a schema pointing into one would be addressing whichever
// copy happened to come first.
//
// Two programs here write this format, both through the same rex SDK, and both
// write it flat: rex::cvar::SerializeToTOML emits no headers at all, and only
// the flags that differ from their defaults, so the file is a short list of what
// somebody changed. Headers are read anyway, because the next program to keep a
// TOML config will use them.

type tomlDoc struct {
	buf     []byte
	entries []tomlEntry
	// tableEnd is the offset a new key for each table is inserted at, keyed by
	// the table's dotted path — "" for the headerless top of the file — so a new
	// key joins the table it belongs to instead of landing at the end.
	tableEnd map[string]int
}

type tomlEntry struct {
	p Path
	// start and end bracket the value, which for an array or a multi-line string
	// may run well past the end of its own line.
	start, end int
}

func parseTOML(data []byte) (Doc, error) {
	d := &tomlDoc{buf: append([]byte(nil), data...), tableEnd: map[string]int{}}
	s := string(data)
	var table Path
	tableKey := ""
	d.tableEnd[""] = 0

	for i := 0; i < len(s); {
		lineEnd := lineEndAt(s, i)
		trimmed := strings.TrimSpace(s[i:lineEnd])

		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			// Blank or comment: carried through untouched.
		case strings.HasPrefix(trimmed, "[["):
			return nil, fmt.Errorf("byte %d: an array of tables has no single place for a path to address", i)
		case strings.HasPrefix(trimmed, "["):
			if !strings.HasSuffix(trimmed, "]") {
				return nil, fmt.Errorf("byte %d: unterminated table header", i)
			}
			p, err := parseTOMLKey(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
			if err != nil {
				return nil, fmt.Errorf("byte %d: %w", i, err)
			}
			table, tableKey = p, p.String()
			d.tableEnd[tableKey] = lineEnd
		default:
			eq := strings.IndexByte(s[i:lineEnd], '=')
			if eq < 0 {
				return nil, fmt.Errorf("byte %d: not a comment, a header or an assignment", i)
			}
			key, err := parseTOMLKey(strings.TrimSpace(s[i : i+eq]))
			if err != nil {
				return nil, fmt.Errorf("byte %d: %w", i, err)
			}
			start, end, err := tomlValueSpan(s, i+eq+1)
			if err != nil {
				return nil, err
			}
			full := make(Path, 0, len(table)+len(key))
			full = append(append(full, table...), key...)
			d.entries = append(d.entries, tomlEntry{p: full, start: start, end: end})
			// The value may have run past this line; the table's insertion point
			// is the end of the line the value finishes on.
			lineEnd = lineEndAt(s, end)
			d.tableEnd[tableKey] = lineEnd
		}

		if lineEnd >= len(s) {
			break
		}
		i = lineEnd + 1
	}
	return d, nil
}

func lineEndAt(s string, i int) int {
	if nl := strings.IndexByte(s[i:], '\n'); nl >= 0 {
		return i + nl
	}
	return len(s)
}

// parseTOMLKey reads a key or a table header: bare or quoted parts separated by
// dots. A quoted part may itself contain a dot, which is why this returns a Path
// rather than a string to be split later.
func parseTOMLKey(s string) (Path, error) {
	var out Path
	for {
		s = strings.TrimLeft(s, " \t")
		if s == "" {
			return nil, fmt.Errorf("expected a key")
		}
		var part string
		switch s[0] {
		case '"', '\'':
			end := tomlStringEnd(s, 0)
			if end < 0 {
				return nil, fmt.Errorf("unterminated quoted key")
			}
			v := tomlValue(s[:end])
			if v.Kind != KindString {
				return nil, fmt.Errorf("a quoted key this package cannot reproduce")
			}
			part, s = v.Str, s[end:]
		default:
			n := 0
			for n < len(s) {
				c := s[n]
				if c == '_' || c == '-' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
					n++
					continue
				}
				break
			}
			if n == 0 {
				return nil, fmt.Errorf("%q is not a key", s)
			}
			part, s = s[:n], s[n:]
		}
		out = append(out, part)
		s = strings.TrimLeft(s, " \t")
		if s == "" {
			return out, nil
		}
		if s[0] != '.' {
			return nil, fmt.Errorf("unexpected %q after a key", s)
		}
		s = s[1:]
	}
}

// tomlStringEnd returns the offset just past the string literal starting at i,
// or -1 if it is unterminated. It handles all four of TOML's string forms.
func tomlStringEnd(s string, i int) int {
	switch {
	case strings.HasPrefix(s[i:], `"""`):
		if n := strings.Index(s[i+3:], `"""`); n >= 0 {
			return i + 3 + n + 3
		}
		return -1
	case strings.HasPrefix(s[i:], "'''"):
		if n := strings.Index(s[i+3:], "'''"); n >= 0 {
			return i + 3 + n + 3
		}
		return -1
	case s[i] == '\'':
		// A literal string has no escapes at all, and cannot span lines.
		for j := i + 1; j < len(s) && s[j] != '\n'; j++ {
			if s[j] == '\'' {
				return j + 1
			}
		}
		return -1
	case s[i] == '"':
		for j := i + 1; j < len(s) && s[j] != '\n'; j++ {
			if s[j] == '\\' {
				j++
				continue
			}
			if s[j] == '"' {
				return j + 1
			}
		}
		return -1
	}
	return -1
}

// tomlValueSpan brackets the value that starts at or after from. A bracketed
// value or a multi-line string may run past the end of its line, and getting
// that extent right is what keeps a later edit out of the middle of it.
func tomlValueSpan(s string, from int) (int, int, error) {
	i := from
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	if i >= len(s) || s[i] == '\n' || s[i] == '#' {
		return 0, 0, fmt.Errorf("byte %d: an assignment has no value", from)
	}
	switch s[i] {
	case '"', '\'':
		end := tomlStringEnd(s, i)
		if end < 0 {
			return 0, 0, fmt.Errorf("byte %d: unterminated string", i)
		}
		return i, end, nil
	case '[', '{':
		end, err := tomlBalance(s, i)
		if err != nil {
			return 0, 0, err
		}
		return i, end, nil
	}
	// A scalar runs to the end of the line, less any trailing comment. A bare
	// scalar cannot contain a #, so the first one ends the value.
	end := lineEndAt(s, i)
	if h := strings.IndexByte(s[i:end], '#'); h >= 0 {
		end = i + h
	}
	for end > i && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return i, end, nil
}

// tomlBalance spans an array or an inline table, skipping over strings and
// comments so a bracket inside either does not count.
func tomlBalance(s string, i int) (int, error) {
	open, close := s[i], byte(']')
	if open == '{' {
		close = '}'
	}
	depth := 0
	for j := i; j < len(s); j++ {
		switch c := s[j]; {
		case c == '"' || c == '\'':
			end := tomlStringEnd(s, j)
			if end < 0 {
				return 0, fmt.Errorf("byte %d: unterminated string inside a %c", j, open)
			}
			j = end - 1
		case c == '#':
			// A comment inside a multi-line array runs to the end of its line.
			j = lineEndAt(s, j)
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
			if depth == 0 {
				if c != close {
					return 0, fmt.Errorf("byte %d: a %c closed by a %c", i, open, c)
				}
				return j + 1, nil
			}
		}
	}
	return 0, fmt.Errorf("byte %d: unterminated %c", i, open)
}

func (d *tomlDoc) find(p Path) int {
	for i, e := range d.entries {
		if e.p.equal(p) {
			return i
		}
	}
	return -1
}

func (d *tomlDoc) Get(p Path) (Value, bool) {
	i := d.find(p)
	if i < 0 {
		return Value{}, false
	}
	e := d.entries[i]
	return tomlValue(string(d.buf[e.start:e.end])), true
}

func (d *tomlDoc) Paths() []Path {
	out := make([]Path, 0, len(d.entries))
	for _, e := range d.entries {
		out = append(out, e.p)
	}
	return out
}

func (d *tomlDoc) Bytes() []byte { return append([]byte(nil), d.buf...) }

func (d *tomlDoc) Set(p Path, v Value) error {
	i := d.find(p)
	if i < 0 {
		return fmt.Errorf("%w: %s", ErrNoSuchPath, p)
	}
	e := d.entries[i]
	old := string(d.buf[e.start:e.end])
	if have := tomlValue(old); have.Kind != v.Kind {
		return fmt.Errorf("%s: holds %v, not %v: %w", p, have.Kind, v.Kind, ErrKindMismatch)
	}
	lit, err := tomlLiteral(v, old)
	if err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	if lit == old {
		return nil
	}
	d.splice(e.start, e.end, lit)
	d.entries[i].end = e.start + len(lit)
	return nil
}

// splice replaces the bytes between start and end and moves every offset after
// them. Nothing addressable nests in this grammar — an array's contents have no
// paths — so an offset either precedes the edit or follows it.
func (d *tomlDoc) splice(start, end int, text string) {
	buf := make([]byte, 0, len(d.buf)+len(text))
	buf = append(buf, d.buf[:start]...)
	buf = append(buf, text...)
	buf = append(buf, d.buf[end:]...)
	d.buf = buf

	delta := len(text) - (end - start)
	for i := range d.entries {
		if d.entries[i].start >= end {
			d.entries[i].start += delta
			d.entries[i].end += delta
		}
	}
	for table, at := range d.tableEnd {
		if at >= end {
			d.tableEnd[table] = at + delta
		}
	}
}

// container returns the table a path would be added to, and whether the file has
// it. A path of one element belongs to the headerless top of the file, which
// every file has; a longer one belongs to the table named by all but its last
// element. A dotted key is not considered: this package adds a key to a table
// that is already there, and `a.b = 1` under no header would be declaring the
// table a as well.
func (d *tomlDoc) container(p Path) (string, bool) {
	if len(p) == 1 {
		return "", true
	}
	key := Path(p[:len(p)-1]).String()
	_, ok := d.tableEnd[key]
	return key, ok
}

func (d *tomlDoc) CanCreate(p Path) bool {
	if len(p) == 0 || p[len(p)-1] == "" || d.find(p) >= 0 {
		return false
	}
	_, ok := d.container(p)
	return ok
}

// CreateContainer adds a table header, which a key can then be added under. A
// dotted header declares every level above it at once, which is why a path of more
// than one element is allowed here where Create's is not.
func (d *tomlDoc) CreateContainer(p Path) error {
	if len(p) == 0 || p[len(p)-1] == "" {
		return fmt.Errorf("%w: %s", ErrNoContainer, p)
	}
	key := p.String()
	if _, have := d.tableEnd[key]; have {
		return fmt.Errorf("%w: %s", ErrAlreadyPresent, p)
	}
	parts := make([]string, len(p))
	for i, k := range p {
		parts[i] = tomlKey(k)
	}
	text := "\n[" + strings.Join(parts, ".") + "]\n"
	if len(d.buf) == 0 {
		text = "[" + strings.Join(parts, ".") + "]\n"
	} else if d.buf[len(d.buf)-1] != '\n' {
		text = "\n" + text
	}
	at := len(d.buf)
	d.buf = append(d.buf, text...)
	d.tableEnd[key] = at + len(text) - 1
	return nil
}

func (d *tomlDoc) Create(p Path, v Value) error {
	if len(p) == 0 || p[len(p)-1] == "" {
		return fmt.Errorf("%w: %s", ErrNoSuchPath, p)
	}
	if d.find(p) >= 0 {
		return fmt.Errorf("%w: %s", ErrAlreadyPresent, p)
	}
	table, ok := d.container(p)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoContainer, Path(p[:len(p)-1]))
	}
	lit, err := tomlLiteral(v, "")
	if err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	line := tomlKey(p[len(p)-1]) + " = " + lit

	at := d.tableEnd[table]
	if at > len(d.buf) {
		at = len(d.buf)
	}
	var text string
	var valueOffset int
	if at == 0 && len(d.buf) > 0 {
		// The headerless top of a file that begins with a header or a comment: the
		// key becomes the first line.
		text = line + "\n"
		valueOffset = len(line) - len(lit)
	} else {
		text = "\n" + line
		valueOffset = 1 + len(line) - len(lit)
		if at == 0 {
			// An empty file has no line to follow.
			text, valueOffset = line+"\n", len(line)-len(lit)
		}
	}

	d.splice(at, at, text)
	valStart := at + valueOffset
	d.entries = append(d.entries, tomlEntry{p: append(Path(nil), p...), start: valStart, end: valStart + len(lit)})
	d.tableEnd[table] = valStart + len(lit)
	return nil
}

// tomlKey renders a key: bare where the grammar allows it, quoted otherwise.
func tomlKey(k string) string {
	bare := k != ""
	for i := 0; i < len(k); i++ {
		c := k[i]
		ok := c == '_' || c == '-' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !ok {
			bare = false
			break
		}
	}
	if bare {
		return k
	}
	lit, _ := tomlLiteral(Value{Kind: KindString, Str: k}, `""`)
	return lit
}

func tomlValue(lit string) Value {
	switch lit {
	case "true":
		return Value{Kind: KindBool, Bool: true, Raw: lit}
	case "false":
		return Value{Kind: KindBool, Bool: false, Raw: lit}
	}
	if lit == "" {
		return Value{Raw: lit}
	}
	switch lit[0] {
	case '"', '\'':
		// A multi-line string is readable and not rewritable: this package would
		// have to render it back as one escaped line, which is the same value in a
		// layout the program did not choose. Opaque is what every other grammar
		// here reports for a string it cannot reproduce as it found it.
		if strings.HasPrefix(lit, `"""`) || strings.HasPrefix(lit, "'''") {
			return Value{Raw: lit}
		}
		if s, ok := tomlUnquote(lit); ok {
			return Value{Kind: KindString, Str: s, Raw: lit}
		}
		return Value{Raw: lit}
	case '[', '{':
		return Value{Raw: lit}
	}
	// A prefixed integer keeps its own spelling rather than being rewritten in
	// decimal, and inf and nan are not numbers this package will write.
	if len(lit) > 1 && (lit[0] == '0' && strings.ContainsAny(lit[1:2], "xXoObB")) {
		return Value{Raw: lit}
	}
	if strings.ContainsAny(lit, "inaIN") {
		// inf, nan, and a date's month names. A bare scalar containing a letter is
		// none of the numbers below.
		return Value{Raw: lit}
	}
	// TOML allows _ as a digit separator, which strconv does not.
	clean := strings.ReplaceAll(lit, "_", "")
	if i, err := strconv.ParseInt(clean, 10, 64); err == nil && !strings.ContainsAny(lit, ":-") {
		return Value{Kind: KindInt, Int: i, Raw: lit}
	}
	if f, err := strconv.ParseFloat(clean, 64); err == nil && !strings.ContainsAny(lit, ":") {
		return Value{Kind: KindFloat, Float: f, Raw: lit}
	}
	// A date, a time, or something this package does not recognise.
	return Value{Raw: lit}
}

// tomlUnquote decodes a string literal of any of TOML's four forms. A basic
// string's escapes are decoded; a literal string has none by definition.
func tomlUnquote(lit string) (string, bool) {
	switch {
	case strings.HasPrefix(lit, "'''") && strings.HasSuffix(lit, "'''") && len(lit) >= 6:
		return trimFirstNewline(lit[3 : len(lit)-3]), true
	case strings.HasPrefix(lit, `"""`) && strings.HasSuffix(lit, `"""`) && len(lit) >= 6:
		return tomlUnescape(trimFirstNewline(lit[3 : len(lit)-3]))
	case strings.HasPrefix(lit, "'") && strings.HasSuffix(lit, "'") && len(lit) >= 2:
		return lit[1 : len(lit)-1], true
	case strings.HasPrefix(lit, `"`) && strings.HasSuffix(lit, `"`) && len(lit) >= 2:
		return tomlUnescape(lit[1 : len(lit)-1])
	}
	return "", false
}

// A newline immediately after a multi-line string's opening delimiter is not part
// of the value.
func trimFirstNewline(s string) string {
	s = strings.TrimPrefix(s, "\r\n")
	return strings.TrimPrefix(s, "\n")
}

func tomlUnescape(body string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(body) {
			return "", false
		}
		switch e := body[i]; e {
		case '"', '\\':
			b.WriteByte(e)
		case 'b':
			b.WriteByte('\b')
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'f':
			b.WriteByte('\f')
		case 'r':
			b.WriteByte('\r')
		case 'u', 'U':
			n := 4
			if e == 'U' {
				n = 8
			}
			if i+n >= len(body) {
				return "", false
			}
			cp, err := strconv.ParseUint(body[i+1:i+1+n], 16, 32)
			if err != nil {
				return "", false
			}
			b.WriteRune(rune(cp))
			i += n
		case '\n', '\r', ' ', '\t':
			// A line-ending backslash in a multi-line string swallows the
			// whitespace that follows it.
			for i+1 < len(body) && (body[i+1] == '\n' || body[i+1] == '\r' || body[i+1] == ' ' || body[i+1] == '\t') {
				i++
			}
		default:
			return "", false
		}
	}
	return b.String(), true
}

// tomlLiteral renders a value. old is the literal being replaced, which decides
// which of TOML's string forms to write back; it is empty when the key is new.
func tomlLiteral(v Value, old string) (string, error) {
	switch v.Kind {
	case KindBool:
		return strconv.FormatBool(v.Bool), nil
	case KindInt:
		return strconv.FormatInt(v.Int, 10), nil
	case KindFloat:
		s := strconv.FormatFloat(v.Float, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eE") {
			// TOML's float and integer are different types, so a float that lost
			// its point would come back as an integer and stop matching.
			s += ".0"
		}
		return s, nil
	case KindString:
		// A literal string keeps its form where it can — it is what the file used
		// and it needs no escaping — and becomes a basic string when the value now
		// holds something a literal string cannot express.
		if strings.HasPrefix(old, "'") && !strings.HasPrefix(old, "'''") &&
			!strings.ContainsAny(v.Str, "'\n\r") {
			return "'" + v.Str + "'", nil
		}
		var b strings.Builder
		b.WriteByte('"')
		for _, r := range v.Str {
			switch r {
			case '"':
				b.WriteString(`\"`)
			case '\\':
				b.WriteString(`\\`)
			case '\b':
				b.WriteString(`\b`)
			case '\t':
				b.WriteString(`\t`)
			case '\n':
				b.WriteString(`\n`)
			case '\f':
				b.WriteString(`\f`)
			case '\r':
				b.WriteString(`\r`)
			default:
				if r < 0x20 || r == 0x7f {
					b.WriteString(fmt.Sprintf(`\u%04X`, r))
					continue
				}
				b.WriteRune(r)
			}
		}
		b.WriteByte('"')
		return b.String(), nil
	}
	return "", ErrUnsupportedValue
}
