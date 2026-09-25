// Package schema describes which settings inside a config file a host should
// offer, and how. It is the counterpart to configfile: that package knows how to
// read and write a value, this one knows which values are worth showing, what to
// call them, and what sort of control belongs on them.
//
// A schema is data, and it is readable: a host can see every value a schema can
// reach before it touches a file, and Validate reports a schema that could not
// work against any file at all. Nothing here renders anything — the widget is a
// name the host interprets — because a settings page belongs to whoever is
// drawing it.
package schema

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zamiba/config-forge/configfile"
)

// File is one config file a program keeps, and the settings inside it.
type File struct {
	// Title names the file for a person — "Graphics", "Controls". Required, and
	// not derived from Path, because several programs here keep five or six
	// files and a page has to name them something better than general.json.
	Title string `json:"title"`

	// Order places this file among the program's others, lowest first; files
	// sharing an order fall back to their titles. It exists so an author does not
	// have to resort to numbering filenames.
	Order int `json:"order,omitempty"`

	// Path locates the file as the program's own declaration spells it, relative
	// to wherever the host keeps the program. Resolving it is the host's job.
	Path string `json:"path"`

	// Format is declared, never inferred from Path's extension: the same
	// extension is a different grammar in the next program.
	Format configfile.Format `json:"format"`

	// Reference names copies of this config file that the catalog ships beside the
	// schema — complete files, authored from the program's own source, with every
	// setting at the value the program itself uses. See Reference.
	//
	// It reads as a single object or as an array of them, because most programs
	// need one and a program whose config changes shape between releases needs one
	// per shape.
	Reference References `json:"reference,omitempty"`

	Sections []Section `json:"sections"`
}

// Reference is a copy of a config file, named by the schema that describes it.
//
// A reference copy is what makes two things possible that nothing should do on a
// guess: adding a section the program has not written, and starting a config file
// the program has never created. Both are copying rather than inventing, and the
// copy is the whole of the justification — which is why this names a file the
// catalog ships rather than holding the settings inline. A file can be diffed
// against a config captured from a real run; a tree inside a schema cannot, and
// that diff is the only way to tell a faithful copy from a plausible one.
//
// SinceVersion and UntilVersion limit it to a range of the program's releases, the
// same way a field's do. A reference with neither is the author saying this shape
// is right for every release the spec declares — which for a program with one
// release is simply true, and for one with several is a claim only somebody who
// knows the program can make. Several references are tried in declaration order,
// so an unbounded one placed last reads as the fallback it is.
type Reference struct {
	// File is the name of the copy, beside the schema. A bare name, not a path:
	// Path is where the *program* keeps its file and says nothing about where the
	// catalog keeps this one.
	File string `json:"file"`

	SinceVersion string `json:"sinceVersion,omitempty"`
	UntilVersion string `json:"untilVersion,omitempty"`
}

// References reads as one object or as an array of them, following the same two
// spellings userDataPaths uses in a spec.
type References []Reference

func (r *References) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "[") {
		var list []Reference
		if err := json.Unmarshal(data, &list); err != nil {
			return fmt.Errorf("reference: an array of objects with file, sinceVersion and untilVersion: %w", err)
		}
		*r = list
		return nil
	}
	var one Reference
	if err := json.Unmarshal(data, &one); err != nil {
		return fmt.Errorf("reference: an object with file, sinceVersion and untilVersion, or an array of them: %w", err)
	}
	*r = References{one}
	return nil
}

func (r References) MarshalJSON() ([]byte, error) {
	if len(r) == 1 {
		return json.Marshal(r[0])
	}
	return json.Marshal([]Reference(r))
}

// ReferenceFor is the copy that applies to a release, in declaration order.
func (f File) ReferenceFor(version string, versions []string) (Reference, bool) {
	for _, r := range f.Reference {
		if r.appliesTo(version, versions) {
			return r, true
		}
	}
	return Reference{}, false
}

// appliesTo mirrors a field's version bounds: a bound naming a release the spec does
// not declare is ignored rather than guessed at, and ValidateAgainstVersions is what
// reports it.
func (r Reference) appliesTo(version string, versions []string) bool {
	if version == "" || len(versions) == 0 {
		return true
	}
	at := indexOf(versions, version)
	if at < 0 {
		return true
	}
	if since := indexOf(versions, r.SinceVersion); since >= 0 && at < since {
		return false
	}
	if until := indexOf(versions, r.UntilVersion); until >= 0 && at > until {
		return false
	}
	return true
}

// Section groups fields under a heading, so a page can follow the file's own
// organisation rather than inventing one.
type Section struct {
	Title  string  `json:"title"`
	Help   string  `json:"help,omitempty"`
	Fields []Field `json:"fields"`
}

// Widget names the control a host should draw. It is a closed set so a host can
// be exhaustive, and a new one earns its place when a program needs it.
type Widget string

const (
	WidgetToggle   Widget = "toggle"
	WidgetCheckbox Widget = "checkbox"
	WidgetSelect   Widget = "select"
	WidgetRadio    Widget = "radio"
	WidgetText     Widget = "text"
	WidgetNumber   Widget = "number"
	WidgetSlider   Widget = "slider"
	WidgetPath     Widget = "path"
)

// Field is one editable setting.
type Field struct {
	// Pointer addresses the value, in the dotted form configfile.ParsePath reads.
	Pointer string `json:"pointer"`

	// Kind is what the file holds at Pointer — "bool", "int", "float" or
	// "string". It is declared separately from Widget because the two are not the
	// same question: a program storing 1 and 0 for a setting still wants a
	// toggle, and must still be written 1 and 0.
	Kind Kind `json:"kind"`

	Widget Widget `json:"widget"`
	Label  string `json:"label"`
	Help   string `json:"help,omitempty"`

	// On and Off are the values a toggle or checkbox writes when Kind is not
	// bool. Required there, forbidden when Kind is bool.
	On  *Value `json:"on,omitempty"`
	Off *Value `json:"off,omitempty"`

	// Options are the choices for a select or a radio group.
	Options []Option `json:"options,omitempty"`

	// Default is the value the *program* uses when the setting is absent from its
	// config — not a value PortForge invents. It makes a setting the program has
	// not written yet editable anyway: the control shows what the program would
	// use, and saving a change adds the key.
	//
	// It has to match what the program actually does, or the page states
	// something untrue about a setting nobody has touched. Take it from the
	// program's own source, and bound it with SinceVersion/UntilVersion when a
	// release changes it.
	Default *Value `json:"default,omitempty"`

	// Min, Max and Step bound a number or a slider. A slider needs both bounds;
	// a number field may have neither.
	Min  *float64 `json:"min,omitempty"`
	Max  *float64 `json:"max,omitempty"`
	Step *float64 `json:"step,omitempty"`

	// Unit is shown beside a number or a slider's readout — "%", "fps", " / 7".
	// Without it a bare "70" or "5" says nothing, so a numeric field that is not
	// self-evidently a count should carry one.
	Unit string `json:"unit,omitempty"`

	// ReadOnly shows the setting without a control. It is for values worth seeing
	// and not safely rewritable — a window size, a coordinate pair, a nested
	// block — so such a field may declare kind "opaque", which no editable field
	// may. It is a deliberate choice by the schema's author, not a fault, and a
	// host should mark it differently from a field it cannot edit for a reason.
	ReadOnly bool `json:"readOnly,omitempty"`

	// SinceVersion and UntilVersion limit the field to a range of the program's
	// releases, because a config's shape moves between them. Empty means
	// unbounded. UntilVersion is inclusive: it is the last release that had it.
	SinceVersion string `json:"sinceVersion,omitempty"`
	UntilVersion string `json:"untilVersion,omitempty"`
}

// Option is one choice of a select or radio field.
type Option struct {
	Label string `json:"label"`
	Value Value  `json:"value"`
}

// Kind mirrors configfile's editable kinds as a JSON name.
type Kind string

const (
	KindBool   Kind = "bool"
	KindInt    Kind = "int"
	KindFloat  Kind = "float"
	KindString Kind = "string"

	// KindNumber is a number whose spelling the program does not care about, for
	// a grammar that does not care either. It is for a writer that drops the
	// decimal point on a whole number — C#'s System.Text.Json writes a float of
	// 1.0 as `1`, and C++'s operator<< does the same — where declaring float
	// would leave the setting unavailable at exactly its own default, and
	// declaring int would refuse every value between. Only a format whose
	// numbers are untyped may use it, which Validate checks.
	KindNumber Kind = "number"

	// KindOpaque is a value no grammar here will rewrite — a list, a coordinate
	// pair, a nested table. Only a ReadOnly field may declare it.
	KindOpaque Kind = "opaque"
)

// accepts reports whether a value of kind c is what this declaration means. It
// is one-to-one for every kind but number, which takes either spelling.
func (k Kind) accepts(c configfile.Kind) bool {
	if k == KindNumber {
		return c == configfile.KindInt || c == configfile.KindFloat
	}
	want, ok := k.configKind()
	return ok && want == c
}

// numeric reports whether a field of this kind edits a number.
func (k Kind) numeric() bool {
	return k == KindInt || k == KindFloat || k == KindNumber
}

// editable reports whether an editable field may declare this kind.
func (k Kind) editable() bool {
	if k == KindNumber {
		return true
	}
	_, ok := k.configKind()
	return ok
}

// Kind returns the configfile kind this names, and whether it is one. A number
// names no single one, because taking either is the point of it.
func (k Kind) configKind() (configfile.Kind, bool) {
	switch k {
	case KindBool:
		return configfile.KindBool, true
	case KindInt:
		return configfile.KindInt, true
	case KindFloat:
		return configfile.KindFloat, true
	case KindString:
		return configfile.KindString, true
	}
	return configfile.KindOpaque, false
}

// Value is a literal in a schema — an option's value, a toggle's on and off. It
// decodes from JSON by its JSON type, and an unpunctuated number is an int,
// because writing 1 where a program keeps an int matters.
type Value struct {
	V configfile.Value
}

func (v *Value) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	switch {
	case s == "true":
		v.V = configfile.Bool(true)
	case s == "false":
		v.V = configfile.Bool(false)
	case strings.HasPrefix(s, `"`):
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return err
		}
		v.V = configfile.String(str)
	case strings.ContainsAny(s, ".eE"):
		var f float64
		if err := json.Unmarshal(data, &f); err != nil {
			return err
		}
		v.V = configfile.Float(f)
	default:
		var i int64
		if err := json.Unmarshal(data, &i); err != nil {
			return fmt.Errorf("a schema value is a bool, a number or a string: %s", s)
		}
		v.V = configfile.Int(i)
	}
	return nil
}

func (v Value) MarshalJSON() ([]byte, error) {
	switch v.V.Kind {
	case configfile.KindBool:
		return json.Marshal(v.V.Bool)
	case configfile.KindInt:
		return json.Marshal(v.V.Int)
	case configfile.KindFloat:
		return json.Marshal(v.V.Float)
	case configfile.KindString:
		return json.Marshal(v.V.Str)
	}
	return nil, fmt.Errorf("a schema value cannot be opaque")
}

// Validate reports every fault in a schema at once, rather than the first, since
// an author fixing a catalog entry wants the whole list. A schema that validates
// can still name a pointer a given file does not contain — that is a fact about
// the file, which Resolve reports, not a fault in the schema.
func Validate(f File) []error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if strings.TrimSpace(f.Path) == "" {
		bad("the file has no path")
	}
	if strings.TrimSpace(f.Title) == "" {
		bad("%s: the file has no title, so a page cannot name it", f.Path)
	}
	known := false
	for _, format := range configfile.Formats() {
		if f.Format == format {
			known = true
		}
	}
	if !known {
		bad("%q is not a format this build can read; they are %v", f.Format, configfile.Formats())
	}
	if len(f.Sections) == 0 {
		bad("%s: no sections, so the schema can reach nothing", f.Path)
	}
	seenRef := map[string]bool{}
	for _, r := range f.Reference {
		switch {
		case strings.TrimSpace(r.File) == "":
			bad("%s: a reference copy has no file", f.Path)
		case strings.ContainsAny(r.File, `/\`), r.File == ".", r.File == "..":
			bad("%s: a reference copy is a bare name beside the schema, not a path: %q", f.Path, r.File)
		case isSchemaName(r.File):
			// LoadDir reads these as schemas, so a reference named like one would be
			// parsed as a schema and fail.
			bad("%s: a reference copy must not be named %s, which is what a schema is: %q",
				f.Path, SchemaSuffix, r.File)
		case seenRef[r.File]:
			bad("%s: the reference copy %q is named twice", f.Path, r.File)
		}
		seenRef[r.File] = true
	}

	seen := map[string]string{}
	for _, sec := range f.Sections {
		if strings.TrimSpace(sec.Title) == "" {
			bad("a section has no title")
		}
		if len(sec.Fields) == 0 {
			bad("section %q has no fields", sec.Title)
		}
		for _, fl := range sec.Fields {
			where := fmt.Sprintf("%s/%s", sec.Title, fl.Pointer)
			if strings.TrimSpace(fl.Pointer) == "" {
				bad("section %q has a field with no pointer", sec.Title)
				continue
			}
			if prev, dup := seen[fl.Pointer]; dup {
				bad("%s is edited by two fields (%s and %s); one value cannot have two controls", fl.Pointer, prev, where)
			}
			seen[fl.Pointer] = where
			if strings.TrimSpace(fl.Label) == "" {
				bad("%s has no label", where)
			}
			if fl.ReadOnly {
				if fl.Default != nil {
					bad("%s: a read-only field is never written, so a default would never be used", where)
				}
				errs = append(errs, validateReadOnly(where, fl)...)
				continue
			}
			if !fl.Kind.editable() {
				bad("%s: %q is not a kind a file can hold editably; they are bool, int, float, number, string", where, fl.Kind)
				continue
			}
			if fl.Kind == KindNumber && !configfile.UntypedNumbers(f.Format) {
				bad("%s: kind number is for a grammar whose numbers are untyped, and %s's are not; declare int or float", where, f.Format)
				continue
			}
			errs = append(errs, validateWidget(where, fl)...)
		}
	}
	return errs
}

// validateReadOnly checks a field that is shown rather than edited. It carries no
// control, so everything that configures one is a mistake worth reporting rather
// than ignoring: an author who set them expected the field to be editable.
func validateReadOnly(where string, fl Field) []error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if !fl.Kind.editable() && fl.Kind != KindOpaque {
		bad("%s: %q is not a kind; they are bool, int, float, number, string, opaque", where, fl.Kind)
	}
	if fl.Widget != "" {
		bad("%s: a read-only field has no control, so it must not name a widget", where)
	}
	for what, set := range map[string]bool{
		"options": len(fl.Options) > 0,
		"on":      fl.On != nil,
		"off":     fl.Off != nil,
		"min":     fl.Min != nil,
		"max":     fl.Max != nil,
		"step":    fl.Step != nil,
		"unit":    fl.Unit != "",
	} {
		if set {
			bad("%s: a read-only field takes no %s", where, what)
		}
	}
	return errs
}

func validateWidget(where string, fl Field) []error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	switch fl.Widget {
	case WidgetToggle, WidgetCheckbox:
		if fl.Kind == KindBool {
			if fl.On != nil || fl.Off != nil {
				bad("%s: a bool toggle writes true and false, so it must not name on and off", where)
			}
		} else {
			if fl.On == nil || fl.Off == nil {
				bad("%s: a %s toggle must name the on and off values it writes", where, fl.Kind)
				break
			}
			if !fl.Kind.accepts(fl.On.V.Kind) || !fl.Kind.accepts(fl.Off.V.Kind) {
				bad("%s: on and off must be %s values, the kind the file holds", where, fl.Kind)
			}
			if fl.On.V == fl.Off.V {
				bad("%s: on and off are the same value", where)
			}
		}
	case WidgetSelect, WidgetRadio:
		if len(fl.Options) < 2 {
			bad("%s: a %s needs at least two options", where, fl.Widget)
		}
		values := map[configfile.Value]bool{}
		for _, o := range fl.Options {
			if strings.TrimSpace(o.Label) == "" {
				bad("%s: an option has no label", where)
			}
			if !fl.Kind.accepts(o.V().Kind) {
				bad("%s: option %q is a %v but the file holds %s", where, o.Label, o.V().Kind, fl.Kind)
			}
			if values[o.V()] {
				bad("%s: option value %s appears twice", where, o.V().Display())
			}
			values[o.V()] = true
		}
	case WidgetNumber, WidgetSlider:
		if !fl.Kind.numeric() {
			bad("%s: a %s edits a number, but the file holds %s", where, fl.Widget, fl.Kind)
		}
		if fl.Widget == WidgetSlider && (fl.Min == nil || fl.Max == nil) {
			bad("%s: a slider needs both min and max, or it cannot be drawn", where)
		}
		if fl.Min != nil && fl.Max != nil && *fl.Min >= *fl.Max {
			bad("%s: min %v is not below max %v", where, *fl.Min, *fl.Max)
		}
		if fl.Step != nil && *fl.Step <= 0 {
			bad("%s: step must be above zero", where)
		}
	case WidgetText, WidgetPath:
		if fl.Kind != KindString {
			bad("%s: a %s edits text, but the file holds %s", where, fl.Widget, fl.Kind)
		}
	case "":
		bad("%s has no widget", where)
	default:
		bad("%s: %q is not a widget", where, fl.Widget)
	}

	if len(fl.Options) > 0 && fl.Widget != WidgetSelect && fl.Widget != WidgetRadio {
		bad("%s: a %s takes no options", where, fl.Widget)
	}
	if fl.Unit != "" && fl.Widget != WidgetNumber && fl.Widget != WidgetSlider {
		bad("%s: a unit belongs to a number or a slider, not a %s", where, fl.Widget)
	}
	// A default is written like any other value, so it has to be one the field
	// would accept. A default the field's own rules reject would be offered and
	// then refused on save.
	if fl.Default != nil {
		if !fl.Kind.accepts(fl.Default.V.Kind) {
			bad("%s: the default is a %v but the file holds %s", where, fl.Default.V.Kind, fl.Kind)
		} else if err := checkFieldValue(fl, fl.Default.V); err != nil {
			bad("%s: the default is not a value this field accepts: %v", where, err)
		}
	}
	return errs
}

// V is the option's underlying value.
func (o Option) V() configfile.Value { return o.Value.V }
