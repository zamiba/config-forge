package schema

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zamiba/config-forge/configfile"
)

// One object or an array of them, the same two spellings a spec's userDataPaths
// accepts, because most programs need one reference copy and one whose config
// changes shape between releases needs one per shape.
func TestReferenceReadsBothSpellings(t *testing.T) {
	var one File
	if err := json.Unmarshal([]byte(`{
	  "title": "Options", "path": "install/options.lua", "format": "luaTable",
	  "reference": { "file": "options.lua.example" },
	  "sections": []
	}`), &one); err != nil {
		t.Fatal(err)
	}
	if len(one.Reference) != 1 || one.Reference[0].File != "options.lua.example" {
		t.Errorf("single object: %+v", one.Reference)
	}

	var many File
	if err := json.Unmarshal([]byte(`{
	  "title": "Settings", "path": "install/settings.cfg", "format": "godot",
	  "reference": [
	    { "file": "settings.cfg.1.0.2.example", "untilVersion": "1.0.2" },
	    { "file": "settings.cfg.1.1 RC4.example", "sinceVersion": "1.1 RC4" }
	  ],
	  "sections": []
	}`), &many); err != nil {
		t.Fatal(err)
	}
	if len(many.Reference) != 2 || many.Reference[1].SinceVersion != "1.1 RC4" {
		t.Errorf("array: %+v", many.Reference)
	}

	// And the single case round-trips back to an object rather than a one-element
	// array, so a schema written by hand stays written that way.
	out, err := json.Marshal(one.Reference)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "{") {
		t.Errorf("marshalled as %s, want an object", out)
	}
}

func TestReferenceForPicksByRelease(t *testing.T) {
	f := File{Reference: References{
		{File: "old.example", UntilVersion: "1.0.2"},
		{File: "new.example", SinceVersion: "1.1 RC4"},
	}}
	versions := []string{"1.0.2", "1.1 RC4"}
	for version, want := range map[string]string{"1.0.2": "old.example", "1.1 RC4": "new.example"} {
		r, ok := f.ReferenceFor(version, versions)
		if !ok || r.File != want {
			t.Errorf("%s → %q (found %v), want %q", version, r.File, ok, want)
		}
	}

	// An unbounded one is the author saying every release has this shape, and placed
	// last it reads as the fallback it is.
	g := File{Reference: References{
		{File: "old.example", UntilVersion: "1.0.2"},
		{File: "any.example"},
	}}
	if r, _ := g.ReferenceFor("1.1 RC4", versions); r.File != "any.example" {
		t.Errorf("fallback → %q", r.File)
	}
	if r, _ := g.ReferenceFor("1.0.2", versions); r.File != "old.example" {
		t.Errorf("declaration order should win: %q", r.File)
	}

	// No reference at all is the ordinary case for a program that writes its whole
	// config every time.
	if _, ok := (File{}).ReferenceFor("1.0", versions); ok {
		t.Error("a schema with no reference should report none")
	}
}

func TestValidateRefusesAnUnusableReference(t *testing.T) {
	base := func(file string) File {
		return File{
			Title: "S", Path: "x.cfg", Format: configfile.FormatGodot,
			Reference: References{{File: file}},
			Sections: []Section{{Title: "S", Fields: []Field{
				{Pointer: "a.b", Kind: KindInt, Widget: WidgetNumber, Label: "B"},
			}}},
		}
	}
	for name, tc := range map[string]struct{ file, want string }{
		"empty":    {"", "has no file"},
		"a path":   {"sub/x.example", "bare name"},
		"a parent": {"../x.example", "bare name"},
		// LoadDir reads these as schemas, so this one would be parsed as a schema and
		// fail. A plain .json is fine now that the suffix is what marks a schema —
		// which matters, because a reference copy of a config that is itself JSON is
		// the ordinary case.
		"named like a schema": {"x.schema.json", "must not be named .schema.json"},
	} {
		errs := Validate(base(tc.file))
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), tc.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: errors = %v, want one mentioning %q", name, errs, tc.want)
		}
	}
	// A usable one is not an error, and neither is a plain .json: a reference copy of
	// a config file that is itself JSON is what several of these programs keep.
	for _, name := range []string{"x.cfg.example", "shipofharkinian.json.example", "settings.json"} {
		if errs := Validate(base(name)); len(errs) > 0 {
			t.Errorf("%s: %v", name, errs)
		}
	}
	// The same file twice is a mistake worth naming.
	f := base("x.cfg.example")
	f.Reference = append(f.Reference, Reference{File: "x.cfg.example"})
	if errs := Validate(f); len(errs) == 0 {
		t.Error("the same reference named twice should be reported")
	}
}

// A reference's version bounds are held to the same rule a field's are: a bound
// naming no declared release would silently apply to nothing.
func TestValidateAgainstVersionsChecksReferenceBounds(t *testing.T) {
	f := File{
		Title: "S", Path: "x.cfg", Format: configfile.FormatGodot,
		Reference: References{{File: "x.cfg.example", SinceVersion: "9.9"}},
		Sections: []Section{{Title: "S", Fields: []Field{
			{Pointer: "a.b", Kind: KindInt, Widget: WidgetNumber, Label: "B"},
		}}},
	}
	errs := ValidateAgainstVersions(f, []string{"1.0", "2.0"})
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "9.9") {
		t.Errorf("errs = %v", errs)
	}
}
