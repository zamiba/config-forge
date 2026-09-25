package configfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// JSON, as the great majority of these programs write their settings. It is
// strict JSON: no comments, no trailing commas, no unquoted keys. A program that
// writes its config with nlohmann::json, serde_json or encoding/json emits
// exactly that, and a file carrying anything else was not written by the program
// whose settings are being edited.
//
// The root must be an object, because a value is addressed by key. Inside one,
// an object is addressable twice over — as an opaque value at its own path, and
// through its children — the same way a Lua table is, so `graphics` reads as a
// literal and `graphics.msaa` is an editable int.
//
// An array is located and reported as KindOpaque, and its elements have no paths
// of their own. Ship of Harkinian's window size and Banjo's controller bindings
// are arrays, and addressing into one by index would let a schema written against
// one release quietly rewrite the wrong slot in the next. A host shows such a
// value and leaves it to the game. null is opaque for the same reason: the
// program chose to record the absence of a value, and which kind belongs there
// instead is not this package's guess to make.

type jsonDoc struct {
	buf     []byte
	entries []jsonEntry
	// The root object's span. It has no path of its own but is the container a
	// top-level key is added to.
	rootStart, rootEnd int
}

type jsonEntry struct {
	p          Path
	start, end int
}

type jsonParser struct {
	s   string
	pos int
	out []jsonEntry
}

func parseJSON(data []byte) (Doc, error) {
	p := &jsonParser{s: string(data)}
	// A BOM is not part of JSON, but an editor may have left one; it is skipped
	// for reading and carried through untouched on write.
	p.pos = len(bom(p.s))
	p.skip()
	if p.peek() != '{' {
		return nil, fmt.Errorf("a json config file's root is an object")
	}
	rootStart := p.pos
	if err := p.value(Path{}); err != nil {
		return nil, err
	}
	rootEnd := p.pos
	p.skip()
	if p.pos < len(p.s) {
		return nil, fmt.Errorf("unexpected content at byte %d", p.pos)
	}
	return &jsonDoc{
		buf: append([]byte(nil), data...), entries: p.out,
		rootStart: rootStart, rootEnd: rootEnd,
	}, nil
}

func bom(s string) string {
	if strings.HasPrefix(s, "\ufeff") {
		return "\ufeff"
	}
	return ""
}

func (p *jsonParser) peek() byte {
	if p.pos >= len(p.s) {
		return 0
	}
	return p.s[p.pos]
}

func (p *jsonParser) skip() {
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ' ', '\t', '\r', '\n':
			p.pos++
		default:
			return
		}
	}
}

// value parses one value at the current position and records the span of it and
// of every value nested under it. path is the value's own path; the root's is
// empty, and only a path with a key in it is recorded.
func (p *jsonParser) value(path Path) error {
	start := p.pos
	if err := p.parse(path); err != nil {
		return err
	}
	if len(path) > 0 {
		p.out = append(p.out, jsonEntry{p: path, start: start, end: p.pos})
	}
	return nil
}

func (p *jsonParser) parse(path Path) error {
	switch c := p.peek(); {
	case c == '{':
		return p.object(path)
	case c == '[':
		return p.array()
	case c == '"':
		return p.jsonString()
	case c == 't' || c == 'f' || c == 'n':
		return p.word()
	default:
		return p.number()
	}
}

// skipValue parses one value and records nothing, which is how an array's
// elements are handled: this package addresses values by key, so an element has
// no path, and an object nested inside an array is not addressable either.
func (p *jsonParser) skipValue() error { return p.parse(nil) }

func (p *jsonParser) object(path Path) error {
	p.pos++ // {
	p.skip()
	if p.peek() == '}' {
		p.pos++
		return nil
	}
	for {
		p.skip()
		if p.peek() != '"' {
			return fmt.Errorf("byte %d: expected a quoted key", p.pos)
		}
		start := p.pos
		if err := p.jsonString(); err != nil {
			return err
		}
		key, ok := jsonUnquote(p.s[start:p.pos])
		if !ok {
			return fmt.Errorf("byte %d: a key this package cannot reproduce", start)
		}
		p.skip()
		if p.peek() != ':' {
			return fmt.Errorf("byte %d: expected `:` after a key", p.pos)
		}
		p.pos++
		p.skip()
		if path == nil {
			// Inside an array, so this member is not addressable.
			if err := p.skipValue(); err != nil {
				return err
			}
		} else {
			child := make(Path, 0, len(path)+1)
			child = append(append(child, path...), key)
			if err := p.value(child); err != nil {
				return err
			}
		}
		p.skip()
		switch p.peek() {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return nil
		default:
			return fmt.Errorf("byte %d: expected `,` or `}`", p.pos)
		}
	}
}

func (p *jsonParser) array() error {
	p.pos++ // [
	p.skip()
	if p.peek() == ']' {
		p.pos++
		return nil
	}
	for {
		p.skip()
		if err := p.skipValue(); err != nil {
			return err
		}
		p.skip()
		switch p.peek() {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return nil
		default:
			return fmt.Errorf("byte %d: expected `,` or `]`", p.pos)
		}
	}
}

func (p *jsonParser) jsonString() error {
	p.pos++ // opening quote
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case '\\':
			p.pos += 2
			continue
		case '"':
			p.pos++
			return nil
		}
		p.pos++
	}
	return fmt.Errorf("unterminated string")
}

func (p *jsonParser) word() error {
	start := p.pos
	for p.pos < len(p.s) && p.s[p.pos] >= 'a' && p.s[p.pos] <= 'z' {
		p.pos++
	}
	switch p.s[start:p.pos] {
	case "true", "false", "null":
		return nil
	}
	return fmt.Errorf("byte %d: not a literal json allows", start)
}

func (p *jsonParser) number() error {
	start := p.pos
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' || c >= '0' && c <= '9' {
			p.pos++
			continue
		}
		break
	}
	if p.pos == start {
		return fmt.Errorf("byte %d: expected a value", p.pos)
	}
	if _, err := strconv.ParseFloat(p.s[start:p.pos], 64); err != nil {
		return fmt.Errorf("byte %d: %q is not a number", start, p.s[start:p.pos])
	}
	return nil
}

// find returns the last entry at a path. A duplicate key is not something a
// program's own writer emits, but a parser that reads one lets the later member
// win, so that is the one edited here too.
func (d *jsonDoc) find(p Path) int {
	found := -1
	for i, e := range d.entries {
		if e.p.equal(p) {
			found = i
		}
	}
	return found
}

func (d *jsonDoc) Get(p Path) (Value, bool) {
	i := d.find(p)
	if i < 0 {
		return Value{}, false
	}
	e := d.entries[i]
	return jsonValue(string(d.buf[e.start:e.end])), true
}

func (d *jsonDoc) Paths() []Path {
	out := make([]Path, 0, len(d.entries))
	for _, e := range d.entries {
		out = append(out, e.p)
	}
	return out
}

func (d *jsonDoc) Bytes() []byte { return append([]byte(nil), d.buf...) }

func (d *jsonDoc) Set(p Path, v Value) error {
	i := d.find(p)
	if i < 0 {
		return fmt.Errorf("%w: %s", ErrNoSuchPath, p)
	}
	e := d.entries[i]
	if have := jsonValue(string(d.buf[e.start:e.end])); have.Kind != v.Kind {
		return fmt.Errorf("%s: holds %v, not %v: %w", p, have.Kind, v.Kind, ErrKindMismatch)
	}
	return d.write(i, p, v)
}

// SetNumber rewrites a number in the spelling the value carries. JSON has one
// number type, so the file's own `1` and `1.0` are the same value and either may
// replace the other; which of them a program wants is the schema's business.
func (d *jsonDoc) SetNumber(p Path, v Value) error {
	i := d.find(p)
	if i < 0 {
		return fmt.Errorf("%w: %s", ErrNoSuchPath, p)
	}
	e := d.entries[i]
	have := jsonValue(string(d.buf[e.start:e.end]))
	if !numeric(have.Kind) || !numeric(v.Kind) {
		return fmt.Errorf("%s: holds %v, not a number: %w", p, have.Kind, ErrKindMismatch)
	}
	return d.write(i, p, v)
}

func (d *jsonDoc) write(i int, p Path, v Value) error {
	e := d.entries[i]
	lit, err := jsonLiteral(v)
	if err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	if lit == string(d.buf[e.start:e.end]) {
		return nil
	}
	buf := make([]byte, 0, len(d.buf)+len(lit))
	buf = append(buf, d.buf[:e.start]...)
	buf = append(buf, lit...)
	buf = append(buf, d.buf[e.end:]...)
	d.buf = buf

	delta := len(lit) - (e.end - e.start)
	d.entries[i].end = e.start + len(lit)
	for j := range d.entries {
		if j == i {
			continue
		}
		// An enclosing object brackets the edited value, so its end moves while
		// its start does not; a sibling after it moves whole.
		if d.entries[j].start > e.start {
			d.entries[j].start += delta
			d.entries[j].end += delta
		} else if d.entries[j].end >= e.end {
			d.entries[j].end += delta
		}
	}
	// The root encloses every edit, so its end moves too. Without this, Create
	// would measure the closing brace from a stale offset after any Set.
	if d.rootStart > e.start {
		d.rootStart += delta
	}
	if d.rootEnd >= e.end {
		d.rootEnd += delta
	}
	return nil
}

// container returns the span of the object a path would be added to, and whether
// it is there. A path of one element belongs to the root object.
func (d *jsonDoc) container(p Path) (int, int, bool) {
	if len(p) <= 1 {
		return d.rootStart, d.rootEnd, d.rootEnd > d.rootStart
	}
	i := d.find(p[:len(p)-1])
	if i < 0 {
		return 0, 0, false
	}
	e := d.entries[i]
	if e.end-e.start < 2 || d.buf[e.start] != '{' {
		// The parent is there but is not an object, so nothing can be added under
		// it without replacing what it is.
		return 0, 0, false
	}
	return e.start, e.end, true
}

func (d *jsonDoc) CanCreate(p Path) bool {
	if len(p) == 0 || p[len(p)-1] == "" || d.find(p) >= 0 {
		return false
	}
	_, _, ok := d.container(p)
	return ok
}

func (d *jsonDoc) Create(p Path, v Value) error {
	lit, err := jsonLiteral(v)
	if err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	return d.createLiteral(p, lit)
}

// CreateContainer adds an empty object, which a leaf can then be added inside.
func (d *jsonDoc) CreateContainer(p Path) error { return d.createLiteral(p, "{}") }

func (d *jsonDoc) createLiteral(p Path, lit string) error {
	if len(p) == 0 || p[len(p)-1] == "" {
		return fmt.Errorf("%w: %s", ErrNoSuchPath, p)
	}
	if d.find(p) >= 0 {
		return fmt.Errorf("%w: %s", ErrAlreadyPresent, p)
	}
	start, end, ok := d.container(p)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoContainer, Path(p[:len(p)-1]))
	}
	assign := jsonQuoteKey(p[len(p)-1]) + ": " + lit

	// JSON separates members with a comma and forbids a trailing one, so the
	// anchor is the last content inside the object rather than the closing brace:
	// the new member goes after it, bringing the comma the object now needs.
	brace := end - 1
	at := brace
	for at > start+1 {
		if c := d.buf[at-1]; c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			at--
			continue
		}
		break
	}
	empty := at == start+1
	// Whether the object is written across lines is decided by what sits between
	// the last content and the brace, which covers `{}` and `{\n}` alike.
	multiline := bytes.IndexByte(d.buf[at:brace], '\n') >= 0

	var prefix, suffix string
	switch {
	case multiline && empty:
		// The newline and indent before the brace are already there, so only the
		// member's own line is added.
		prefix = "\n" + jsonLineIndent(d.buf, brace) + jsonIndentStep(d.buf)
	case multiline:
		prefix = ",\n" + jsonLineIndent(d.buf, at)
	case empty:
		prefix = " "
		if d.buf[at] == '}' {
			suffix = " "
		}
	default:
		prefix = ", "
	}
	text := prefix + assign + suffix

	buf := make([]byte, 0, len(d.buf)+len(text))
	buf = append(buf, d.buf[:at]...)
	buf = append(buf, text...)
	buf = append(buf, d.buf[at:]...)
	d.buf = buf

	delta := len(text)
	for i := range d.entries {
		switch {
		case d.entries[i].start >= at:
			d.entries[i].start += delta
			d.entries[i].end += delta
		case d.entries[i].end > at:
			// Strictly past the insertion point, so this entry encloses it. One
			// that merely ends *at* it is the preceding sibling and stays put.
			d.entries[i].end += delta
		}
	}
	if d.rootStart >= at {
		d.rootStart += delta
	}
	if d.rootEnd > at {
		d.rootEnd += delta
	}
	valStart := at + len(prefix) + len(assign) - len(lit)
	d.entries = append(d.entries, jsonEntry{p: append(Path(nil), p...), start: valStart, end: valStart + len(lit)})
	return nil
}

// jsonLineIndent returns the leading whitespace of the line the offset sits on,
// so a new member lines up with the ones already in the file however the program
// indents — tabs, two spaces or four.
func jsonLineIndent(buf []byte, at int) string {
	lineStart := at
	for lineStart > 0 && buf[lineStart-1] != '\n' {
		lineStart--
	}
	i := lineStart
	for i < len(buf) && (buf[i] == ' ' || buf[i] == '\t') {
		i++
	}
	if i > at {
		i = at
	}
	return string(buf[lineStart:i])
}

// jsonIndentStep guesses one level of indentation from the first indented line in
// the file. It is only needed to add the first member to an empty object, where
// there is no sibling to line up with.
func jsonIndentStep(buf []byte) string {
	for _, line := range bytes.Split(buf, []byte("\n")) {
		i := 0
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		if i > 0 && i < len(line) {
			return string(line[:i])
		}
	}
	return "  "
}

func jsonQuoteKey(k string) string {
	lit, err := jsonLiteral(Value{Kind: KindString, Str: k})
	if err != nil {
		// A key is a string, and a string that cannot be written is not a key
		// this package would have parsed in the first place.
		return `""`
	}
	return lit
}

func jsonValue(lit string) Value {
	switch lit {
	case "true":
		return Value{Kind: KindBool, Bool: true, Raw: lit}
	case "false":
		return Value{Kind: KindBool, Bool: false, Raw: lit}
	}
	if strings.HasPrefix(lit, `"`) {
		if s, ok := jsonUnquote(lit); ok {
			return Value{Kind: KindString, Str: s, Raw: lit}
		}
		return Value{Raw: lit}
	}
	if i, err := strconv.ParseInt(lit, 10, 64); err == nil {
		return Value{Kind: KindInt, Int: i, Raw: lit}
	}
	if f, err := strconv.ParseFloat(lit, 64); err == nil {
		return Value{Kind: KindFloat, Float: f, Raw: lit}
	}
	// An object, an array or null.
	return Value{Raw: lit}
}

// jsonUnquote decodes one string literal. JSON's escapes include surrogate pairs,
// which is the one place a hand-rolled decoder here would be a liability, so the
// standard library decodes the literal. That is the whole of the use: the file's
// structure is still found by span, never by unmarshalling it.
func jsonUnquote(lit string) (string, bool) {
	var s string
	if err := json.Unmarshal([]byte(lit), &s); err != nil {
		return "", false
	}
	return s, true
}

func jsonLiteral(v Value) (string, error) {
	switch v.Kind {
	case KindBool:
		return strconv.FormatBool(v.Bool), nil
	case KindInt:
		return strconv.FormatInt(v.Int, 10), nil
	case KindFloat:
		if math.IsInf(v.Float, 0) || math.IsNaN(v.Float) {
			return "", ErrUnsupportedValue
		}
		s := strconv.FormatFloat(v.Float, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eE") {
			// JSON has one number type, but the program reading the file does
			// not: a field the schema calls a float keeps its point, so it reads
			// back as the same kind it was written as.
			s += ".0"
		}
		return s, nil
	case KindString:
		if !utf8.ValidString(v.Str) {
			return "", ErrUnsupportedValue
		}
		var b strings.Builder
		b.WriteByte('"')
		for i := 0; i < len(v.Str); i++ {
			switch c := v.Str[i]; {
			case c == '"':
				b.WriteString(`\"`)
			case c == '\\':
				b.WriteString(`\\`)
			case c == '\n':
				b.WriteString(`\n`)
			case c == '\r':
				b.WriteString(`\r`)
			case c == '\t':
				b.WriteString(`\t`)
			case c == '\b':
				b.WriteString(`\b`)
			case c == '\f':
				b.WriteString(`\f`)
			case c < 0x20:
				b.WriteString(`\u00`)
				const hex = "0123456789abcdef"
				b.WriteByte(hex[c>>4])
				b.WriteByte(hex[c&0xf])
			default:
				// Text above the control range passes through as the UTF-8 the
				// program itself writes, rather than as \u escapes.
				b.WriteByte(c)
			}
		}
		b.WriteByte('"')
		return b.String(), nil
	}
	return "", ErrUnsupportedValue
}

// jsonDoc and iniDoc are NumberSetters; the typed grammars deliberately are not.
var (
	_ NumberSetter = (*jsonDoc)(nil)
	_ NumberSetter = (*iniDoc)(nil)
	_ NumberSetter = (*spaceSepDoc)(nil)
)
