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

	Sections []Section `json:"sections"`
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

	// KindOpaque is a value no grammar here will rewrite — a list, a coordinate
	// pair, a nested table. Only a ReadOnly field may declare it.
	KindOpaque Kind = "opaque"
)

// Kind returns the configfile kind this names, and whether it is one.
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
				errs = append(errs, validateReadOnly(where, fl)...)
				continue
			}
			kind, ok := fl.Kind.configKind()
			if !ok {
				bad("%s: %q is not a kind a file can hold editably; they are bool, int, float, string", where, fl.Kind)
				continue
			}
			errs = append(errs, validateWidget(where, fl, kind)...)
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

	if _, ok := fl.Kind.configKind(); !ok && fl.Kind != KindOpaque {
		bad("%s: %q is not a kind; they are bool, int, float, string, opaque", where, fl.Kind)
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

func validateWidget(where string, fl Field, kind configfile.Kind) []error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	switch fl.Widget {
	case WidgetToggle, WidgetCheckbox:
		if kind == configfile.KindBool {
			if fl.On != nil || fl.Off != nil {
				bad("%s: a bool toggle writes true and false, so it must not name on and off", where)
			}
		} else {
			if fl.On == nil || fl.Off == nil {
				bad("%s: a %s toggle must name the on and off values it writes", where, fl.Kind)
				break
			}
			if fl.On.V.Kind != kind || fl.Off.V.Kind != kind {
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
			if o.V().Kind != kind {
				bad("%s: option %q is a %v but the file holds %s", where, o.Label, o.V().Kind, fl.Kind)
			}
			if values[o.V()] {
				bad("%s: option value %s appears twice", where, o.V().Display())
			}
			values[o.V()] = true
		}
	case WidgetNumber, WidgetSlider:
		if kind != configfile.KindInt && kind != configfile.KindFloat {
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
		if kind != configfile.KindString {
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
	return errs
}

// V is the option's underlying value.
func (o Option) V() configfile.Value { return o.Value.V }
