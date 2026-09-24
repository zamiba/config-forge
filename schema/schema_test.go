package schema

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/zamiba/config-forge/configfile"
)

func load(t *testing.T) (File, configfile.Doc, string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/smbr-settings.json")
	if err != nil {
		t.Fatal(err)
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.ReadFile("testdata/smbr-settings.cfg")
	if err != nil {
		t.Fatal(err)
	}
	d, err := configfile.Parse(cfg, f.Format)
	if err != nil {
		t.Fatal(err)
	}
	return f, d, string(cfg)
}

// The schema for a real program, against a real file: it validates, and every
// field it declares resolves to something editable.
func TestRealSchemaValidatesAndResolves(t *testing.T) {
	f, d, _ := load(t)
	if errs := Validate(f); len(errs) > 0 {
		for _, err := range errs {
			t.Errorf("validate: %v", err)
		}
	}
	sections := Resolve(d, f, "1.0.2", []string{"1.0.2"})
	if len(sections) != 5 {
		t.Fatalf("got %d sections, want 5", len(sections))
	}
	for _, sec := range sections {
		for _, fl := range sec.Fields {
			if !fl.Editable {
				t.Errorf("%s/%s not editable: %s", sec.Title, fl.Pointer, fl.Problem)
			}
		}
	}
	// Values arrive as the file holds them, ready for a control to be drawn.
	for _, sec := range sections {
		for _, fl := range sec.Fields {
			switch fl.Pointer {
			case "video.mode":
				if fl.Value.Int != 0 {
					t.Errorf("video.mode = %v", fl.Value.Display())
				}
			case "game.campaign":
				if fl.Value.Str != "SMB1" {
					t.Errorf("campaign = %q", fl.Value.Str)
				}
			case "controller.deadzone":
				if fl.Value.Float != 0.5 {
					t.Errorf("deadzone = %v", fl.Value.Float)
				}
			case "editor.seen_guide":
				if fl.Value.Bool {
					t.Error("seen_guide should be false")
				}
			}
		}
	}
}

// An int used as a boolean is written as the int, never as true.
func TestToggleOnAnIntWritesTheInt(t *testing.T) {
	f, d, before := load(t)
	var vsync Field
	for _, sec := range f.Sections {
		for _, fl := range sec.Fields {
			if fl.Pointer == "video.vsync" {
				vsync = fl
			}
		}
	}
	if vsync.On == nil || vsync.On.V.Kind != configfile.KindInt {
		t.Fatalf("vsync on = %+v", vsync.On)
	}
	if err := WriteField(d, vsync, vsync.Off.V); err != nil {
		t.Fatal(err)
	}
	if got, want := string(d.Bytes()), strings.Replace(before, "vsync=1\n", "vsync=0\n", 1); got != want {
		t.Errorf("writing a toggle changed more than its value:\ngot  %q", got)
	}
	// And true is refused, because the program stores an int here.
	if err := WriteField(d, vsync, configfile.Bool(true)); !errors.Is(err, configfile.ErrKindMismatch) {
		t.Errorf("bool into an int field: %v", err)
	}
}

// The schema's own rules are enforced in one place, so a host cannot skip them.
func TestWriteFieldEnforcesTheFieldsRules(t *testing.T) {
	f, d, _ := load(t)
	get := func(p string) Field {
		for _, sec := range f.Sections {
			for _, fl := range sec.Fields {
				if fl.Pointer == p {
					return fl
				}
			}
		}
		t.Fatalf("no field %s", p)
		return Field{}
	}

	if err := WriteField(d, get("game.campaign"), configfile.String("SMB3")); err == nil {
		t.Error("a value a select does not offer should be refused")
	}
	if err := WriteField(d, get("game.campaign"), configfile.String("SMBLL")); err != nil {
		t.Errorf("an offered value should be accepted: %v", err)
	}
	if err := WriteField(d, get("audio.master"), configfile.Int(11)); err == nil {
		t.Error("above a slider's maximum should be refused")
	}
	if err := WriteField(d, get("audio.master"), configfile.Int(-1)); err == nil {
		t.Error("below a slider's minimum should be refused")
	}
	if err := WriteField(d, get("audio.master"), configfile.Int(0)); err != nil {
		t.Errorf("the minimum itself should be accepted: %v", err)
	}
	if err := WriteField(d, get("video.vsync"), configfile.Int(7)); err == nil {
		t.Error("a toggle should write only its on or off value")
	}
}

// A schema that has fallen behind its program must say so rather than write.
func TestResolveReportsAFileThatHasMovedOn(t *testing.T) {
	f, _, _ := load(t)
	// vsync became a bool upstream; window_size was never a scalar at all.
	d, err := configfile.Parse([]byte("[video]\nmode=0\nmultiplier=3\nvsync=true\n"), configfile.FormatGodot)
	if err != nil {
		t.Fatal(err)
	}
	var problems []string
	for _, sec := range Resolve(d, f, "", nil) {
		for _, fl := range sec.Fields {
			if !fl.Editable {
				problems = append(problems, fl.Pointer+": "+fl.Problem)
			}
		}
	}
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "video.vsync: the file holds bool here") {
		t.Errorf("a changed kind should be reported:\n%s", joined)
	}
	if !strings.Contains(joined, "audio.master: this setting is not in the file") {
		t.Errorf("a missing setting should be reported:\n%s", joined)
	}
	// Reporting must not have written anything.
	if got := string(d.Bytes()); got != "[video]\nmode=0\nmultiplier=3\nvsync=true\n" {
		t.Errorf("resolving modified the file: %q", got)
	}
}

func TestVersionBoundsLimitFields(t *testing.T) {
	// Built here rather than taken from the catalog-shaped testdata: a real schema
	// covers every release its spec declares, and a bound exists only for a setting
	// that genuinely arrived or went away, so the fixtures carry none to borrow.
	const in = `{
	  "title": "Settings", "path": "a.cfg", "format": "godot",
	  "sections": [{ "title": "S", "fields": [
	    { "pointer": "a.always", "kind": "int", "widget": "number", "label": "Always" },
	    { "pointer": "a.since", "kind": "int", "widget": "number", "label": "Since", "sinceVersion": "1.1" },
	    { "pointer": "a.until", "kind": "int", "widget": "number", "label": "Until", "untilVersion": "1.1" }
	  ]}]
	}`
	var f File
	if err := json.Unmarshal([]byte(in), &f); err != nil {
		t.Fatal(err)
	}
	if errs := Validate(f); len(errs) > 0 {
		t.Fatalf("schema should be valid: %v", errs)
	}
	d, err := configfile.Parse([]byte("[a]\nalways=1\nsince=1\nuntil=1\n"), configfile.FormatGodot)
	if err != nil {
		t.Fatal(err)
	}

	versions := []string{"1.0", "1.1", "1.2"}
	has := func(version, pointer string) bool {
		for _, sec := range Resolve(d, f, version, versions) {
			for _, fl := range sec.Fields {
				if fl.Pointer == pointer {
					return true
				}
			}
		}
		return false
	}
	for _, tc := range []struct {
		version, pointer string
		want             bool
	}{
		{"1.0", "a.always", true}, {"1.2", "a.always", true},
		{"1.0", "a.since", false}, {"1.1", "a.since", true}, {"1.2", "a.since", true},
		{"1.0", "a.until", true}, {"1.1", "a.until", true}, {"1.2", "a.until", false},
	} {
		if got := has(tc.version, tc.pointer); got != tc.want {
			t.Errorf("%s on %s = %v, want %v", tc.pointer, tc.version, got, tc.want)
		}
	}
	// An unknown version cannot be placed, so nothing is filtered out by guesswork.
	if !has("2.0", "a.since") {
		t.Error("a version not in the list should not have its fields filtered")
	}

	if errs := ValidateAgainstVersions(f, versions); len(errs) > 0 {
		t.Errorf("bounds naming known releases should pass: %v", errs)
	}
	if errs := ValidateAgainstVersions(f, []string{"9.9"}); len(errs) != 2 {
		t.Errorf("both bounds name no known release, got %v", errs)
	}
}

// Validate reports every fault at once, so an author fixing a catalog entry sees
// the whole list rather than the first line.
func TestValidateCatchesBadSchemas(t *testing.T) {
	for name, in := range map[string]string{
		"unknown format":      `{"path":"a.cfg","format":"ini","sections":[{"title":"T","fields":[{"pointer":"a.b","kind":"int","widget":"number","label":"L"}]}]}`,
		"no path":             `{"format":"godot","sections":[{"title":"T","fields":[{"pointer":"a.b","kind":"int","widget":"number","label":"L"}]}]}`,
		"no sections":         `{"path":"a.cfg","format":"godot","sections":[]}`,
		"no label":            `{"path":"a.cfg","format":"godot","sections":[{"title":"T","fields":[{"pointer":"a.b","kind":"int","widget":"number"}]}]}`,
		"no widget":           `{"path":"a.cfg","format":"godot","sections":[{"title":"T","fields":[{"pointer":"a.b","kind":"int","label":"L"}]}]}`,
		"opaque kind":         `{"path":"a.cfg","format":"godot","sections":[{"title":"T","fields":[{"pointer":"a.b","kind":"array","widget":"text","label":"L"}]}]}`,
		"duplicate pointer":   `{"path":"a.cfg","format":"godot","sections":[{"title":"T","fields":[{"pointer":"a.b","kind":"int","widget":"number","label":"L"},{"pointer":"a.b","kind":"int","widget":"number","label":"M"}]}]}`,
		"int toggle, no on":   `{"path":"a.cfg","format":"godot","sections":[{"title":"T","fields":[{"pointer":"a.b","kind":"int","widget":"toggle","label":"L"}]}]}`,
		"bool toggle with on": `{"path":"a.cfg","format":"godot","sections":[{"title":"T","fields":[{"pointer":"a.b","kind":"bool","widget":"toggle","label":"L","on":1,"off":0}]}]}`,
		"on equals off":       `{"path":"a.cfg","format":"godot","sections":[{"title":"T","fields":[{"pointer":"a.b","kind":"int","widget":"toggle","label":"L","on":1,"off":1}]}]}`,
		"select, one option":  `{"path":"a.cfg","format":"godot","sections":[{"title":"T","fields":[{"pointer":"a.b","kind":"int","widget":"select","label":"L","options":[{"label":"A","value":1}]}]}]}`,
		"option wrong kind":   `{"path":"a.cfg","format":"godot","sections":[{"title":"T","fields":[{"pointer":"a.b","kind":"int","widget":"select","label":"L","options":[{"label":"A","value":1},{"label":"B","value":"two"}]}]}]}`,
		"slider, no bounds":   `{"path":"a.cfg","format":"godot","sections":[{"title":"T","fields":[{"pointer":"a.b","kind":"int","widget":"slider","label":"L"}]}]}`,
		"min above max":       `{"path":"a.cfg","format":"godot","sections":[{"title":"T","fields":[{"pointer":"a.b","kind":"int","widget":"slider","label":"L","min":5,"max":1}]}]}`,
		"text on an int":      `{"path":"a.cfg","format":"godot","sections":[{"title":"T","fields":[{"pointer":"a.b","kind":"int","widget":"text","label":"L"}]}]}`,
		"number on a string":  `{"path":"a.cfg","format":"godot","sections":[{"title":"T","fields":[{"pointer":"a.b","kind":"string","widget":"number","label":"L"}]}]}`,
		"options on a toggle": `{"path":"a.cfg","format":"godot","sections":[{"title":"T","fields":[{"pointer":"a.b","kind":"bool","widget":"toggle","label":"L","options":[{"label":"A","value":true}]}]}]}`,
	} {
		var f File
		if err := json.Unmarshal([]byte(in), &f); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if errs := Validate(f); len(errs) == 0 {
			t.Errorf("%s: should have been reported as invalid", name)
		}
	}
}

// A schema round-trips through JSON, so a host can read one, hand it back, and
// get the same thing — including which numbers are ints.
func TestSchemaValuesKeepTheirKindThroughJSON(t *testing.T) {
	const in = `{"label":"x","value":1}`
	var o Option
	if err := json.Unmarshal([]byte(in), &o); err != nil {
		t.Fatal(err)
	}
	if o.V().Kind != configfile.KindInt {
		t.Errorf("1 decoded as %v, want int", o.V().Kind)
	}
	var f Option
	if err := json.Unmarshal([]byte(`{"label":"x","value":1.5}`), &f); err != nil {
		t.Fatal(err)
	}
	if f.V().Kind != configfile.KindFloat {
		t.Errorf("1.5 decoded as %v, want float", f.V().Kind)
	}
	out, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != in {
		t.Errorf("round trip gave %s, want %s", out, in)
	}
}

// A read-only field is shown whatever the file holds, including a kind nothing
// here can rewrite — which is the usual reason to declare one.
func TestReadOnlyFieldIsShownNotEdited(t *testing.T) {
	f := File{Title: "Video", Path: "a.cfg", Format: configfile.FormatGodot, Sections: []Section{{
		Title: "Window",
		Fields: []Field{
			{Pointer: "video.window_size", Kind: KindOpaque, Label: "Window size", ReadOnly: true},
			{Pointer: "video.mode", Kind: KindInt, Widget: WidgetNumber, Label: "Mode"},
		},
	}}}
	if errs := Validate(f); len(errs) > 0 {
		t.Fatalf("a read-only opaque field is legitimate: %v", errs)
	}
	d, err := configfile.Parse([]byte("[video]\nwindow_size=[1024, 960]\nmode=0\n"), configfile.FormatGodot)
	if err != nil {
		t.Fatal(err)
	}
	fields := Resolve(d, f, "", nil)[0].Fields
	ro := fields[0]
	if !ro.ReadOnly || ro.Editable || ro.Reason != ReasonNone {
		t.Errorf("read-only field resolved as %+v", ro)
	}
	if ro.Value.Display() != "[1024, 960]" {
		t.Errorf("its value should still be shown: %q", ro.Value.Display())
	}
	if !fields[1].Editable {
		t.Error("the editable field beside it should be unaffected")
	}
	if err := WriteField(d, ro.Field, configfile.String("x")); err == nil {
		t.Error("writing a read-only field should be refused")
	}
}

// Everything that configures a control is a mistake on a field that has none:
// an author who set one expected the field to be editable.
func TestReadOnlyRejectsControlSettings(t *testing.T) {
	mk := func(mut func(*Field)) File {
		fl := Field{Pointer: "a.b", Kind: KindOpaque, Label: "L", ReadOnly: true}
		mut(&fl)
		return File{Title: "T", Path: "a.cfg", Format: configfile.FormatGodot,
			Sections: []Section{{Title: "S", Fields: []Field{fl}}}}
	}
	one := 1.0
	for name, mut := range map[string]func(*Field){
		"widget":   func(f *Field) { f.Widget = WidgetText },
		"options":  func(f *Field) { f.Options = []Option{{Label: "A"}, {Label: "B"}} },
		"min":      func(f *Field) { f.Min = &one },
		"max":      func(f *Field) { f.Max = &one },
		"step":     func(f *Field) { f.Step = &one },
		"unit":     func(f *Field) { f.Unit = "fps" },
		"on":       func(f *Field) { v := Value{V: configfile.Int(1)}; f.On = &v },
		"bad kind": func(f *Field) { f.Kind = Kind("array") },
	} {
		if errs := Validate(mk(mut)); len(errs) == 0 {
			t.Errorf("%s on a read-only field should be reported", name)
		}
	}
	// And opaque is refused on an editable field, which is the inverse rule.
	f := File{Title: "T", Path: "a.cfg", Format: configfile.FormatGodot, Sections: []Section{{
		Title: "S", Fields: []Field{{Pointer: "a.b", Kind: KindOpaque, Widget: WidgetText, Label: "L"}},
	}}}
	if errs := Validate(f); len(errs) == 0 {
		t.Error("an editable field must not declare the opaque kind")
	}
}

// A unit belongs to the two numeric controls and nowhere else.
func TestUnitOnlyOnNumericFields(t *testing.T) {
	mk := func(w Widget, kind Kind) File {
		fl := Field{Pointer: "a.b", Kind: kind, Widget: w, Label: "L", Unit: "fps"}
		if w == WidgetSlider {
			lo, hi := 0.0, 10.0
			fl.Min, fl.Max = &lo, &hi
		}
		return File{Title: "T", Path: "a.cfg", Format: configfile.FormatGodot,
			Sections: []Section{{Title: "S", Fields: []Field{fl}}}}
	}
	for _, ok := range []File{mk(WidgetNumber, KindInt), mk(WidgetSlider, KindInt)} {
		if errs := Validate(ok); len(errs) > 0 {
			t.Errorf("a unit on a numeric field is fine: %v", errs)
		}
	}
	if errs := Validate(mk(WidgetText, KindString)); len(errs) == 0 {
		t.Error("a unit on a text field should be reported")
	}
}
