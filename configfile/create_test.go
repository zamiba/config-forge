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
