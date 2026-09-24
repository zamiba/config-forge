package schema

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zamiba/config-forge/configfile"
)

func defaultSchema(t *testing.T) File {
	t.Helper()
	const in = `{
	  "title": "Settings", "path": "a.cfg", "format": "godot",
	  "sections": [{ "title": "Video", "fields": [
	    { "pointer": "video.mode", "kind": "int", "widget": "select", "label": "Mode", "default": 0,
	      "options": [{ "label": "Windowed", "value": 0 }, { "label": "Fullscreen", "value": 3 }] },
	    { "pointer": "video.vsync", "kind": "int", "widget": "toggle", "label": "V-sync", "on": 1, "off": 0, "default": 1 },
	    { "pointer": "video.gamma", "kind": "float", "widget": "slider", "label": "Gamma", "min": 0, "max": 2, "default": 1.0 },
	    { "pointer": "video.nodefault", "kind": "int", "widget": "number", "label": "No default" },
	    { "pointer": "network.port", "kind": "int", "widget": "number", "label": "In a missing section", "default": 7777 }
	  ]}]
	}`
	var f File
	if err := json.Unmarshal([]byte(in), &f); err != nil {
		t.Fatal(err)
	}
	if errs := Validate(f); len(errs) > 0 {
		t.Fatalf("schema should be valid: %v", errs)
	}
	return f
}

func stateOf(t *testing.T, secs []SectionState, pointer string) FieldState {
	t.Helper()
	for _, sec := range secs {
		for _, fl := range sec.Fields {
			if fl.Pointer == pointer {
				return fl
			}
		}
	}
	t.Fatalf("no field %s", pointer)
	return FieldState{}
}

// A setting the program has not written yet is editable when the schema knows the
// program's own default and the key can be added.
func TestUnsetFieldUsesItsDefault(t *testing.T) {
	f := defaultSchema(t)
	d, err := configfile.Parse([]byte("[video]\nmode=3\n"), configfile.FormatGodot)
	if err != nil {
		t.Fatal(err)
	}
	secs := Resolve(d, f, "", nil)

	// Present: the file's value wins over the default.
	mode := stateOf(t, secs, "video.mode")
	if mode.Unset || !mode.Editable || mode.Value.Int != 3 {
		t.Errorf("mode = %+v", mode)
	}

	// Absent, with a default and a section to put it in: editable and marked.
	vsync := stateOf(t, secs, "video.vsync")
	if !vsync.Unset || !vsync.Editable || vsync.Present || vsync.Value.Int != 1 || vsync.Reason != ReasonNone {
		t.Errorf("vsync = %+v", vsync)
	}
	gamma := stateOf(t, secs, "video.gamma")
	if !gamma.Unset || gamma.Value.Float != 1 || gamma.Value.Kind != configfile.KindFloat {
		t.Errorf("gamma = %+v", gamma)
	}

	// Absent with no default: unavailable, as before.
	none := stateOf(t, secs, "video.nodefault")
	if none.Unset || none.Editable || none.Reason != ReasonMissing {
		t.Errorf("nodefault = %+v", none)
	}

	// Absent with a default but no section to add it to: still unavailable,
	// because creating the section is what this refuses to do.
	missing := stateOf(t, secs, "network.port")
	if missing.Unset || missing.Editable || missing.Reason != ReasonMissing {
		t.Errorf("network.port = %+v", missing)
	}
}

// Saving an unset field adds the key, in its own section, leaving the rest alone.
func TestWritingAnUnsetFieldAddsTheKey(t *testing.T) {
	f := defaultSchema(t)
	const in = "[video]\nmode=3\n\n[audio]\nmaster=10\n"
	d, err := configfile.Parse([]byte(in), configfile.FormatGodot)
	if err != nil {
		t.Fatal(err)
	}
	vsync := stateOf(t, Resolve(d, f, "", nil), "video.vsync")

	if err := WriteField(d, vsync.Field, configfile.Int(0)); err != nil {
		t.Fatal(err)
	}
	const want = "[video]\nmode=3\nvsync=0\n\n[audio]\nmaster=10\n"
	if got := string(d.Bytes()); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	// And it is an ordinary setting from then on.
	after := stateOf(t, Resolve(d, f, "", nil), "video.vsync")
	if after.Unset || !after.Present || after.Value.Int != 0 {
		t.Errorf("after writing: %+v", after)
	}
}

// A field in a section the file lacks cannot be written even by force, because
// adding the section is what could break the program's own loader.
func TestWritingIntoAMissingSectionIsRefused(t *testing.T) {
	f := defaultSchema(t)
	d, err := configfile.Parse([]byte("[video]\nmode=3\n"), configfile.FormatGodot)
	if err != nil {
		t.Fatal(err)
	}
	var port Field
	for _, sec := range f.Sections {
		for _, fl := range sec.Fields {
			if fl.Pointer == "network.port" {
				port = fl
			}
		}
	}
	if err := WriteField(d, port, configfile.Int(1)); err == nil {
		t.Error("writing into a section the file lacks should be refused")
	}
	if got := string(d.Bytes()); got != "[video]\nmode=3\n" {
		t.Errorf("a refused write changed the file: %q", got)
	}
}

// A default is held to the same rules as a person's edit, or it would be offered
// and then refused on save.
func TestValidateChecksTheDefaultAgainstTheFieldsRules(t *testing.T) {
	for name, in := range map[string]string{
		"not an option":  `{"pointer":"a.b","kind":"int","widget":"select","label":"L","default":9,"options":[{"label":"A","value":1},{"label":"B","value":2}]}`,
		"above the max":  `{"pointer":"a.b","kind":"int","widget":"slider","label":"L","min":0,"max":7,"default":9}`,
		"not on or off":  `{"pointer":"a.b","kind":"int","widget":"toggle","label":"L","on":1,"off":0,"default":5}`,
		"wrong kind":     `{"pointer":"a.b","kind":"int","widget":"number","label":"L","default":"three"}`,
		"on a read-only": `{"pointer":"a.b","kind":"opaque","label":"L","readOnly":true,"default":1}`,
	} {
		var f File
		body := `{"title":"T","path":"a.cfg","format":"godot","sections":[{"title":"S","fields":[` + in + `]}]}`
		if err := json.Unmarshal([]byte(body), &f); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		errs := Validate(f)
		if len(errs) == 0 {
			t.Errorf("%s: should have been reported", name)
			continue
		}
		if !strings.Contains(strings.ToLower(errs[0].Error()), "default") {
			t.Errorf("%s: the error should mention the default: %v", name, errs[0])
		}
	}
	// A default that satisfies the rules is fine.
	var ok File
	body := `{"title":"T","path":"a.cfg","format":"godot","sections":[{"title":"S","fields":[
	  {"pointer":"a.b","kind":"int","widget":"slider","label":"L","min":0,"max":7,"default":7}]}]}`
	if err := json.Unmarshal([]byte(body), &ok); err != nil {
		t.Fatal(err)
	}
	if errs := Validate(ok); len(errs) > 0 {
		t.Errorf("a valid default was reported: %v", errs)
	}
}

// The same behaviour through the other grammar, where the container is a table.
func TestUnsetFieldInALuaTable(t *testing.T) {
	const in = `{
	  "title": "Options", "path": "options.lua", "format": "luaTable",
	  "sections": [{ "title": "Touch", "fields": [
	    { "pointer": "touchControls.enabled", "kind": "bool", "widget": "toggle", "label": "Touch", "default": true },
	    { "pointer": "absent.thing", "kind": "bool", "widget": "toggle", "label": "Absent", "default": true }
	  ]}]
	}`
	var f File
	if err := json.Unmarshal([]byte(in), &f); err != nil {
		t.Fatal(err)
	}
	d, err := configfile.Parse([]byte("return {\n  touchControls = {},\n}\n"), configfile.FormatLuaTable)
	if err != nil {
		t.Fatal(err)
	}
	secs := Resolve(d, f, "", nil)
	enabled := stateOf(t, secs, "touchControls.enabled")
	if !enabled.Unset || !enabled.Editable || !enabled.Value.Bool {
		t.Errorf("enabled = %+v", enabled)
	}
	if absent := stateOf(t, secs, "absent.thing"); absent.Editable || absent.Reason != ReasonMissing {
		t.Errorf("absent.thing = %+v", absent)
	}
	if err := WriteField(d, enabled.Field, configfile.Bool(false)); err != nil {
		t.Fatal(err)
	}
	if got, want := string(d.Bytes()), "return {\n  touchControls = { enabled = false },\n}\n"; got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// A field with no default is never created, even by a caller reaching past the
// page: the schema does not claim to know what the program would use for it.
func TestAFieldWithoutADefaultIsNotCreated(t *testing.T) {
	f := defaultSchema(t)
	d, err := configfile.Parse([]byte("[video]\nmode=3\n"), configfile.FormatGodot)
	if err != nil {
		t.Fatal(err)
	}
	var none Field
	for _, sec := range f.Sections {
		for _, fl := range sec.Fields {
			if fl.Pointer == "video.nodefault" {
				none = fl
			}
		}
	}
	if err := WriteField(d, none, configfile.Int(1)); err == nil {
		t.Error("a field with no default should not be created")
	}
	if got := string(d.Bytes()); got != "[video]\nmode=3\n" {
		t.Errorf("the file changed: %q", got)
	}
}
