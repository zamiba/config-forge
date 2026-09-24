package schema

import (
	"errors"
	"fmt"

	"github.com/zamiba/config-forge/configfile"
)

// FieldState is a field as it stands against one real file: what it currently
// holds, and whether it can be edited at all.
type FieldState struct {
	Field
	Value   configfile.Value
	Present bool

	// Editable is false when the file does not contain the field, or contains
	// something other than the kind the schema declares. A field is never quietly
	// dropped: a schema that has fallen behind its program shows as unavailable
	// rather than being written to on a guess.
	Editable bool

	// ReadOnly is a field the schema shows without a control, on purpose. It is
	// not editable and has no Reason: nothing is wrong with it.
	ReadOnly bool

	// Unset is a field the file does not hold, shown with the program's own
	// default and editable anyway: saving a change adds the key. Value is the
	// default, so a host renders it like any other value — but it is what the
	// program would use, not what it has recorded.
	Unset bool

	// Reason says why it is not editable, as a code rather than a sentence, so
	// the words shown to a person belong to whoever is drawing the page. Problem
	// is the same fact spelled out, for a log or a catalog test.
	Reason  Reason
	Problem string
}

// Reason is why a field cannot be edited.
type Reason string

const (
	// ReasonNone is an editable field.
	ReasonNone Reason = ""
	// ReasonMissing: the file does not contain the setting. Usually a program
	// that has never been launched and so has never written its config.
	ReasonMissing Reason = "missing"
	// ReasonKindChanged: the file holds a different kind of value than the schema
	// declares, which is what a release changing a setting's type looks like.
	ReasonKindChanged Reason = "kindChanged"
	// ReasonUnknownKind: the schema itself declares a kind this build does not
	// know. A fault in the schema, not in the file.
	ReasonUnknownKind Reason = "unknownKind"
)

// SectionState is a section with the fields that apply to this release.
type SectionState struct {
	Title  string
	Help   string
	Fields []FieldState
}

// Resolve reads a schema against an open file and reports what a host should
// draw. Fields outside the release's version range are left out entirely; fields
// the file cannot support are included and marked, because "this setting exists
// but cannot be edited here" is worth showing and silence is not.
//
// versions is the program's releases, oldest first, as the install spec declares
// them; it is what SinceVersion and UntilVersion are compared against. A bound
// naming a release not in the list is ignored rather than guessed at — a
// misspelled bound is a schema fault, which ValidateAgainstVersions reports.
func Resolve(d configfile.Doc, f File, version string, versions []string) []SectionState {
	var out []SectionState
	for _, sec := range f.Sections {
		state := SectionState{Title: sec.Title, Help: sec.Help}
		for _, fl := range sec.Fields {
			if !fl.appliesTo(version, versions) {
				continue
			}
			state.Fields = append(state.Fields, resolveField(d, fl))
		}
		if len(state.Fields) > 0 {
			out = append(out, state)
		}
	}
	return out
}

func resolveField(d configfile.Doc, fl Field) FieldState {
	st := FieldState{Field: fl}
	if fl.ReadOnly {
		// Shown, never written, whatever the file holds — including a kind no
		// grammar here can rewrite, which is the usual reason to declare one.
		st.ReadOnly = true
		v, present := d.Get(configfile.ParsePath(fl.Pointer))
		st.Value, st.Present = v, present
		if !present {
			st.Reason = ReasonMissing
			st.Problem = "this setting is not in the file"
		}
		return st
	}
	if !fl.Kind.editable() {
		st.Reason = ReasonUnknownKind
		st.Problem = fmt.Sprintf("the schema declares an unknown kind, %q", fl.Kind)
		return st
	}
	path := configfile.ParsePath(fl.Pointer)
	v, present := d.Get(path)
	st.Value, st.Present = v, present
	switch {
	case !present && fl.Default != nil && d.CanCreate(path):
		// The program has not written this one yet, but the schema knows what the
		// program would use and the key can be added, so it is editable with that
		// value showing.
		st.Value = fl.Default.V
		st.Unset = true
		st.Editable = true
	case !present:
		st.Reason = ReasonMissing
		st.Problem = "this setting is not in the file"
	case !fl.Kind.accepts(v.Kind):
		st.Reason = ReasonKindChanged
		st.Problem = fmt.Sprintf("the file holds %v here, but the schema expects %s", v.Kind, fl.Kind)
	default:
		st.Editable = true
	}
	return st
}

// appliesTo reports whether the field belongs to this release.
func (fl Field) appliesTo(version string, versions []string) bool {
	if version == "" || len(versions) == 0 {
		return true
	}
	at := indexOf(versions, version)
	if at < 0 {
		return true
	}
	if since := indexOf(versions, fl.SinceVersion); since >= 0 && at < since {
		return false
	}
	// Inclusive: UntilVersion is the last release that had the setting.
	if until := indexOf(versions, fl.UntilVersion); until >= 0 && at > until {
		return false
	}
	return true
}

func indexOf(list []string, s string) int {
	if s == "" {
		return -1
	}
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// ValidateAgainstVersions reports version bounds that name no known release,
// which Validate cannot see on its own. Kept separate because a schema is valid
// or not by itself, while this depends on the spec it is paired with.
func ValidateAgainstVersions(f File, versions []string) []error {
	var errs []error
	for _, sec := range f.Sections {
		for _, fl := range sec.Fields {
			for label, bound := range map[string]string{"sinceVersion": fl.SinceVersion, "untilVersion": fl.UntilVersion} {
				if bound != "" && indexOf(versions, bound) < 0 {
					errs = append(errs, fmt.Errorf("%s/%s: %s names %q, which is not a declared release", sec.Title, fl.Pointer, label, bound))
				}
			}
		}
	}
	return errs
}

// WriteField writes a value to the field's pointer, refusing anything the field
// does not permit: a value of the wrong kind, or one a select does not offer.
// Going through here rather than straight to the Doc is what keeps a host from
// having to re-implement the schema's own rules.
func WriteField(d configfile.Doc, fl Field, v configfile.Value) error {
	if fl.ReadOnly {
		return fmt.Errorf("%s: this setting is shown, not edited", fl.Pointer)
	}
	if !fl.Kind.editable() {
		return fmt.Errorf("%s: the schema declares an unknown kind, %q", fl.Pointer, fl.Kind)
	}
	if !fl.Kind.accepts(v.Kind) {
		return fmt.Errorf("%s: %w", fl.Pointer, configfile.ErrKindMismatch)
	}
	if err := checkFieldValue(fl, v); err != nil {
		return fmt.Errorf("%s: %w", fl.Pointer, err)
	}
	path := configfile.ParsePath(fl.Pointer)
	set := d.Set
	if fl.Kind == KindNumber {
		// The program does not distinguish 1 from 1.0, so the value is written in
		// the spelling the edit carries rather than the one the file happens to
		// hold. Only a grammar whose numbers are untyped offers this, and Validate
		// has already refused the declaration against any other.
		n, ok := d.(configfile.NumberSetter)
		if !ok {
			return fmt.Errorf("%s: this file's grammar gives its numbers a type, so kind number cannot be written to it", fl.Pointer)
		}
		set = n.SetNumber
	}
	err := set(path, v)
	if errors.Is(err, configfile.ErrNoSuchPath) && fl.Default != nil {
		// The program has not written this setting yet, and the schema knows what
		// the program would use, so the key is added. A field with no default is
		// not created: the schema does not claim to know the program's value for
		// it, and Resolve does not offer it for editing either. Create still
		// refuses to add the section or table it would live in.
		return d.Create(path, v)
	}
	return err
}

// checkFieldValue applies the field's own rules to a value: the options a select
// offers, the two values a toggle writes, the bounds of a number. Shared by
// WriteField and by Validate, so a default is held to exactly what a person's
// edit would be.
func checkFieldValue(fl Field, v configfile.Value) error {
	switch fl.Widget {
	case WidgetSelect, WidgetRadio:
		for _, o := range fl.Options {
			if o.V() == v {
				return nil
			}
		}
		return fmt.Errorf("%s is not one of the options this field offers", v.Display())
	case WidgetToggle, WidgetCheckbox:
		if fl.On != nil && fl.Off != nil && v != fl.On.V && v != fl.Off.V {
			return fmt.Errorf("a toggle writes %s or %s, not %s", fl.On.V.Display(), fl.Off.V.Display(), v.Display())
		}
	case WidgetNumber, WidgetSlider:
		n := v.Float
		if v.Kind == configfile.KindInt {
			n = float64(v.Int)
		}
		if fl.Min != nil && n < *fl.Min {
			return fmt.Errorf("%s is below the minimum %v", v.Display(), *fl.Min)
		}
		if fl.Max != nil && n > *fl.Max {
			return fmt.Errorf("%s is above the maximum %v", v.Display(), *fl.Max)
		}
	}
	return nil
}
