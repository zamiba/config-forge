// Package configfile reads and writes individual settings inside a program's own
// configuration file, without taking ownership of the file.
//
// That distinction is the whole point. A program's config holds far more than any
// editor will ever model — window geometry, the last controller used, keys a newer
// release added — and it is the program, not this package, that owns all of it. So
// a Doc is not a representation of the file that gets serialised back out. It is
// the file's bytes, plus the positions of the values inside them. A Set rewrites
// one value in place; Bytes returns the original buffer with only those edits
// applied. Anything not written to comes back byte for byte, comments, key order,
// blank lines and unknown entries included.
//
// The format is always declared, never inferred from the file's extension. An
// extension says nothing: .cfg is Godot's ConfigFile in one program and an
// entirely different grammar in the next.
package configfile

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Format names a config grammar this package can edit.
type Format string

const (
	// FormatGodot is Godot's ConfigFile: [section] headers over key=value lines
	// whose values are Godot variant literals.
	FormatGodot Format = "godot"

	// FormatLuaTable is a Lua data file — `return { key = value, … }` — in the
	// restricted grammar a deterministic writer emits: keyed tables, numbers,
	// booleans and quoted strings. It is not general Lua, and must not be.
	FormatLuaTable Format = "luaTable"
)

var (
	// ErrNoSuchPath is returned by Set for a path the file does not already
	// contain. Editing an existing value is safe; creating structure is not —
	// a program that reads its config into a fixed set of sections can fail on
	// one it does not recognise — so creating is left to the host, deliberately.
	ErrNoSuchPath = errors.New("configfile: no such path in this file")

	// ErrUnsupportedValue is returned by Set when the value in the file is of a
	// kind this package can read but not safely rewrite. The field is readable
	// and must be presented as read-only rather than guessed at.
	ErrUnsupportedValue = errors.New("configfile: this value's kind cannot be rewritten")

	// ErrAlreadyPresent is returned by Create for a path the file already holds.
	// Set is what changes an existing value.
	ErrAlreadyPresent = errors.New("configfile: the file already holds this path")

	// ErrNoContainer is returned by Create when the section or table the path
	// belongs to is not in the file. Adding it would mean adding structure, which
	// this package does not do.
	ErrNoContainer = errors.New("configfile: the section or table this path belongs to is not in the file")

	// ErrKindMismatch is returned by Set when the value's kind differs from what
	// the file already holds at that path. A program reads its config into typed
	// fields, so turning its [1024, 960] into a string, or its 1 into true, can
	// break it in ways no test here would catch. The schema is where a field's
	// kind in the file is declared, separately from the widget used to edit it —
	// which is how an int used as a boolean is edited by a toggle that writes 0
	// and 1 rather than by writing true.
	ErrKindMismatch = errors.New("configfile: that is not the kind of value this file holds here")

	// ErrUnknownFormat is returned by Parse and Open for a format this build
	// does not implement.
	ErrUnknownFormat = errors.New("configfile: unknown format")
)

// Kind is what sort of value sits at a path.
type Kind int

const (
	// KindOpaque is a value this package can locate and show but not rewrite —
	// an array, a Vector2, a dictionary. Raw holds its literal text.
	KindOpaque Kind = iota
	KindBool
	KindInt
	KindFloat
	KindString
)

func (k Kind) String() string {
	switch k {
	case KindBool:
		return "bool"
	case KindInt:
		return "int"
	case KindFloat:
		return "float"
	case KindString:
		return "string"
	}
	return "opaque"
}

// Value is one setting's value. Raw is always the literal as it appears in the
// file, so a host can show an opaque value without understanding it.
type Value struct {
	Kind  Kind
	Bool  bool
	Int   int64
	Float float64
	Str   string
	Raw   string
}

func Bool(b bool) Value     { return Value{Kind: KindBool, Bool: b} }
func Int(i int64) Value     { return Value{Kind: KindInt, Int: i} }
func Float(f float64) Value { return Value{Kind: KindFloat, Float: f} }
func String(s string) Value { return Value{Kind: KindString, Str: s} }

// Display renders the value for a UI. For an opaque value that is its literal.
func (v Value) Display() string {
	switch v.Kind {
	case KindBool:
		return strconv.FormatBool(v.Bool)
	case KindInt:
		return strconv.FormatInt(v.Int, 10)
	case KindFloat:
		return strconv.FormatFloat(v.Float, 'g', -1, 64)
	case KindString:
		return v.Str
	}
	return v.Raw
}

// Path addresses one value. It is a slice rather than a dotted string because a
// key may itself contain a dot, and a format may nest arbitrarily; ParsePath is
// the convenience for the common case where neither is true.
type Path []string

func ParsePath(s string) Path { return Path(strings.Split(s, ".")) }

func (p Path) String() string { return strings.Join(p, ".") }

func (p Path) equal(q Path) bool {
	if len(p) != len(q) {
		return false
	}
	for i := range p {
		if p[i] != q[i] {
			return false
		}
	}
	return true
}

// Doc is an open config file: the bytes, and where the values are within them.
type Doc interface {
	// Get reports the value at a path, and whether the file contains it.
	Get(Path) (Value, bool)

	// Set rewrites the value at a path in place. It returns ErrNoSuchPath if the
	// file does not already contain it, and ErrUnsupportedValue if what is there
	// is a kind this package will not rewrite.
	Set(Path, Value) error

	// Create adds a value the file does not hold yet. It adds a leaf into a
	// container that already exists and never creates the container: a program
	// reads its config into a fixed set of sections, and one it does not
	// recognise can break its load outright, while a key it does not recognise
	// inside a section it does sits there harmlessly. CanCreate reports in
	// advance whether a given path is addable, so a caller can present a setting
	// as editable only when writing it would actually work.
	//
	// Create is for a setting the caller already knows about — one a schema
	// names, with a default the program itself would use. It is not a way to put
	// arbitrary keys into someone else's file.
	Create(Path, Value) error

	// CanCreate reports whether a path the file lacks could be added: true when
	// its container is present, false when adding it would mean adding structure.
	CanCreate(Path) bool

	// Paths lists every value the file contains, in the order it declares them.
	// It is what a host uses to report a schema field that no longer exists.
	Paths() []Path

	// Bytes returns the file with the applied edits and nothing else changed.
	Bytes() []byte
}

// Open reads and parses a config file. The format is declared by the caller.
func Open(path string, format Format) (Doc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc, err := Parse(data, format)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return doc, nil
}

// Parse parses config file contents in the named format.
func Parse(data []byte, format Format) (Doc, error) {
	switch format {
	case FormatGodot:
		return parseGodot(data)
	case FormatLuaTable:
		return parseLuaTable(data)
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownFormat, format)
}

// Formats lists the formats this build implements.
func Formats() []Format { return []Format{FormatGodot, FormatLuaTable} }

// Empty is a document that contains nothing: every Get reports absent and every
// Set is refused.
//
// It exists for the file a program has not written yet. Parsing an empty byte
// slice is not the same thing and cannot stand in for it — a grammar with a
// required preamble, such as a Lua data file's `return`, rightly rejects empty
// input — but a host still needs to resolve a schema against "the file is not
// there" so it can describe the settings it would offer once it is.
func Empty() Doc { return emptyDoc{} }

type emptyDoc struct{}

func (emptyDoc) Get(Path) (Value, bool) { return Value{}, false }
func (emptyDoc) Paths() []Path          { return nil }
func (emptyDoc) Bytes() []byte          { return nil }

func (emptyDoc) Set(p Path, _ Value) error {
	return fmt.Errorf("%w: %s", ErrNoSuchPath, p)
}

// Nothing can be added to a file that is not there: there is no container to add
// it to, and creating one would be creating the file.
func (emptyDoc) Create(p Path, _ Value) error {
	return fmt.Errorf("%w: %s", ErrNoContainer, p)
}

func (emptyDoc) CanCreate(Path) bool { return false }
