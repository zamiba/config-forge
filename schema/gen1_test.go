package schema

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/zamiba/config-forge/configfile"
)

// Gen1Recomp keeps its settings as a Lua data file, which is a different grammar
// from SMB:Remastered's Godot ConfigFile and exercises the same schema layer —
// including a field inside a nested table.
func loadGen1(t *testing.T) (File, configfile.Doc, string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/gen1-options.json")
	if err != nil {
		t.Fatal(err)
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.ReadFile("testdata/gen1-options.lua")
	if err != nil {
		t.Fatal(err)
	}
	d, err := configfile.Parse(cfg, f.Format)
	if err != nil {
		t.Fatal(err)
	}
	return f, d, string(cfg)
}

func TestGen1SchemaValidatesAndResolves(t *testing.T) {
	f, d, _ := loadGen1(t)
	if errs := Validate(f); len(errs) > 0 {
		for _, err := range errs {
			t.Errorf("validate: %v", err)
		}
	}
	n := 0
	for _, sec := range Resolve(d, f, "", nil) {
		for _, fl := range sec.Fields {
			n++
			if !fl.Editable {
				t.Errorf("%s/%s not editable: %s", sec.Title, fl.Pointer, fl.Problem)
			}
		}
	}
	if n != 21 {
		t.Errorf("resolved %d fields, want 21", n)
	}
}

// One schema layer, two grammars: the same field rules apply to a Lua file.
func TestGen1WritesThroughTheSchema(t *testing.T) {
	f, d, before := loadGen1(t)
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

	if err := WriteField(d, get("battleStyle"), configfile.String("set")); err != nil {
		t.Fatal(err)
	}
	if err := WriteField(d, get("musicVol"), configfile.Int(3)); err != nil {
		t.Fatal(err)
	}
	// A field addressed through a nested table.
	if err := WriteField(d, get("touchControls.enabled"), configfile.Bool(false)); err != nil {
		t.Fatal(err)
	}

	want := before
	want = strings.Replace(want, `battleStyle = "shift"`, `battleStyle = "set"`, 1)
	want = strings.Replace(want, "musicVol = 7", "musicVol = 3", 1)
	want = strings.Replace(want, "enabled = true", "enabled = false", 1)
	if got := string(d.Bytes()); got != want {
		t.Errorf("three edits changed more than their values:\ngot:\n%s", got)
	}

	// The schema's rules hold here exactly as they do for the other grammar.
	if err := WriteField(d, get("battleStyle"), configfile.String("rotate")); err == nil {
		t.Error("a value the select does not offer should be refused")
	}
	if err := WriteField(d, get("musicVol"), configfile.Int(8)); err == nil {
		t.Error("above the slider maximum should be refused")
	}
	if err := WriteField(d, get("animations"), configfile.Int(1)); err == nil {
		t.Error("an int into a bool field should be refused")
	}
}

// The tables this schema never mentions — mods, cartOptions, modOptions — must
// come through an edit completely untouched, because they are the program's and
// losing them is the failure this package exists to prevent.
func TestGen1LeavesTheProgramsOwnStateAlone(t *testing.T) {
	f, d, _ := loadGen1(t)
	var vol Field
	for _, sec := range f.Sections {
		for _, fl := range sec.Fields {
			if fl.Pointer == "musicVol" {
				vol = fl
			}
		}
	}
	if err := WriteField(d, vol, configfile.Int(0)); err != nil {
		t.Fatal(err)
	}
	out := string(d.Bytes())
	for _, untouched := range []string{
		"cartOptions = {},",
		"modOptions = {},",
		"mods = {},",
	} {
		if !strings.Contains(out, untouched) {
			t.Errorf("%q did not survive the edit", untouched)
		}
	}
	// And nothing the schema does not name became addressable-but-broken.
	if v, ok := d.Get(configfile.ParsePath("cartOptions")); !ok || v.Raw != "{}" {
		t.Errorf("cartOptions = %+v", v)
	}
}
