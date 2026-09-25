package schema

import (
	"strings"
	"testing"

	"github.com/zamiba/config-forge/configfile"
)

// A libultraship config as it stands after a few runs: the Window block, which the
// program always writes, and one cvar somebody has touched. Every other setting's
// section is simply not there — CVars.gSettings.Volume does not exist, so nothing
// inside it could be added on its own.
const partial = `{
    "CVars": {
        "gSettings": {
            "VsyncEnabled": 0
        }
    },
    "Window": {
        "Height": 720,
        "Width": 1280
    }
}
`

// The reference copy the catalog ships: the same file with every setting the schema
// names, at the value the program itself uses.
const reference = `{
    "CVars": {
        "gSettings": {
            "MSAAValue": 1,
            "VsyncEnabled": 1,
            "Volume": {
                "MainMusic": 100,
                "Master": 40
            }
        }
    },
    "Window": {
        "Fullscreen": {
            "Enabled": false
        },
        "Height": 480,
        "Width": 640
    }
}
`

func completeFile() File {
	return File{
		Title: "Settings", Path: "x.json", Format: configfile.FormatJSON,
		Sections: []Section{{Title: "Audio", Fields: []Field{
			{Pointer: "CVars.gSettings.Volume.Master", Kind: KindInt, Widget: WidgetNumber,
				Label: "Master", Default: &Value{V: configfile.Int(40)}},
			{Pointer: "CVars.gSettings.Volume.MainMusic", Kind: KindInt, Widget: WidgetNumber,
				Label: "Music", Default: &Value{V: configfile.Int(100)}},
			{Pointer: "CVars.gSettings.VsyncEnabled", Kind: KindInt, Widget: WidgetNumber,
				Label: "V-sync", Default: &Value{V: configfile.Int(1)}},
			{Pointer: "Window.Fullscreen.Enabled", Kind: KindBool, Widget: WidgetToggle,
				Label: "Fullscreen", Default: &Value{V: configfile.Bool(false)}},
		}}},
	}
}

func docs(t *testing.T) (configfile.Doc, configfile.Doc) {
	t.Helper()
	d, err := configfile.Parse([]byte(partial), configfile.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := configfile.Parse([]byte(reference), configfile.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	return d, ref
}

// The whole point: a setting whose section the program has not written becomes
// present, because the reference file attests that the section is real.
func TestCompleteAddsMissingSectionsAndSettings(t *testing.T) {
	d, ref := docs(t)
	n, err := Complete(d, ref, completeFile(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("added %d, want 3 (VsyncEnabled was already there)", n)
	}
	for path, want := range map[string]string{
		"CVars.gSettings.Volume.Master":    "40",
		"CVars.gSettings.Volume.MainMusic": "100",
		"Window.Fullscreen.Enabled":        "false",
	} {
		v, ok := d.Get(configfile.ParsePath(path))
		if !ok {
			t.Errorf("%s: still absent", path)
			continue
		}
		if v.Display() != want {
			t.Errorf("%s = %q, want %q", path, v.Display(), want)
		}
	}
	// The value already in the file is the program's, not the reference's.
	if v, _ := d.Get(configfile.ParsePath("CVars.gSettings.VsyncEnabled")); v.Int != 0 {
		t.Errorf("VsyncEnabled = %v, want the file's own 0 rather than the reference's 1", v.Int)
	}
	// So is every other value the file already held.
	for path, want := range map[string]string{"Window.Width": "1280", "Window.Height": "720"} {
		if v, _ := d.Get(configfile.ParsePath(path)); v.Display() != want {
			t.Errorf("%s = %q, want the file's own %q", path, v.Display(), want)
		}
	}
	// And the result is still the grammar it claims to be.
	back, err := configfile.Parse(d.Bytes(), configfile.FormatJSON)
	if err != nil {
		t.Fatalf("the completed file no longer parses: %v\n%s", err, d.Bytes())
	}
	if v, _ := back.Get(configfile.ParsePath("CVars.gSettings.Volume.Master")); v.Int != 40 {
		t.Errorf("after re-reading: Master = %+v", v)
	}
}

// Running it twice adds nothing the second time, which is what makes it safe to
// call on every save.
func TestCompleteIsIdempotent(t *testing.T) {
	d, ref := docs(t)
	f := completeFile()
	if _, err := Complete(d, ref, f, "", nil); err != nil {
		t.Fatal(err)
	}
	once := string(d.Bytes())
	n, err := Complete(d, ref, f, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("added %d on the second pass, want 0", n)
	}
	if got := string(d.Bytes()); got != once {
		t.Errorf("the second pass changed the file:\n%s", got)
	}
}

// What gets written is bounded by the schema and its version range, not by the
// reference file. A reference taken from a newer build must not drag that build's
// settings into an older one.
func TestCompleteHonoursVersionBoundsAndTheSchema(t *testing.T) {
	d, err := configfile.Parse([]byte("{\n    \"a\": 1\n}\n"), configfile.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := configfile.Parse([]byte("{\n    \"a\": 1,\n    \"b\": 2,\n    \"c\": 3\n}\n"), configfile.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	f := File{
		Title: "S", Path: "x.json", Format: configfile.FormatJSON,
		Sections: []Section{{Title: "S", Fields: []Field{
			{Pointer: "b", Kind: KindInt, Widget: WidgetNumber, Label: "B",
				Default: &Value{V: configfile.Int(2)}, SinceVersion: "2.0"},
		}}},
	}
	// b belongs to 2.0 and later; c is in the reference but in no field.
	n, err := Complete(d, ref, f, "1.0", []string{"1.0", "2.0"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("added %d on 1.0, want 0", n)
	}
	if got := string(d.Bytes()); got != "{\n    \"a\": 1\n}\n" {
		t.Errorf("the file changed:\n%s", got)
	}
	if _, err := Complete(d, ref, f, "2.0", []string{"1.0", "2.0"}); err != nil {
		t.Fatal(err)
	}
	if v, ok := d.Get(configfile.ParsePath("b")); !ok || v.Int != 2 {
		t.Errorf("b on 2.0 = %+v", v)
	}
	if _, ok := d.Get(configfile.ParsePath("c")); ok {
		t.Error("c is in the reference but no field names it, so it must not be written")
	}
}

// A reference that disagrees with the schema about a setting's kind means one of
// the two is wrong. Writing either would be writing a guess, so neither is written.
func TestCompleteSkipsWhereTheReferenceDisagrees(t *testing.T) {
	d, _ := docs(t)
	ref, err := configfile.Parse([]byte(strings.Replace(reference,
		`"Master": 40`, `"Master": "loud"`, 1)), configfile.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Complete(d, ref, completeFile(), "", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Get(configfile.ParsePath("CVars.gSettings.Volume.Master")); ok {
		t.Error("the reference holds a string where the schema says int; nothing should be written")
	}
	// Its sibling, which they agree on, is still added.
	if _, ok := d.Get(configfile.ParsePath("CVars.gSettings.Volume.MainMusic")); !ok {
		t.Error("MainMusic should still have been added")
	}
}

// A section the reference does not describe either is not invented. This is the
// line the whole feature rests on: structure is copied, never guessed.
func TestCompleteWillNotInventASectionTheReferenceLacks(t *testing.T) {
	d, err := configfile.Parse([]byte("{\n    \"a\": 1\n}\n"), configfile.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := configfile.Parse([]byte("{\n    \"a\": 1\n}\n"), configfile.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	f := File{
		Title: "S", Path: "x.json", Format: configfile.FormatJSON,
		Sections: []Section{{Title: "S", Fields: []Field{
			{Pointer: "graphics.msaa", Kind: KindInt, Widget: WidgetNumber, Label: "MSAA",
				Default: &Value{V: configfile.Int(1)}},
		}}},
	}
	n, err := Complete(d, ref, f, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("added %d, want 0", n)
	}
	if got := string(d.Bytes()); got != "{\n    \"a\": 1\n}\n" {
		t.Errorf("a section nobody attested was invented:\n%s", got)
	}
}

// The same thing in a grammar whose container is a section header rather than a
// nested object, since that is the other shape this has to work in.
func TestCompleteAddsAGodotSection(t *testing.T) {
	d, err := configfile.Parse([]byte("[video]\nmode=0\n"), configfile.FormatGodot)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := configfile.Parse([]byte("[video]\nmode=0\n\n[editor]\nautosave=5\n"), configfile.FormatGodot)
	if err != nil {
		t.Fatal(err)
	}
	f := File{
		Title: "S", Path: "x.cfg", Format: configfile.FormatGodot,
		Sections: []Section{{Title: "Editor", Fields: []Field{
			{Pointer: "editor.autosave", Kind: KindInt, Widget: WidgetNumber, Label: "Autosave",
				Default: &Value{V: configfile.Int(5)}},
		}}},
	}
	if _, err := Complete(d, ref, f, "", nil); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != "[video]\nmode=0\n\n[editor]\nautosave=5\n" {
		t.Errorf("got %q", got)
	}
	if _, err := configfile.Parse(d.Bytes(), configfile.FormatGodot); err != nil {
		t.Errorf("the result no longer parses: %v", err)
	}
}

// Completing a nested grammar where the container *is* a value, from a reference
// that has it as an empty one. The path has to survive the round trip through a
// container that was created rather than parsed.
func TestCompleteIntoAContainerItJustCreated(t *testing.T) {
	d, err := configfile.Parse([]byte("return {\n  volume = 7,\n}\n"), configfile.FormatLuaTable)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := configfile.Parse([]byte("return {\n  volume = 7,\n  touchControls = {\n    enabled = true,\n  },\n}\n"), configfile.FormatLuaTable)
	if err != nil {
		t.Fatal(err)
	}
	f := File{
		Title: "S", Path: "x.lua", Format: configfile.FormatLuaTable,
		Sections: []Section{{Title: "S", Fields: []Field{
			{Pointer: "touchControls.enabled", Kind: KindBool, Widget: WidgetToggle,
				Label: "Touch controls", Default: &Value{V: configfile.Bool(true)}},
		}}},
	}
	if _, err := Complete(d, ref, f, "", nil); err != nil {
		t.Fatal(err)
	}
	back, err := configfile.Parse(d.Bytes(), configfile.FormatLuaTable)
	if err != nil {
		t.Fatalf("the completed file no longer parses: %v\n%s", err, d.Bytes())
	}
	if v, ok := back.Get(configfile.ParsePath("touchControls.enabled")); !ok || !v.Bool {
		t.Errorf("touchControls.enabled = %+v\n%s", v, d.Bytes())
	}
	if v, _ := back.Get(configfile.ParsePath("volume")); v.Int != 7 {
		t.Errorf("volume = %+v", v)
	}
}

// Two settings inside the same missing container: the container is added once, not
// once per setting.
func TestCompleteAddsAContainerOnlyOnce(t *testing.T) {
	d, ref := docs(t)
	if _, err := Complete(d, ref, completeFile(), "", nil); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(d.Bytes()), `"Volume"`); n != 1 {
		t.Errorf("Volume appears %d times, want 1:\n%s", n, d.Bytes())
	}
}

// A select has to recognise the option the file is already set to. It did not, for a
// while: a value parsed out of a file carries its literal in Raw and one declared in
// a schema does not, so comparing the structs made them different values.
func TestFieldRulesIgnoreHowAValueWasSpelled(t *testing.T) {
	d, err := configfile.Parse([]byte("[video]\nmode=2\nvsync=1\ncampaign=\"SMB1\"\n"), configfile.FormatGodot)
	if err != nil {
		t.Fatal(err)
	}
	two := configfile.Int(2)
	fromFile, _ := d.Get(configfile.ParsePath("video.mode"))
	if fromFile == two {
		t.Fatal("this test is pointless if the two are already identical structs")
	}
	if !fromFile.Equal(two) {
		t.Error("2 read from a file is the same value as 2 declared in a schema")
	}

	f := File{
		Title: "S", Path: "x.cfg", Format: configfile.FormatGodot,
		Sections: []Section{{Title: "Video", Fields: []Field{
			{Pointer: "video.mode", Kind: KindInt, Widget: WidgetSelect, Label: "Mode",
				Options: []Option{
					{Label: "A", Value: Value{V: configfile.Int(0)}},
					{Label: "C", Value: Value{V: configfile.Int(2)}},
				}},
			{Pointer: "video.vsync", Kind: KindInt, Widget: WidgetToggle, Label: "V-sync",
				On: &Value{V: configfile.Int(1)}, Off: &Value{V: configfile.Int(0)}},
			{Pointer: "video.campaign", Kind: KindString, Widget: WidgetSelect, Label: "Campaign",
				Options: []Option{
					{Label: "1", Value: Value{V: configfile.String("SMB1")}},
					{Label: "LL", Value: Value{V: configfile.String("SMBLL")}},
				}},
		}}},
	}
	// Writing back the value the file already holds has to be allowed, which is the
	// same check Complete and the page both make.
	for _, sec := range Resolve(d, f, "", nil) {
		for _, st := range sec.Fields {
			if !st.Editable {
				t.Errorf("%s: %s: %s", st.Pointer, st.Reason, st.Problem)
				continue
			}
			if err := WriteField(d, st.Field, st.Value); err != nil {
				t.Errorf("%s: writing back its own value: %v", st.Pointer, err)
			}
		}
	}
	if got := string(d.Bytes()); got != "[video]\nmode=2\nvsync=1\ncampaign=\"SMB1\"\n" {
		t.Errorf("the file changed: %q", got)
	}
}
