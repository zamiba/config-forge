package schema

import (
	"errors"
	"strings"
	"testing"

	"github.com/zamiba/config-forge/configfile"
)

// A writer that drops the decimal point on a whole number is why kind number
// exists. C#'s System.Text.Json writes a float of 1.0f as `1`, and C++'s
// operator<< does the same, so the setting sits in the file as an int at exactly
// its own default and as a float the moment anyone moves it.
const csharpSettings = `{
  "CdPath": "",
  "MasterVolume": 1,
  "Muted": false
}
`

func numberField() Field {
	min, max, step := 0.0, 1.0, 0.05
	return Field{
		Pointer: "MasterVolume", Kind: KindNumber, Widget: WidgetSlider,
		Label: "Master volume", Min: &min, Max: &max, Step: &step,
		Default: &Value{V: configfile.Float(1)},
	}
}

func numberFile(format configfile.Format) File {
	return File{
		Title: "Settings", Path: "settings.json", Format: format,
		Sections: []Section{{Title: "Audio", Fields: []Field{numberField()}}},
	}
}

func TestNumberKindValidatesOnlyWhereNumbersAreUntyped(t *testing.T) {
	if errs := Validate(numberFile(configfile.FormatJSON)); len(errs) > 0 {
		t.Errorf("json numbers are untyped: %v", errs)
	}
	if errs := Validate(numberFile(configfile.FormatINI)); len(errs) > 0 {
		t.Errorf("ini numbers are untyped: %v", errs)
	}
	// Godot's variants and Lua's numbers carry their own types, so there a
	// setting is an int or a float and the schema has to say which.
	for _, format := range []configfile.Format{configfile.FormatGodot, configfile.FormatLuaTable} {
		errs := Validate(numberFile(format))
		if len(errs) == 0 {
			t.Errorf("%s: kind number should be refused", format)
			continue
		}
		if !strings.Contains(errs[0].Error(), "untyped") {
			t.Errorf("%s: %v", format, errs[0])
		}
	}
}

// The whole point: a field of kind number is editable whichever way the file
// spells what it holds.
func TestNumberKindReadsEitherSpelling(t *testing.T) {
	for name, in := range map[string]string{
		"whole":   csharpSettings,
		"decimal": strings.Replace(csharpSettings, `"MasterVolume": 1`, `"MasterVolume": 0.65`, 1),
	} {
		d, err := configfile.Parse([]byte(in), configfile.FormatJSON)
		if err != nil {
			t.Fatal(err)
		}
		st := Resolve(d, numberFile(configfile.FormatJSON), "", nil)
		f := st[0].Fields[0]
		if !f.Editable || f.Reason != ReasonNone {
			t.Errorf("%s: not editable (%s: %s)", name, f.Reason, f.Problem)
		}
	}
	// A float field over the same file is the behaviour kind number is there to
	// avoid, and it is still the right behaviour for a program that cares.
	fl := numberField()
	fl.Kind = KindFloat
	file := numberFile(configfile.FormatJSON)
	file.Sections[0].Fields[0] = fl
	d, _ := configfile.Parse([]byte(csharpSettings), configfile.FormatJSON)
	if f := Resolve(d, file, "", nil)[0].Fields[0]; f.Editable || f.Reason != ReasonKindChanged {
		t.Errorf("a float field over `1` should report kindChanged, got %q", f.Reason)
	}
}

func TestNumberKindWritesTheSpellingTheEditCarries(t *testing.T) {
	d, err := configfile.Parse([]byte(csharpSettings), configfile.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	fl := numberField()
	// A decimal replacing the whole number the writer left behind.
	if err := WriteField(d, fl, configfile.Float(0.65)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(d.Bytes()), `"MasterVolume": 0.65`) {
		t.Errorf("got:\n%s", d.Bytes())
	}
	// And back the other way, to a whole number written as an int.
	if err := WriteField(d, fl, configfile.Int(1)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(d.Bytes()), `"MasterVolume": 1`) {
		t.Errorf("got:\n%s", d.Bytes())
	}
	// Nothing else in the file moved.
	back, err := configfile.Parse(d.Bytes(), configfile.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := back.Get(configfile.ParsePath("Muted")); v.Kind != configfile.KindBool || v.Bool {
		t.Errorf("Muted = %+v", v)
	}
	// The field's own bounds still apply.
	if err := WriteField(d, fl, configfile.Float(2)); err == nil {
		t.Error("2 is above the maximum and should be refused")
	}
	// A value that is not a number at all is still refused.
	if err := WriteField(d, fl, configfile.String("loud")); !errors.Is(err, configfile.ErrKindMismatch) {
		t.Errorf("err = %v, want ErrKindMismatch", err)
	}
}

// The same thing in the other untyped grammar: interface.ini holds
// TextureFilterStrength as 1 at its default and 0.75 once it is moved.
func TestNumberKindInAnIniFile(t *testing.T) {
	d, err := configfile.Parse([]byte("[RecompOne]\nTextureFilterStrength=1\n"), configfile.FormatINI)
	if err != nil {
		t.Fatal(err)
	}
	min, max := 0.0, 1.0
	fl := Field{
		Pointer: "RecompOne.TextureFilterStrength", Kind: KindNumber,
		Widget: WidgetSlider, Label: "Filter strength", Min: &min, Max: &max,
		Default: &Value{V: configfile.Float(0.5)},
	}
	if err := WriteField(d, fl, configfile.Float(0.75)); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != "[RecompOne]\nTextureFilterStrength=0.75\n" {
		t.Errorf("got %q", got)
	}
}

// A grammar that types its numbers refuses the write rather than guessing, even
// if a schema reached WriteField without having been validated.
func TestNumberKindIsRefusedByATypedGrammar(t *testing.T) {
	d, err := configfile.Parse([]byte("[audio]\nmaster=1\n"), configfile.FormatGodot)
	if err != nil {
		t.Fatal(err)
	}
	fl := numberField()
	fl.Pointer = "audio.master"
	if err := WriteField(d, fl, configfile.Float(0.5)); err == nil {
		t.Error("a typed grammar should refuse kind number")
	} else if !strings.Contains(err.Error(), "type") {
		t.Errorf("err = %v", err)
	}
}
