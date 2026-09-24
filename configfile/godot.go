package configfile

import (
	"fmt"
	"strconv"
	"strings"
)

// Godot's ConfigFile: [section] headers over key=value lines, where each value is
// a Godot variant literal. Godot's own writer emits no comments and drops any it
// read, but a hand-edited file may carry them, so this parser keeps every byte it
// does not deliberately rewrite.
//
// A value that is not a plain scalar — an array, a dictionary, Vector2(…) — is
// located and reported as KindOpaque rather than interpreted. That is deliberate:
// SMB:Remastered stores window_size as [1024, 960] and a controller binding as
// [0, 1], and a rewriter that half-understood those would corrupt them. Opaque
// values are readable, shown as their literal, and refused by Set.

type godotDoc struct {
	buf     []byte
	entries []godotEntry
}

type godotEntry struct {
	section string
	key     string
	// start and end bracket the value's literal within buf, so an edit is a
	// splice rather than a re-serialisation of the file.
	start, end int
}

func (e godotEntry) path() Path {
	if e.section == "" {
		return Path{e.key}
	}
	return Path{e.section, e.key}
}

func parseGodot(data []byte) (Doc, error) {
	d := &godotDoc{buf: append([]byte(nil), data...)}
	s := string(data)
	section := ""

	for i := 0; i < len(s); {
		// The line this offset begins.
		nl := strings.IndexByte(s[i:], '\n')
		lineEnd := len(s)
		if nl >= 0 {
			lineEnd = i + nl
		}
		line := s[i:lineEnd]
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#"):
			// Blank or comment: carried through untouched.
		case strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"):
			section = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
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
			// The literal may run past the end of its line when it is a
			// bracketed value, so the span is found by balancing rather than by
			// taking the rest of the line.
			valStart := i + eq + 1
			for valStart < len(s) && (s[valStart] == ' ' || s[valStart] == '\t') {
				valStart++
			}
			valEnd, err := godotValueEnd(s, valStart)
			if err != nil {
				return nil, fmt.Errorf("%s=%s: %w", key, strings.TrimSpace(line[eq+1:]), err)
			}
			d.entries = append(d.entries, godotEntry{section: section, key: key, start: valStart, end: valEnd})
			if valEnd > lineEnd {
				// A multi-line literal: resume after it.
				i = valEnd
				continue
			}
		}

		if nl < 0 {
			break
		}
		i = lineEnd + 1
	}
	return d, nil
}

// godotValueEnd returns the offset just past the literal beginning at start,
// following brackets and quotes so a bracketed value that spans lines is
// measured whole. Trailing whitespace and carriage returns are excluded so an
// edit does not disturb the line ending.
func godotValueEnd(s string, start int) (int, error) {
	depth := 0
	inStr := false
	esc := false
	i := start
	for ; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '[', '{', '(':
			depth++
		case ']', '}', ')':
			depth--
		case '\n':
			if depth <= 0 {
				goto done
			}
		case ';', '#':
			// A trailing comment, but only outside a literal and at depth zero.
			if depth <= 0 {
				goto done
			}
		}
	}
done:
	if inStr {
		return 0, fmt.Errorf("unterminated string")
	}
	if depth > 0 {
		return 0, fmt.Errorf("unterminated %q", "[{(")
	}
	end := i
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return end, nil
}

func (d *godotDoc) find(p Path) int {
	for i, e := range d.entries {
		if e.path().equal(p) {
			return i
		}
	}
	return -1
}

func (d *godotDoc) Get(p Path) (Value, bool) {
	i := d.find(p)
	if i < 0 {
		return Value{}, false
	}
	e := d.entries[i]
	return godotValue(string(d.buf[e.start:e.end])), true
}

func (d *godotDoc) Paths() []Path {
	out := make([]Path, 0, len(d.entries))
	for _, e := range d.entries {
		out = append(out, e.path())
	}
	return out
}

func (d *godotDoc) Bytes() []byte { return append([]byte(nil), d.buf...) }

func (d *godotDoc) Set(p Path, v Value) error {
	i := d.find(p)
	if i < 0 {
		return fmt.Errorf("%w: %s", ErrNoSuchPath, p)
	}
	e := d.entries[i]
	if have := godotValue(string(d.buf[e.start:e.end])); have.Kind != v.Kind {
		return fmt.Errorf("%s: holds %v, not %v: %w", p, have.Kind, v.Kind, ErrKindMismatch)
	}
	lit, err := godotLiteral(v)
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

	// Every span after the edit moves by the difference in length. Held as
	// offsets rather than recomputed by reparsing, so an edit cannot change how
	// the rest of the file is understood.
	delta := len(lit) - (e.end - e.start)
	d.entries[i].end = e.start + len(lit)
	for j := range d.entries {
		if j == i {
			continue
		}
		if d.entries[j].start > e.start {
			d.entries[j].start += delta
			d.entries[j].end += delta
		}
	}
	return nil
}

// godotValue classifies a literal. Anything that is not a plain scalar, or a
// string carrying an escape this package does not reproduce exactly, is opaque.
func godotValue(lit string) Value {
	v := Value{Raw: lit}
	switch lit {
	case "true":
		return Value{Kind: KindBool, Bool: true, Raw: lit}
	case "false":
		return Value{Kind: KindBool, Bool: false, Raw: lit}
	}
	if strings.HasPrefix(lit, `"`) && strings.HasSuffix(lit, `"`) && len(lit) >= 2 {
		if s, ok := godotUnquote(lit); ok {
			return Value{Kind: KindString, Str: s, Raw: lit}
		}
		return v
	}
	if i, err := strconv.ParseInt(lit, 10, 64); err == nil {
		return Value{Kind: KindInt, Int: i, Raw: lit}
	}
	if f, err := strconv.ParseFloat(lit, 64); err == nil {
		return Value{Kind: KindFloat, Float: f, Raw: lit}
	}
	return v
}

func godotUnquote(lit string) (string, bool) {
	body := lit[1 : len(lit)-1]
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' {
			if c == '"' {
				return "", false // an unescaped quote inside the body
			}
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(body) {
			return "", false
		}
		switch body[i] {
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		default:
			// \uXXXX and anything else: readable as a literal, not rewritable,
			// because re-encoding it would not reproduce these bytes.
			return "", false
		}
	}
	return b.String(), true
}

func godotLiteral(v Value) (string, error) {
	switch v.Kind {
	case KindBool:
		return strconv.FormatBool(v.Bool), nil
	case KindInt:
		return strconv.FormatInt(v.Int, 10), nil
	case KindFloat:
		s := strconv.FormatFloat(v.Float, 'g', -1, 64)
		// Godot reads an unpunctuated number as an int, so a float keeps a point:
		// writing 1 where the program expects a float changes its type.
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		return s, nil
	case KindString:
		var b strings.Builder
		b.WriteByte('"')
		for i := 0; i < len(v.Str); i++ {
			switch c := v.Str[i]; c {
			case '"':
				b.WriteString(`\"`)
			case '\\':
				b.WriteString(`\\`)
			case '\n':
				b.WriteString(`\n`)
			case '\t':
				b.WriteString(`\t`)
			case '\r':
				b.WriteString(`\r`)
			default:
				b.WriteByte(c)
			}
		}
		b.WriteByte('"')
		return b.String(), nil
	}
	return "", ErrUnsupportedValue
}
