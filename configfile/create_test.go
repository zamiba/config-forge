package configfile

import (
	"errors"
	"strings"
	"testing"
)

// A key is added inside a section that already exists, beside that section's own
// keys rather than at the end of the file.
func TestGodotCreateAddsAKeyToItsSection(t *testing.T) {
	d := doc(t, smbr)
	if !d.CanCreate(ParsePath("video.hdr")) {
		t.Fatal("video exists, so a key can be added to it")
	}
	if err := d.Create(ParsePath("video.hdr"), Bool(true)); err != nil {
		t.Fatal(err)
	}
	out := string(d.Bytes())
	if !strings.Contains(out, "window_size=[1024, 960]\nhdr=true\n\n[audio]") {
		t.Errorf("the key did not land at the end of [video]:\n%s", out)
	}
	if v, ok := d.Get(ParsePath("video.hdr")); !ok || v.Kind != KindBool || !v.Bool {
		t.Errorf("created value reads back as %+v", v)
	}
	// Everything else is where it was.
	for path, want := range map[string]string{
		"video.mode": "0", "audio.master": "10", "game.campaign": "SMB1",
		"visuals.parallax_style": "2", "controller.deadzone": "0.5",
	} {
		if v, _ := d.Get(ParsePath(path)); v.Display() != want {
			t.Errorf("%s = %q, want %q", path, v.Display(), want)
		}
	}
	// And the new key can then be edited like any other.
	if err := d.Set(ParsePath("video.hdr"), Bool(false)); err != nil {
		t.Fatal(err)
	}
	if v, _ := d.Get(ParsePath("video.hdr")); v.Bool {
		t.Error("the created key should now be false")
	}
}

// A section the file lacks is structure, and adding structure can break the
// program's own loader, so it is refused.
func TestGodotCreateRefusesANewSection(t *testing.T) {
	d := doc(t, smbr)
	before := string(d.Bytes())
	if d.CanCreate(ParsePath("network.port")) {
		t.Error("CanCreate should be false for a section the file lacks")
	}
	if err := d.Create(ParsePath("network.port"), Int(1)); !errors.Is(err, ErrNoContainer) {
		t.Errorf("err = %v, want ErrNoContainer", err)
	}
	if err := d.Create(ParsePath("video.mode"), Int(1)); !errors.Is(err, ErrAlreadyPresent) {
		t.Errorf("err = %v, want ErrAlreadyPresent", err)
	}
	if got := string(d.Bytes()); got != before {
		t.Error("a refused Create modified the file")
	}
}

// A section with a header but no keys yet still takes one.
func TestGodotCreateIntoAnEmptySection(t *testing.T) {
	d := doc(t, "[video]\nmode=1\n\n[audio]\n")
	if err := d.Create(ParsePath("audio.master"), Int(8)); err != nil {
		t.Fatal(err)
	}
	if got, want := string(d.Bytes()), "[video]\nmode=1\n\n[audio]\nmaster=8\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLuaCreateAddsAKeyToItsTable(t *testing.T) {
	d := lua(t, gen1)
	if !d.CanCreate(ParsePath("newSetting")) {
		t.Fatal("the root table exists")
	}
	if err := d.Create(ParsePath("newSetting"), Int(4)); err != nil {
		t.Fatal(err)
	}
	out := string(d.Bytes())
	// Added as a line above the closing brace, in the writer's own layout.
	if !strings.Contains(out, "  newSetting = 4,\n}\n") {
		t.Errorf("layout:\n%s", out)
	}
	if v, ok := d.Get(ParsePath("newSetting")); !ok || v.Int != 4 {
		t.Errorf("reads back as %+v", v)
	}
	// A nested table takes one at its own indentation.
	if err := d.Create(Path{"touchControls", "haptics"}, String("light")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(d.Bytes()), "    haptics = \"light\",\n  },") {
		t.Errorf("nested layout:\n%s", d.Bytes())
	}
	if v, _ := d.Get(Path{"touchControls", "enabled"}); !v.Bool {
		t.Error("the sibling inside that table was disturbed")
	}
	if v, _ := d.Get(ParsePath("musicVol")); v.Int != 7 {
		t.Error("an unrelated value was disturbed")
	}
}

func TestLuaCreateRefusesAMissingOrNonTableParent(t *testing.T) {
	d := lua(t, gen1)
	before := string(d.Bytes())
	for name, p := range map[string]Path{
		"missing parent":     {"nothingHere", "key"},
		"parent is a bool":   {"animations", "key"},
		"parent is a string": {"battleBg", "key"},
	} {
		if d.CanCreate(p) {
			t.Errorf("%s: CanCreate should be false", name)
		}
		if err := d.Create(p, Int(1)); !errors.Is(err, ErrNoContainer) {
			t.Errorf("%s: err = %v, want ErrNoContainer", name, err)
		}
	}
	if err := d.Create(ParsePath("musicVol"), Int(1)); !errors.Is(err, ErrAlreadyPresent) {
		t.Errorf("err = %v, want ErrAlreadyPresent", err)
	}
	if got := string(d.Bytes()); got != before {
		t.Error("a refused Create modified the file")
	}
}

// An inline table is added to inline, rather than being reformatted.
func TestLuaCreateIntoAnInlineTable(t *testing.T) {
	d := lua(t, "return {\n  empty = {},\n  filled = { a = 1 },\n}\n")
	if err := d.Create(Path{"empty", "x"}, Int(1)); err != nil {
		t.Fatal(err)
	}
	if err := d.Create(Path{"filled", "b"}, Int(2)); err != nil {
		t.Fatal(err)
	}
	const want = "return {\n  empty = { x = 1 },\n  filled = { a = 1, b = 2 },\n}\n"
	if got := string(d.Bytes()); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if v, _ := d.Get(Path{"filled", "a"}); v.Int != 1 {
		t.Error("the existing entry was disturbed")
	}
}

// A key that is not a bare identifier is bracketed and quoted, as the program's
// own writer does.
func TestLuaCreateQuotesAKeyThatNeedsIt(t *testing.T) {
	d := lua(t, "return {\n  mods = {},\n}\n")
	if err := d.Create(Path{"mods", "some.mod"}, Bool(true)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(d.Bytes()), `["some.mod"] = true`) {
		t.Errorf("got %s", d.Bytes())
	}
	if v, ok := d.Get(Path{"mods", "some.mod"}); !ok || !v.Bool {
		t.Errorf("reads back as %+v", v)
	}
}

func TestEmptyDocCreatesNothing(t *testing.T) {
	d := Empty()
	if d.CanCreate(ParsePath("a.b")) {
		t.Error("nothing can be added to a file that is not there")
	}
	if err := d.Create(ParsePath("a.b"), Int(1)); !errors.Is(err, ErrNoContainer) {
		t.Errorf("err = %v, want ErrNoContainer", err)
	}
}

// Create after Set: the root table's span has to have moved with the edits, or the
// new key is measured from a stale closing brace and lands outside the table.
func TestLuaCreateAfterEditsLandsInsideTheTable(t *testing.T) {
	d := lua(t, gen1)
	// A shortening edit and a lengthening one, so the net delta is not zero.
	if err := d.Set(ParsePath("battleStyle"), String("set")); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(ParsePath("animations"), Bool(false)); err != nil {
		t.Fatal(err)
	}
	if err := d.Create(ParsePath("sfxVol"), Int(7)); err != nil {
		t.Fatal(err)
	}
	out := string(d.Bytes())
	if !strings.Contains(out, "  sfxVol = 7,\n}") {
		t.Errorf("the key did not land inside the table:\n%s", out)
	}
	if strings.Contains(out, "}, sfxVol") || strings.Contains(out, "}\nsfxVol") {
		t.Errorf("the key landed outside the table:\n%s", out)
	}
	// Re-parsing it proves the result is still the grammar it claims to be.
	if _, err := Parse([]byte(out), FormatLuaTable); err != nil {
		t.Errorf("the result no longer parses: %v", err)
	}
	if v, ok := d.Get(ParsePath("sfxVol")); !ok || v.Int != 7 {
		t.Errorf("sfxVol = %+v", v)
	}

	// The same for a nested table, whose span Set also has to move.
	if err := d.Set(Path{"touchControls", "enabled"}, Bool(false)); err != nil {
		t.Fatal(err)
	}
	if err := d.Create(Path{"touchControls", "haptics"}, String("light")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(d.Bytes()), "haptics = \"light\"") {
		t.Errorf("nested create after edit:\n%s", d.Bytes())
	}
	if _, err := Parse(d.Bytes(), FormatLuaTable); err != nil {
		t.Errorf("nested result no longer parses: %v", err)
	}
}

func TestJSONCreateAddsAMemberToItsObject(t *testing.T) {
	d := js(t, shipCfg)
	if !d.CanCreate(ParsePath("Window.Vsync")) {
		t.Fatal("Window exists, so a member can be added to it")
	}
	if err := d.Create(ParsePath("Window.Vsync"), Bool(true)); err != nil {
		t.Fatal(err)
	}
	// The comma goes on the member that was last, because JSON forbids a trailing
	// one, and the new line takes its siblings' indentation.
	if !strings.Contains(string(d.Bytes()), "        \"Width\": 1280,\n        \"Vsync\": true\n    }") {
		t.Errorf("layout:\n%s", d.Bytes())
	}
	// Deeper in, where the indentation is different again.
	if err := d.Create(ParsePath("CVars.gEnhancements.Mods.Portable"), Int(1)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(d.Bytes()), "                \"AlternateAssets\": 0,\n                \"Portable\": 1\n            }") {
		t.Errorf("nested layout:\n%s", d.Bytes())
	}
	for path, want := range map[string]string{
		"Window.Vsync": "true", "CVars.gEnhancements.Mods.Portable": "1",
		"Window.Width": "1280", "Window.Backend.Name": "OpenGL",
		"CVars.gEnhancements.TimeSavers.SkipIntro": "1",
	} {
		if v, ok := d.Get(ParsePath(path)); !ok || v.Display() != want {
			t.Errorf("%s = %q, want %q", path, v.Display(), want)
		}
	}
	out := d.Bytes()
	if _, err := Parse(out, FormatJSON); err != nil {
		t.Fatalf("the result no longer parses: %v", err)
	}
	// And a created member is editable like any other.
	if err := d.Set(ParsePath("Window.Vsync"), Bool(false)); err != nil {
		t.Fatal(err)
	}
	if v, _ := d.Get(ParsePath("Window.Vsync")); v.Bool {
		t.Error("the created member should now be false")
	}
}

func TestJSONCreateRefusesAMissingOrNonObjectParent(t *testing.T) {
	d := js(t, shipCfg)
	before := string(d.Bytes())
	for name, p := range map[string]Path{
		"missing parent":     {"Audio", "Master"},
		"parent is an int":   {"Window", "Width", "x"},
		"parent is a bool":   {"Window", "Fullscreen", "Enabled", "x"},
		"parent is an array": {"CVars", "gWindowed", "Position", "x"},
	} {
		if d.CanCreate(p) {
			t.Errorf("%s: CanCreate should be false", name)
		}
		if err := d.Create(p, Int(1)); !errors.Is(err, ErrNoContainer) {
			t.Errorf("%s: err = %v, want ErrNoContainer", name, err)
		}
	}
	if err := d.Create(ParsePath("Window.Width"), Int(1)); !errors.Is(err, ErrAlreadyPresent) {
		t.Errorf("err = %v, want ErrAlreadyPresent", err)
	}
	if got := string(d.Bytes()); got != before {
		t.Error("a refused Create modified the file")
	}
}

// An object written on one line is added to on that line, rather than being
// reformatted into the layout this package would have chosen.
func TestJSONCreateIntoAnInlineObject(t *testing.T) {
	d := js(t, `{"empty": {}, "spaced": { }, "filled": {"a": 1}}`)
	if err := d.Create(Path{"empty", "x"}, Int(1)); err != nil {
		t.Fatal(err)
	}
	if err := d.Create(Path{"spaced", "y"}, Int(2)); err != nil {
		t.Fatal(err)
	}
	if err := d.Create(Path{"filled", "b"}, Int(3)); err != nil {
		t.Fatal(err)
	}
	const want = `{"empty": { "x": 1 }, "spaced": { "y": 2 }, "filled": {"a": 1, "b": 3}}`
	if got := string(d.Bytes()); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if v, _ := d.Get(Path{"filled", "a"}); v.Int != 1 {
		t.Error("the existing member was disturbed")
	}
	if _, err := Parse(d.Bytes(), FormatJSON); err != nil {
		t.Errorf("the result no longer parses: %v", err)
	}
}

// An object with nothing in it yet has no sibling to line up with, so the
// indentation comes from the file's own.
func TestJSONCreateIntoAnEmptyMultilineObject(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"four spaces": {
			"{\n    \"Mods\": {\n    }\n}\n",
			"{\n    \"Mods\": {\n        \"a\": 1\n    }\n}\n",
		},
		"tabs": {
			"{\n\t\"Mods\": {\n\t}\n}\n",
			"{\n\t\"Mods\": {\n\t\t\"a\": 1\n\t}\n}\n",
		},
	} {
		d := js(t, tc.in)
		if err := d.Create(Path{"Mods", "a"}, Int(1)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := string(d.Bytes()); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", name, got, tc.want)
		}
	}
}

// Create after Set: the enclosing spans have to have moved with the edits, or the
// new member is measured from a stale closing brace and lands outside the object.
func TestJSONCreateAfterEditsLandsInsideTheObject(t *testing.T) {
	d := js(t, shipCfg)
	// A shortening edit and a lengthening one, so the net delta is not zero.
	if err := d.Set(ParsePath("Window.Backend.Name"), String("DirectX 11")); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(ParsePath("Window.PositionX"), Int(0)); err != nil {
		t.Fatal(err)
	}
	if err := d.Create(ParsePath("Window.Vsync"), Bool(true)); err != nil {
		t.Fatal(err)
	}
	out := string(d.Bytes())
	if !strings.Contains(out, "\"Vsync\": true\n    }\n}") {
		t.Errorf("the member did not land inside Window:\n%s", out)
	}
	if _, err := Parse([]byte(out), FormatJSON); err != nil {
		t.Fatalf("the result no longer parses: %v", err)
	}

	// The same one level deeper, after an edit inside that very object.
	if err := d.Set(ParsePath("CVars.gGraphics.MSAAValue"), Int(4)); err != nil {
		t.Fatal(err)
	}
	if err := d.Create(ParsePath("CVars.gGraphics.Vsync"), Int(1)); err != nil {
		t.Fatal(err)
	}
	back := js(t, string(d.Bytes()))
	for path, want := range map[string]string{
		"Window.Vsync": "true", "Window.Backend.Name": "DirectX 11",
		"Window.PositionX": "0", "CVars.gGraphics.MSAAValue": "4",
		"CVars.gGraphics.Vsync": "1", "CVars.gGraphics.InternalResolution": "1",
	} {
		if v, ok := back.Get(ParsePath(path)); !ok || v.Display() != want {
			t.Errorf("after re-reading: %s = %q, want %q", path, v.Display(), want)
		}
	}
}

// CreateContainer is the one thing here that adds structure. Each grammar renders
// its own kind of empty container, and a grammar with no containers says so.
func TestCreateContainerPerGrammar(t *testing.T) {
	for name, tc := range map[string]struct {
		format   Format
		in, want string
		at       Path
		leaf     Path
	}{
		"godot section": {
			FormatGodot,
			"[video]\nmode=0\n",
			"[video]\nmode=0\n\n[editor]\nautosave=5\n",
			Path{"editor"}, Path{"editor", "autosave"},
		},
		"ini section": {
			FormatINI,
			"[RecompOne]\nVSync=True\n",
			"[RecompOne]\nVSync=True\n\n[Audio]\nautosave=5\n",
			Path{"Audio"}, Path{"Audio", "autosave"},
		},
		"toml table": {
			FormatTOML,
			"a = 1\n",
			"a = 1\n\n[server]\nautosave = 5\n",
			Path{"server"}, Path{"server", "autosave"},
		},
		"json object": {
			FormatJSON,
			"{\n    \"a\": 1\n}\n",
			"{\n    \"a\": 1,\n    \"server\": { \"autosave\": 5 }\n}\n",
			Path{"server"}, Path{"server", "autosave"},
		},
		"lua table": {
			FormatLuaTable,
			"return {\n  a = 1,\n}\n",
			"return {\n  a = 1,\n  server = { autosave = 5 },\n}\n",
			Path{"server"}, Path{"server", "autosave"},
		},
		"no final newline": {
			FormatGodot,
			"[video]\nmode=0",
			"[video]\nmode=0\n\n[editor]\nautosave=5\n",
			Path{"editor"}, Path{"editor", "autosave"},
		},
		"empty file": {
			FormatGodot, "", "[editor]\nautosave=5\n",
			Path{"editor"}, Path{"editor", "autosave"},
		},
	} {
		d, err := Parse([]byte(tc.in), tc.format)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if err := d.CreateContainer(tc.at); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if err := d.Create(tc.leaf, Int(5)); err != nil {
			t.Errorf("%s: creating the leaf: %v\n%s", name, err, d.Bytes())
			continue
		}
		if got := string(d.Bytes()); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", name, got, tc.want)
		}
		// The result is still the grammar it claims to be, and the leaf reads back.
		back, err := Parse(d.Bytes(), tc.format)
		if err != nil {
			t.Errorf("%s: no longer parses: %v", name, err)
			continue
		}
		if v, ok := back.Get(tc.leaf); !ok || v.Int != 5 {
			t.Errorf("%s: leaf reads back as %+v", name, v)
		}
	}
}

func TestCreateContainerRefusesWhatItShould(t *testing.T) {
	d := doc(t, smbr)
	// A section that is already there.
	if err := d.CreateContainer(Path{"video"}); !errors.Is(err, ErrAlreadyPresent) {
		t.Errorf("err = %v, want ErrAlreadyPresent", err)
	}
	// A ConfigFile nests one level, so a deeper container is not a thing it has.
	if err := d.CreateContainer(Path{"a", "b"}); !errors.Is(err, ErrNoContainer) {
		t.Errorf("err = %v, want ErrNoContainer", err)
	}
	if err := d.CreateContainer(nil); !errors.Is(err, ErrNoContainer) {
		t.Errorf("err = %v, want ErrNoContainer", err)
	}

	// A grammar with no containers at all.
	ss, err := Parse([]byte("a 1\n"), FormatSpaceSeparated)
	if err != nil {
		t.Fatal(err)
	}
	if err := ss.CreateContainer(Path{"x"}); !errors.Is(err, ErrNoContainer) {
		t.Errorf("err = %v, want ErrNoContainer", err)
	}

	// And a file that is not there: adding a container would be creating the file.
	if err := Empty().CreateContainer(Path{"x"}); !errors.Is(err, ErrNoContainer) {
		t.Errorf("err = %v, want ErrNoContainer", err)
	}
}
