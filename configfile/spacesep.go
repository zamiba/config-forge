package configfile

import (
	"fmt"
	"strconv"
	"strings"
)

// A settings file whose lines are a key and a value with whitespace between them,
// no sections and no equals sign. It is what a C program writes with fprintf and
// reads back with a tokeniser, and two programs here write it: Render96's
// sm64config.txt, from sm64ex's configfile.c, and melee-pc's launcher.cfg.
//
// The value is the rest of the line. That is deliberate rather than lazy: both of
// these files hold entries whose value is several tokens — a controller binding
// is three hex numbers on one line — and a parser that took only the first would
// report a binding as the number 38 and let something overwrite it. So a value of
// more than one token is located whole and reported as KindOpaque, readable and
// refused by Set, like every other value these grammars cannot safely rewrite.
//
// Neither program writes a comment, and neither reads one except on a line of its
// own, so a trailing # is not treated as one here either; a line carrying one has
// a value of several tokens and is therefore opaque, which is the safe reading.
//
// The two programs do not agree on how to spell things, and that is the schema's
// business rather than this parser's. Render96 writes a bool as true or false and
// a float with six decimal places; melee-pc writes a bool as 1 or 0, which is an
// int here with on and off in the schema, and a float through operator<< which
// drops the point on a whole number — so a melee-pc float is kind "number".

type spaceSepDoc struct {
	buf     []byte
	entries []spaceSepEntry
	// end is where a new key is inserted: just past the last line that holds one,
	// or the start of the file when there are none.
	end int
}

type spaceSepEntry struct {
	key        string
	start, end int
}

func parseSpaceSeparated(data []byte) (Doc, error) {
	d := &spaceSepDoc{buf: append([]byte(nil), data...)}
	s := string(data)

	for i := 0; i < len(s); {
		lineEnd := lineEndAt(s, i)
		line := s[i:lineEnd]
		trimmed := strings.TrimSpace(line)

		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			// The key is the first run of non-space characters.
			k := 0
			for k < len(line) && (line[k] == ' ' || line[k] == '\t') {
				k++
			}
			keyEnd := k
			for keyEnd < len(line) && line[keyEnd] != ' ' && line[keyEnd] != '\t' {
				keyEnd++
			}
			// A line with nothing after the key is not an assignment. Left alone
			// rather than guessed at, which is what both readers do with it.
			start := keyEnd
			for start < len(line) && (line[start] == ' ' || line[start] == '\t') {
				start++
			}
			if keyEnd > k && start < len(line) {
				end := len(line)
				for end > start && (line[end-1] == ' ' || line[end-1] == '\t' || line[end-1] == '\r') {
					end--
				}
				d.entries = append(d.entries, spaceSepEntry{
					key: line[k:keyEnd], start: i + start, end: i + end,
				})
				d.end = lineEnd
			}
		}

		if lineEnd >= len(s) {
			break
		}
		i = lineEnd + 1
	}
	return d, nil
}

func (d *spaceSepDoc) find(p Path) int {
	if len(p) != 1 {
		return -1
	}
	for i, e := range d.entries {
		if e.key == p[0] {
			return i
		}
	}
	return -1
}

func (d *spaceSepDoc) Get(p Path) (Value, bool) {
	i := d.find(p)
	if i < 0 {
		return Value{}, false
	}
	e := d.entries[i]
	return spaceSepValue(string(d.buf[e.start:e.end])), true
}

func (d *spaceSepDoc) Paths() []Path {
	out := make([]Path, 0, len(d.entries))
	for _, e := range d.entries {
		out = append(out, Path{e.key})
	}
	return out
}

func (d *spaceSepDoc) Bytes() []byte { return append([]byte(nil), d.buf...) }

func (d *spaceSepDoc) Set(p Path, v Value) error {
	i := d.find(p)
	if i < 0 {
		return fmt.Errorf("%w: %s", ErrNoSuchPath, p)
	}
	e := d.entries[i]
	if have := spaceSepValue(string(d.buf[e.start:e.end])); have.Kind != v.Kind {
		return fmt.Errorf("%s: holds %v, not %v: %w", p, have.Kind, v.Kind, ErrKindMismatch)
	}
	return d.write(i, p, v)
}

// SetNumber rewrites a number in the spelling the value carries. Nothing on these
// lines is typed, and melee-pc's own writer drops the point on a whole float, so
// which spelling the program wants is the schema's business.
func (d *spaceSepDoc) SetNumber(p Path, v Value) error {
	i := d.find(p)
	if i < 0 {
		return fmt.Errorf("%w: %s", ErrNoSuchPath, p)
	}
	e := d.entries[i]
	have := spaceSepValue(string(d.buf[e.start:e.end]))
	if !numeric(have.Kind) || !numeric(v.Kind) {
		return fmt.Errorf("%s: holds %v, not a number: %w", p, have.Kind, ErrKindMismatch)
	}
	return d.write(i, p, v)
}

func (d *spaceSepDoc) write(i int, p Path, v Value) error {
	e := d.entries[i]
	old := string(d.buf[e.start:e.end])
	lit, err := spaceSepLiteral(v)
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

func (d *spaceSepDoc) splice(start, end int, text string) {
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
	if d.end >= end {
		d.end += delta
	}
}

// Nothing nests here, so the only container is the file, which is always there.
func (d *spaceSepDoc) CanCreate(p Path) bool {
	return len(p) == 1 && p[0] != "" && d.find(p) < 0
}

// Nothing nests in this grammar, so there is no container to add. Every path is
// one element and its container is the file itself, which is always there.
func (d *spaceSepDoc) CreateContainer(p Path) error {
	return fmt.Errorf("%w: this grammar does not nest: %s", ErrNoContainer, p)
}

func (d *spaceSepDoc) Create(p Path, v Value) error {
	if len(p) != 1 || p[0] == "" {
		return fmt.Errorf("%w: %s", ErrNoContainer, p)
	}
	if d.find(p) >= 0 {
		return fmt.Errorf("%w: %s", ErrAlreadyPresent, p)
	}
	lit, err := spaceSepLiteral(v)
	if err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	line := p[0] + " " + lit

	at := d.end
	if at > len(d.buf) {
		at = len(d.buf)
	}
	var text string
	var valueOffset int
	if len(d.entries) == 0 {
		// Nothing to follow: the key goes at the top, above whatever comments or
		// blank lines the file begins with.
		at = 0
		text = line + "\n"
		valueOffset = len(line) - len(lit)
	} else {
		text = "\n" + line
		valueOffset = 1 + len(line) - len(lit)
	}

	d.splice(at, at, text)
	valStart := at + valueOffset
	d.entries = append(d.entries, spaceSepEntry{key: p[0], start: valStart, end: valStart + len(lit)})
	d.end = valStart + len(lit)
	return nil
}

func spaceSepValue(text string) Value {
	switch text {
	case "true":
		return Value{Kind: KindBool, Bool: true, Raw: text}
	case "false":
		return Value{Kind: KindBool, Bool: false, Raw: text}
	}
	// A quoted run is one value however many spaces are inside it, which is what
	// std::quoted reads, so the quoting is tested before the token count. One that
	// does not enclose the whole of the rest of the line — `"a" "b"` — is not a
	// single string and falls through to being opaque.
	if strings.HasPrefix(text, `"`) {
		if s, ok := spaceSepUnquote(text); ok {
			return Value{Kind: KindString, Str: s, Raw: text}
		}
		return Value{Raw: text}
	}
	// More than one token: a controller binding, or a value with a note after it.
	// Located, shown, and never rewritten.
	if strings.ContainsAny(text, " \t") {
		return Value{Raw: text}
	}
	if i, err := strconv.ParseInt(text, 10, 64); err == nil {
		return Value{Kind: KindInt, Int: i, Raw: text}
	}
	if f, err := strconv.ParseFloat(text, 64); err == nil && !strings.ContainsAny(text, "xXnNiI") {
		// ParseFloat also reads inf, nan and hex floats. On a line like
		// `install_id 1a2b3c` — melee-pc writes that one in hex — a word is far
		// likelier than a number, and a value left opaque is never rewritten.
		return Value{Kind: KindFloat, Float: f, Raw: text}
	}
	return Value{Raw: text}
}

// spaceSepUnquote decodes what C++'s std::quoted writes: a quoted run in which a
// quote and a backslash each carry a backslash, and nothing else is special.
func spaceSepUnquote(lit string) (string, bool) {
	if len(lit) < 2 || !strings.HasPrefix(lit, `"`) || !strings.HasSuffix(lit, `"`) {
		return "", false
	}
	body := lit[1 : len(lit)-1]
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		if body[i] == '\\' && i+1 < len(body) {
			i++
			b.WriteByte(body[i])
			continue
		}
		if body[i] == '"' {
			// An unescaped quote inside the run means this is not one quoted value.
			return "", false
		}
		b.WriteByte(body[i])
	}
	return b.String(), true
}

func spaceSepLiteral(v Value) (string, error) {
	switch v.Kind {
	case KindBool:
		return strconv.FormatBool(v.Bool), nil
	case KindInt:
		return strconv.FormatInt(v.Int, 10), nil
	case KindFloat:
		s := strconv.FormatFloat(v.Float, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eE") {
			// Without the point the value comes back as an int, and a schema that
			// declared float would report the setting as changed underneath it. A
			// program that writes the bare form declares kind "number" instead.
			s += ".0"
		}
		return s, nil
	case KindString:
		// One line, quoted the way std::quoted reads it.
		if strings.ContainsAny(v.Str, "\n\r") {
			return "", ErrUnsupportedValue
		}
		var b strings.Builder
		b.WriteByte('"')
		for i := 0; i < len(v.Str); i++ {
			if c := v.Str[i]; c == '"' || c == '\\' {
				b.WriteByte('\\')
			}
			b.WriteByte(v.Str[i])
		}
		b.WriteByte('"')
		return b.String(), nil
	}
	return "", ErrUnsupportedValue
}
