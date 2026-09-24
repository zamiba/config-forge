package configfile

import (
	"fmt"
	"strconv"
	"strings"
)

// A plain key/value config: `key = value` lines, optional [section] headers,
// and # or ; comments. It is the shape a program reaches for when it wants a
// file a person can edit, and two programs here write it — Crash Bandicoot
// Recompiled's interface.ini under a [RecompOne] header, Open Nectar's
// pikmin_settings.conf with no headers at all.
//
// Unlike Godot's ConfigFile, which this resembles, the values are not literals
// of any language: they are text, and what a value means is decided by whatever
// parses it on the other side. So the kind is read from the text — true or false
// in any capitalisation is a bool, a whole number is an int, a decimal is a
// float, and everything else is a string with no quotes around it. Rewriting a
// bool keeps the capitalisation the file already used, because a program that
// writes True may well be one that only reads True.
//
// Two consequences a schema author should know. A string whose text happens to
// read as a number or as true is indistinguishable from one, and comes back as
// that kind. And an inline # is not treated as a comment on a value line, since
// nothing says it is not part of the value; a whole-line comment is one, and is
// carried through untouched like everything else.
//
// The file may hold things that are not this grammar at all — interface.ini ends
// with an ImGui layout blob — and that is fine: an unrecognised line is left
// exactly where it is, and only a path a schema names is ever written.

type iniDoc struct {
	buf     []byte
	entries []iniEntry
	// sectionEnd is the offset a new key for each section is inserted at, so a
	// new key joins its own section rather than landing at the end of the file.
	sectionEnd map[string]int
	// spaced records whether this file writes `key = value` or `key=value`, taken
	// from the first entry, so a created key matches what is already there.
	spaced bool
}

type iniEntry struct {
	section string
	key     string
	// start and end bracket the value's text within buf.
	start, end int
}

func (e iniEntry) path() Path {
	if e.section == "" {
		return Path{e.key}
	}
	return Path{e.section, e.key}
}

func parseINI(data []byte) (Doc, error) {
	d := &iniDoc{buf: append([]byte(nil), data...), sectionEnd: map[string]int{}, spaced: true}
	s := string(data)
	section := ""
	first := true

	for i := 0; i < len(s); {
		nl := strings.IndexByte(s[i:], '\n')
		lineEnd := len(s)
		if nl >= 0 {
			lineEnd = i + nl
		}
		line := s[i:lineEnd]
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";"):
			// Blank or comment: carried through untouched.
		case strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"):
			section = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			d.sectionEnd[section] = lineEnd
		default:
			eq := strings.IndexByte(line, '=')
			if eq < 0 {
				// Not a header, not a comment, not an assignment. Left alone
				// rather than guessed at.
				break
			}
			key := strings.TrimSpace(line[:eq])
			if key == "" {
				break
			}
			// The value is the rest of the line with its surrounding spaces
			// dropped, so the span covers the text and not the layout.
			start := i + eq + 1
			end := lineEnd
			for start < end && (s[start] == ' ' || s[start] == '\t') {
				start++
			}
			for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
				end--
			}
			if first {
				d.spaced = eq+1 < len(line) && (line[eq+1] == ' ' || line[eq+1] == '\t')
				first = false
			}
			d.entries = append(d.entries, iniEntry{section: section, key: key, start: start, end: end})
			d.sectionEnd[section] = lineEnd
		}

		if nl < 0 {
			break
		}
		i = lineEnd + 1
	}
	return d, nil
}

func (d *iniDoc) find(p Path) int {
	for i, e := range d.entries {
		if e.path().equal(p) {
			return i
		}
	}
	return -1
}

func (d *iniDoc) Get(p Path) (Value, bool) {
	i := d.find(p)
	if i < 0 {
		return Value{}, false
	}
	e := d.entries[i]
	return iniValue(string(d.buf[e.start:e.end])), true
}

func (d *iniDoc) Paths() []Path {
	out := make([]Path, 0, len(d.entries))
	for _, e := range d.entries {
		out = append(out, e.path())
	}
	return out
}

func (d *iniDoc) Bytes() []byte { return append([]byte(nil), d.buf...) }

func (d *iniDoc) Set(p Path, v Value) error {
	i := d.find(p)
	if i < 0 {
		return fmt.Errorf("%w: %s", ErrNoSuchPath, p)
	}
	e := d.entries[i]
	old := string(d.buf[e.start:e.end])
	if have := iniValue(old); have.Kind != v.Kind {
		return fmt.Errorf("%s: holds %v, not %v: %w", p, have.Kind, v.Kind, ErrKindMismatch)
	}
	return d.write(i, p, v)
}

// SetNumber rewrites a number in the spelling the value carries. Nothing in this
// grammar is typed — 1 and 1.0 are both just text — so which of them the program
// on the other side wants is the schema's business, not this parser's.
func (d *iniDoc) SetNumber(p Path, v Value) error {
	i := d.find(p)
	if i < 0 {
		return fmt.Errorf("%w: %s", ErrNoSuchPath, p)
	}
	e := d.entries[i]
	have := iniValue(string(d.buf[e.start:e.end]))
	if !numeric(have.Kind) || !numeric(v.Kind) {
		return fmt.Errorf("%s: holds %v, not a number: %w", p, have.Kind, ErrKindMismatch)
	}
	return d.write(i, p, v)
}

func (d *iniDoc) write(i int, p Path, v Value) error {
	e := d.entries[i]
	old := string(d.buf[e.start:e.end])
	lit, err := iniLiteral(v, old)
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

// splice replaces the bytes between start and end and moves every offset that
// sat after them. Nothing nests in this grammar, so an offset either precedes
// the edit or follows it.
func (d *iniDoc) splice(start, end int, text string) {
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
	for section, at := range d.sectionEnd {
		if at >= end {
			d.sectionEnd[section] = at + delta
		}
	}
}

// container reports the section a path belongs to, and whether the file has it.
// A path of one element belongs to the headerless top of the file, which every
// file has.
func (d *iniDoc) container(p Path) (string, bool) {
	switch len(p) {
	case 1:
		return "", true
	case 2:
		_, ok := d.sectionEnd[p[0]]
		return p[0], ok
	}
	return "", false
}

func (d *iniDoc) CanCreate(p Path) bool {
	if len(p) == 0 || p[len(p)-1] == "" || d.find(p) >= 0 {
		return false
	}
	_, ok := d.container(p)
	return ok
}

func (d *iniDoc) Create(p Path, v Value) error {
	if len(p) == 0 || p[len(p)-1] == "" {
		return fmt.Errorf("%w: %s", ErrNoSuchPath, p)
	}
	if d.find(p) >= 0 {
		return fmt.Errorf("%w: %s", ErrAlreadyPresent, p)
	}
	section, ok := d.container(p)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoContainer, p[0])
	}
	key := p[len(p)-1]
	lit, err := iniLiteral(v, "")
	if err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	sep := "="
	if d.spaced {
		sep = " = "
	}
	line := key + sep + lit

	at, ok := d.sectionEnd[section]
	var text string
	var valueOffset int
	if ok {
		// sectionEnd sits at the end of the section's last line, so the new key
		// goes in as the line after it.
		text = "\n" + line
		valueOffset = 1 + len(key) + len(sep)
		if at > len(d.buf) {
			at = len(d.buf)
		}
	} else {
		// Headerless, and the file has no line above its first header yet, so
		// the key becomes the file's first line.
		at = 0
		text = line + "\n"
		valueOffset = len(key) + len(sep)
	}

	d.splice(at, at, text)
	valStart := at + valueOffset
	d.entries = append(d.entries, iniEntry{
		section: section, key: key,
		start: valStart, end: valStart + len(lit),
	})
	d.sectionEnd[section] = valStart + len(lit)
	return nil
}

func iniValue(text string) Value {
	switch strings.ToLower(text) {
	case "true":
		return Value{Kind: KindBool, Bool: true, Raw: text}
	case "false":
		return Value{Kind: KindBool, Bool: false, Raw: text}
	}
	if text != "" {
		if i, err := strconv.ParseInt(text, 10, 64); err == nil {
			return Value{Kind: KindInt, Int: i, Raw: text}
		}
		if f, err := strconv.ParseFloat(text, 64); err == nil && !strings.ContainsAny(text, "xXnNiI") {
			// ParseFloat also reads "inf" and hex floats, which in a file like
			// this are far more likely to be somebody's word than a number.
			return Value{Kind: KindFloat, Float: f, Raw: text}
		}
	}
	return Value{Kind: KindString, Str: text, Raw: text}
}

// iniLiteral renders a value. old is the text being replaced, which is what
// decides a bool's capitalisation; it is empty when the key is being created.
func iniLiteral(v Value, old string) (string, error) {
	switch v.Kind {
	case KindBool:
		t, f := "true", "false"
		switch {
		case old == strings.ToUpper(old) && old != "":
			t, f = "TRUE", "FALSE"
		case len(old) > 0 && old[0] >= 'A' && old[0] <= 'Z':
			t, f = "True", "False"
		}
		if v.Bool {
			return t, nil
		}
		return f, nil
	case KindInt:
		return strconv.FormatInt(v.Int, 10), nil
	case KindFloat:
		return strconv.FormatFloat(v.Float, 'g', -1, 64), nil
	case KindString:
		// The value is one line of text with no quoting, so anything that would
		// end the line, start a comment at its head, or be trimmed back off on
		// the next read is not a value this format can hold.
		if strings.ContainsAny(v.Str, "\n\r") {
			return "", ErrUnsupportedValue
		}
		if v.Str != strings.TrimSpace(v.Str) {
			return "", ErrUnsupportedValue
		}
		if strings.HasPrefix(v.Str, "#") || strings.HasPrefix(v.Str, ";") || strings.HasPrefix(v.Str, "[") {
			return "", ErrUnsupportedValue
		}
		return v.Str, nil
	}
	return "", ErrUnsupportedValue
}
