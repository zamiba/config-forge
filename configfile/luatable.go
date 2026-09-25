package configfile

import (
	"fmt"
	"strconv"
	"strings"
)

// A Lua table written as a data file: `return { key = value, … }`.
//
// This is not general Lua and deliberately cannot be. The grammar is the one a
// program's own deterministic writer emits — keyed tables, numbers, booleans and
// quoted strings, nothing else — which is what makes a value's position in the
// file knowable. Gen1Recomp's SaveSerializer says as much about its own reader:
// the writer is the grammar's specification, so a tampered file fails to parse
// rather than executing. A file with a function call, a concatenation, an
// arithmetic expression or a bare sequence entry is refused here for the same
// reason: this package cannot say where such a value begins and ends, and will
// not guess.
//
// A nested table is addressable twice over: as an opaque value at its own path,
// and through its children. So touchControls reads as a literal and
// touchControls.enabled is an editable bool.

type luaDoc struct {
	buf     []byte
	entries []luaEntry
	// The root table's span, which has no path of its own but is the container a
	// top-level key is added to.
	rootStart, rootEnd int
}

type luaEntry struct {
	p          Path
	start, end int
}

type luaParser struct {
	s   string
	pos int
	out []luaEntry
}

func parseLuaTable(data []byte) (Doc, error) {
	p := &luaParser{s: string(data)}
	p.skip()
	if !strings.HasPrefix(p.s[p.pos:], "return") {
		return nil, fmt.Errorf("a lua data file begins with `return`")
	}
	p.pos += len("return")
	p.skip()
	if p.peek() != '{' {
		return nil, fmt.Errorf("`return` must be followed by a table")
	}
	rootStart := p.pos
	if err := p.value(nil); err != nil {
		return nil, err
	}
	rootEnd := p.pos
	p.skip()
	if p.pos < len(p.s) {
		return nil, fmt.Errorf("unexpected content at byte %d", p.pos)
	}
	return &luaDoc{
		buf: append([]byte(nil), data...), entries: p.out,
		rootStart: rootStart, rootEnd: rootEnd,
	}, nil
}

func (p *luaParser) peek() byte {
	if p.pos >= len(p.s) {
		return 0
	}
	return p.s[p.pos]
}

// skip advances over whitespace and -- line comments. Comments are not part of
// what the writer emits, but a hand-edited file may carry them and they are
// carried through untouched.
func (p *luaParser) skip() {
	for p.pos < len(p.s) {
		switch c := p.s[p.pos]; {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			p.pos++
		case strings.HasPrefix(p.s[p.pos:], "--"):
			nl := strings.IndexByte(p.s[p.pos:], '\n')
			if nl < 0 {
				p.pos = len(p.s)
				return
			}
			p.pos += nl + 1
		default:
			return
		}
	}
}

// value parses one value at the current position and records its span under
// path. Returns after the value, before any separator.
func (p *luaParser) value(path Path) error {
	start := p.pos
	switch c := p.peek(); {
	case c == '{':
		if err := p.table(path); err != nil {
			return err
		}
	case c == '"':
		if err := p.luaString(); err != nil {
			return err
		}
	default:
		if !p.scalar() {
			return fmt.Errorf("byte %d: not a literal this grammar allows", p.pos)
		}
	}
	if path != nil {
		p.out = append(p.out, luaEntry{p: path, start: start, end: p.pos})
	}
	return nil
}

func (p *luaParser) table(path Path) error {
	p.pos++ // {
	for {
		p.skip()
		switch p.peek() {
		case '}':
			p.pos++
			return nil
		case 0:
			return fmt.Errorf("byte %d: unterminated table", p.pos)
		}
		key, err := p.key()
		if err != nil {
			return err
		}
		p.skip()
		if p.peek() != '=' {
			return fmt.Errorf("byte %d: every entry is `key = value`; a bare sequence entry is not this grammar", p.pos)
		}
		p.pos++
		p.skip()
		child := make(Path, 0, len(path)+1)
		child = append(append(child, path...), key)
		if err := p.value(child); err != nil {
			return err
		}
		p.skip()
		if p.peek() == ',' || p.peek() == ';' {
			p.pos++
		}
	}
}

// key reads a bare identifier or a bracketed literal key.
func (p *luaParser) key() (string, error) {
	if p.peek() == '[' {
		p.pos++
		p.skip()
		start := p.pos
		if p.peek() == '"' {
			if err := p.luaString(); err != nil {
				return "", err
			}
			lit := p.s[start:p.pos]
			s, ok := luaUnquote(lit)
			if !ok {
				return "", fmt.Errorf("byte %d: a key this package cannot reproduce", start)
			}
			p.skip()
			if p.peek() != ']' {
				return "", fmt.Errorf("byte %d: unterminated key", p.pos)
			}
			p.pos++
			return s, nil
		}
		if !p.scalar() {
			return "", fmt.Errorf("byte %d: a bracketed key must be a string or a number", p.pos)
		}
		lit := p.s[start:p.pos]
		p.skip()
		if p.peek() != ']' {
			return "", fmt.Errorf("byte %d: unterminated key", p.pos)
		}
		p.pos++
		return lit, nil
	}
	start := p.pos
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
			(p.pos > start && c >= '0' && c <= '9') {
			p.pos++
			continue
		}
		break
	}
	if p.pos == start {
		return "", fmt.Errorf("byte %d: expected a key", p.pos)
	}
	return p.s[start:p.pos], nil
}

// scalar consumes a number, true, false or nil.
func (p *luaParser) scalar() bool {
	start := p.pos
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if c == '_' || c == '.' || c == '-' || c == '+' ||
			c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			p.pos++
			continue
		}
		break
	}
	return p.pos > start
}

func (p *luaParser) luaString() error {
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

func (d *luaDoc) find(p Path) int {
	for i, e := range d.entries {
		if e.p.equal(p) {
			return i
		}
	}
	return -1
}

func (d *luaDoc) Get(p Path) (Value, bool) {
	i := d.find(p)
	if i < 0 {
		return Value{}, false
	}
	e := d.entries[i]
	return luaValue(string(d.buf[e.start:e.end])), true
}

func (d *luaDoc) Paths() []Path {
	out := make([]Path, 0, len(d.entries))
	for _, e := range d.entries {
		out = append(out, e.p)
	}
	return out
}

func (d *luaDoc) Bytes() []byte { return append([]byte(nil), d.buf...) }

func (d *luaDoc) Set(p Path, v Value) error {
	i := d.find(p)
	if i < 0 {
		return fmt.Errorf("%w: %s", ErrNoSuchPath, p)
	}
	e := d.entries[i]
	if have := luaValue(string(d.buf[e.start:e.end])); have.Kind != v.Kind {
		return fmt.Errorf("%s: holds %v, not %v: %w", p, have.Kind, v.Kind, ErrKindMismatch)
	}
	lit, err := luaLiteral(v)
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
		// A parent table encloses the edited value, so its end moves while its
		// start does not; a sibling after it moves whole.
		if d.entries[j].start > e.start {
			d.entries[j].start += delta
			d.entries[j].end += delta
		} else if d.entries[j].end >= e.end {
			d.entries[j].end += delta
		}
	}
	// The root table encloses every edit, so its end moves too. Missing this left
	// the root's span stale after any Set, and Create then measured the closing
	// brace from the wrong offset and added the key outside the table.
	if d.rootStart > e.start {
		d.rootStart += delta
	}
	if d.rootEnd >= e.end {
		d.rootEnd += delta
	}
	return nil
}

// container returns the span of the table a path would be added to, and whether
// it is there. An empty parent path is the root table.
func (d *luaDoc) container(p Path) (int, int, bool) {
	if len(p) <= 1 {
		return d.rootStart, d.rootEnd, d.rootEnd > d.rootStart
	}
	i := d.find(p[:len(p)-1])
	if i < 0 {
		return 0, 0, false
	}
	e := d.entries[i]
	if e.end-e.start < 2 || d.buf[e.start] != '{' {
		// The parent is there but is not a table, so nothing can be added under
		// it without replacing what it is.
		return 0, 0, false
	}
	return e.start, e.end, true
}

func (d *luaDoc) CanCreate(p Path) bool {
	if len(p) == 0 || p[len(p)-1] == "" || d.find(p) >= 0 {
		return false
	}
	_, _, ok := d.container(p)
	return ok
}

func (d *luaDoc) Create(p Path, v Value) error {
	lit, err := luaLiteral(v)
	if err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	return d.createLiteral(p, lit)
}

// CreateContainer adds an empty table, which a key can then be added inside.
func (d *luaDoc) CreateContainer(p Path) error { return d.createLiteral(p, "{}") }

func (d *luaDoc) createLiteral(p Path, lit string) error {
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
	assign := luaKey(p[len(p)-1]) + " = " + lit

	// The closing brace is the anchor. When it sits on its own line the entry is
	// added as a line above it, matching the writer's own layout; when the table
	// is written inline it is added inline too, rather than reformatting a table
	// the program wrote.
	brace := end - 1
	lineStart := brace
	for lineStart > start && d.buf[lineStart-1] != '\n' {
		lineStart--
	}
	onlyIndent := true
	for i := lineStart; i < brace; i++ {
		if d.buf[i] != ' ' && d.buf[i] != '\t' {
			onlyIndent = false
			break
		}
	}

	var text string
	var valueOffset int
	if onlyIndent {
		indent := string(d.buf[lineStart:brace])
		text = indent + "  " + assign + ",\n"
		valueOffset = len(indent) + 2 + len(assign) - len(lit)
		brace = lineStart
	} else {
		// Inline: insert after the last content rather than against the brace, so
		// the spacing the program wrote is left as it is instead of gaining a gap
		// before the comma.
		insertAt := end - 1
		for insertAt > start+1 {
			if c := d.buf[insertAt-1]; c == ' ' || c == '\t' {
				insertAt--
				continue
			}
			break
		}
		if insertAt == start+1 {
			// An empty table has nothing to follow, so the entry brings its own
			// spacing on both sides.
			text = " " + assign + " "
			valueOffset = 1 + len(assign) - len(lit)
		} else {
			text = ", " + assign
			valueOffset = 2 + len(assign) - len(lit)
		}
		brace = insertAt
	}

	buf := make([]byte, 0, len(d.buf)+len(text))
	buf = append(buf, d.buf[:brace]...)
	buf = append(buf, text...)
	buf = append(buf, d.buf[brace:]...)
	d.buf = buf

	delta := len(text)
	for i := range d.entries {
		switch {
		case d.entries[i].start >= brace:
			d.entries[i].start += delta
			d.entries[i].end += delta
		case d.entries[i].end > brace:
			// Strictly past the insertion point, so this entry encloses it. One
			// that merely ends *at* it is the preceding sibling and stays put.
			d.entries[i].end += delta
		}
	}
	if d.rootStart >= brace {
		d.rootStart += delta
	}
	if d.rootEnd > brace {
		d.rootEnd += delta
	}
	valStart := brace + valueOffset
	d.entries = append(d.entries, luaEntry{p: append(Path(nil), p...), start: valStart, end: valStart + len(lit)})
	return nil
}

// luaKey renders a key: a bare identifier where the grammar allows one, and a
// bracketed string otherwise, which is what the program's own writer does.
func luaKey(k string) string {
	bare := k != ""
	for i := 0; i < len(k); i++ {
		c := k[i]
		ok := c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || (i > 0 && c >= '0' && c <= '9')
		if !ok {
			bare = false
			break
		}
	}
	if bare {
		return k
	}
	lit, _ := luaLiteral(Value{Kind: KindString, Str: k})
	return "[" + lit + "]"
}

func luaValue(lit string) Value {
	switch lit {
	case "true":
		return Value{Kind: KindBool, Bool: true, Raw: lit}
	case "false":
		return Value{Kind: KindBool, Bool: false, Raw: lit}
	}
	if strings.HasPrefix(lit, `"`) {
		if s, ok := luaUnquote(lit); ok {
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
	return Value{Raw: lit}
}

// luaUnquote decodes the escapes Lua's %q emits across the 5.x family, plus the
// decimal escapes LuaJIT writes for control characters. A string carrying
// anything else is readable as its literal but not rewritable.
func luaUnquote(lit string) (string, bool) {
	if len(lit) < 2 || !strings.HasPrefix(lit, `"`) || !strings.HasSuffix(lit, `"`) {
		return "", false
	}
	body := lit[1 : len(lit)-1]
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' {
			if c == '"' {
				return "", false
			}
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(body) {
			return "", false
		}
		switch e := body[i]; e {
		case '"', '\\', '\'':
			b.WriteByte(e)
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'a':
			b.WriteByte(7)
		case 'b':
			b.WriteByte(8)
		case 'f':
			b.WriteByte(12)
		case 'v':
			b.WriteByte(11)
		case '\n':
			// %q writes a newline as a backslash followed by a real one.
			b.WriteByte('\n')
		default:
			if e < '0' || e > '9' {
				return "", false
			}
			digits := 0
			n := 0
			for digits < 3 && i < len(body) && body[i] >= '0' && body[i] <= '9' {
				n = n*10 + int(body[i]-'0')
				digits++
				i++
			}
			i--
			if n > 255 {
				return "", false
			}
			b.WriteByte(byte(n))
		}
	}
	return b.String(), true
}

func luaLiteral(v Value) (string, error) {
	switch v.Kind {
	case KindBool:
		return strconv.FormatBool(v.Bool), nil
	case KindInt:
		return strconv.FormatInt(v.Int, 10), nil
	case KindFloat:
		s := strconv.FormatFloat(v.Float, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		return s, nil
	case KindString:
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
			case c < 0x20 || c == 0x7f:
				// The decimal form the program's own writer uses, so the file
				// stays within the grammar its reader accepts.
				b.WriteString(`\`)
				b.WriteString(strconv.Itoa(int(c)))
			default:
				b.WriteByte(c)
			}
		}
		b.WriteByte('"')
		return b.String(), nil
	}
	return "", ErrUnsupportedValue
}
