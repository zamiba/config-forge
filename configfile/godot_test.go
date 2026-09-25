package configfile

import (
	"errors"
	"strings"
	"testing"
)

// A settings.cfg as Godot writes one, with the real sections, keys and value
// kinds Super Mario Bros. Remastered declares in SettingsManager.gd — including
// the ones this package will not rewrite.
const smbr = `[video]
mode=0
size=0
multiplier=3
vsync=1
window_size=[1024, 960]

[audio]
master=10
music=10

[game]
campaign="SMB1"
lang="en"
seen_disclaimer=true

[editor]
seen_guide=false
autosave_min_timer=5

[controller]
deadzone=0.5
jump=[0, 1]
move_left="0,-1"

[visuals]
resource_packs=["ROM Pack"]
parallax_style=2
`

func doc(t *testing.T, s string) Doc {
	t.Helper()
	d, err := Parse([]byte(s), FormatGodot)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestGodotReadsEveryKind(t *testing.T) {
	d := doc(t, smbr)
	for _, tc := range []struct {
		path string
		kind Kind
		want string
	}{
		{"video.mode", KindInt, "0"},
		{"video.multiplier", KindInt, "3"},
		{"video.window_size", KindOpaque, "[1024, 960]"},
		{"audio.master", KindInt, "10"},
		{"game.campaign", KindString, "SMB1"},
		{"game.seen_disclaimer", KindBool, "true"},
		{"editor.seen_guide", KindBool, "false"},
		{"controller.deadzone", KindFloat, "0.5"},
		{"controller.jump", KindOpaque, "[0, 1]"},
		{"controller.move_left", KindString, "0,-1"},
		{"visuals.resource_packs", KindOpaque, `["ROM Pack"]`},
	} {
		v, ok := d.Get(ParsePath(tc.path))
		if !ok {
			t.Errorf("%s: not found", tc.path)
			continue
		}
		if v.Kind != tc.kind {
			t.Errorf("%s: kind = %v, want %v", tc.path, v.Kind, tc.kind)
		}
		if got := v.Display(); got != tc.want {
			t.Errorf("%s: display = %q, want %q", tc.path, got, tc.want)
		}
	}
	if _, ok := d.Get(ParsePath("video.nonexistent")); ok {
		t.Error("a path the file does not contain was reported as present")
	}
}

// The guarantee the package exists for: an edit moves the bytes of that one
// value and nothing else in the file.
func TestGodotSetTouchesOnlyItsOwnValue(t *testing.T) {
	d := doc(t, smbr)
	if err := d.Set(ParsePath("video.mode"), Int(3)); err != nil {
		t.Fatal(err)
	}
	got := string(d.Bytes())
	want := strings.Replace(smbr, "mode=0\n", "mode=3\n", 1)
	if got != want {
		t.Errorf("one int edit changed more than its value:\n--- got\n%s\n--- want\n%s", got, want)
	}
}

// Several edits in sequence, including ones that change the value's length, so
// the spans after each edit have to move with it.
func TestGodotSeveralEditsKeepLaterSpansCorrect(t *testing.T) {
	d := doc(t, smbr)
	for _, e := range []struct {
		path string
		val  Value
	}{
		{"video.multiplier", Int(100)},       // longer
		{"game.campaign", String("SMB2")},    // same length
		{"editor.seen_guide", Bool(true)},    // longer
		{"controller.deadzone", Float(0.25)}, // longer
		{"audio.master", Int(7)},             // shorter
		{"visuals.parallax_style", Int(0)},   // last entry in the file
	} {
		if err := d.Set(ParsePath(e.path), e.val); err != nil {
			t.Fatalf("%s: %v", e.path, err)
		}
	}

	// Every edit landed, read back from the mutated buffer.
	for path, want := range map[string]string{
		"video.multiplier":       "100",
		"game.campaign":          "SMB2",
		"editor.seen_guide":      "true",
		"controller.deadzone":    "0.25",
		"audio.master":           "7",
		"visuals.parallax_style": "0",
	} {
		v, ok := d.Get(ParsePath(path))
		if !ok {
			t.Fatalf("%s vanished after editing", path)
		}
		if v.Display() != want {
			t.Errorf("%s = %q, want %q", path, v.Display(), want)
		}
	}
	// And the values that were never written to are still exactly as they were.
	for path, want := range map[string]string{
		"video.mode":             "0",
		"video.window_size":      "[1024, 960]",
		"audio.music":            "10",
		"game.lang":              "en",
		"controller.jump":        "[0, 1]",
		"controller.move_left":   "0,-1",
		"visuals.resource_packs": `["ROM Pack"]`,
	} {
		v, _ := d.Get(ParsePath(path))
		if v.Display() != want {
			t.Errorf("untouched %s = %q, want %q", path, v.Display(), want)
		}
	}
	out := string(d.Bytes())
	if !strings.HasSuffix(out, "parallax_style=0\n") {
		t.Errorf("the file's ending was disturbed: %q", out[len(out)-40:])
	}
	if strings.Count(out, "[video]") != 1 || strings.Count(out, "\n\n") != strings.Count(smbr, "\n\n") {
		t.Error("section headers or blank lines were disturbed")
	}
}

// Writing nothing must return the file unchanged, byte for byte. This is what
// lets a host open a config, show it, and close it without risk.
func TestGodotBytesWithoutEditsIsIdentical(t *testing.T) {
	for name, in := range map[string]string{
		"as godot writes it":  smbr,
		"hand edited":         "; my notes\n[video]\n\n# spaced out\nmode = 2   ; trailing note\n\n[audio]\nmaster=10",
		"no trailing newline": "[video]\nmode=1",
		"crlf":                "[video]\r\nmode=1\r\nsize=2\r\n",
		"global keys first":   "version=3\n\n[video]\nmode=1\n",
		"empty":               "",
	} {
		d := doc(t, in)
		if got := string(d.Bytes()); got != in {
			t.Errorf("%s: reading changed the file:\n got %q\nwant %q", name, got, in)
		}
	}
}

// Odd spacing and trailing comments must survive an edit, since the parser is
// the only thing standing between a hand-edited file and its owner.
func TestGodotEditPreservesSpacingAndComments(t *testing.T) {
	const in = "; keep me\n[video]\nmode = 2   ; and me\nsize=1\n"
	d := doc(t, in)
	if v, _ := d.Get(ParsePath("video.mode")); v.Kind != KindInt || v.Int != 2 {
		t.Fatalf("mode read as %+v", v)
	}
	if err := d.Set(ParsePath("video.mode"), Int(7)); err != nil {
		t.Fatal(err)
	}
	const want = "; keep me\n[video]\nmode = 7   ; and me\nsize=1\n"
	if got := string(d.Bytes()); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// Fail closed: a value this package will not rewrite is refused, and refusing
// must leave the file untouched.
func TestGodotRefusesWhatItCannotRewrite(t *testing.T) {
	d := doc(t, smbr)
	before := string(d.Bytes())

	// Turning an array into a string is the corruption this refusal exists for.
	if err := d.Set(ParsePath("video.window_size"), String("[800, 600]")); !errors.Is(err, ErrKindMismatch) {
		t.Errorf("string over an array: err = %v, want ErrKindMismatch", err)
	}
	// So is writing true where the program stores 1.
	if err := d.Set(ParsePath("video.vsync"), Bool(true)); !errors.Is(err, ErrKindMismatch) {
		t.Errorf("bool over an int: err = %v, want ErrKindMismatch", err)
	}
	// An int used as a boolean is edited by writing the int, which is the
	// schema's job to declare.
	if err := d.Set(ParsePath("video.vsync"), Int(0)); err != nil {
		t.Errorf("int over an int should be fine: %v", err)
	}
	d = doc(t, smbr)
	if err := d.Set(ParsePath("video.window_size"), Value{Kind: KindOpaque, Raw: "[800, 600]"}); err == nil {
		t.Error("writing an opaque value should be refused")
	}
	if err := d.Set(ParsePath("video.missing"), Int(1)); err == nil {
		t.Error("setting a path the file lacks should be refused")
	}
	if err := d.Set(ParsePath("nosuchsection.key"), Int(1)); err == nil {
		t.Error("setting a path in a section the file lacks should be refused")
	}
	if got := string(d.Bytes()); got != before {
		t.Error("a refused Set modified the file")
	}
}

// A float must stay a float in the file, or the program reads an int where it
// expected one and its own type handling changes underneath it.
func TestGodotFloatKeepsItsPoint(t *testing.T) {
	d := doc(t, smbr)
	if err := d.Set(ParsePath("controller.deadzone"), Float(1)); err != nil {
		t.Fatal(err)
	}
	if v, _ := d.Get(ParsePath("controller.deadzone")); v.Kind != KindFloat || v.Raw != "1.0" {
		t.Errorf("deadzone written as %q (kind %v), want \"1.0\" as a float", v.Raw, v.Kind)
	}
}

func TestGodotStringsRoundTripTheirEscapes(t *testing.T) {
	d := doc(t, "[game]\nname=\"a \\\"quoted\\\" name\"\npath=\"C:\\\\Games\"\nuni=\"\\u00e9\"\n")
	if v, _ := d.Get(ParsePath("game.name")); v.Kind != KindString || v.Str != `a "quoted" name` {
		t.Errorf("escaped quotes read as %+v", v)
	}
	if v, _ := d.Get(ParsePath("game.path")); v.Str != `C:\Games` {
		t.Errorf("escaped backslash read as %q", v.Str)
	}
	// An escape this package would not reproduce byte for byte is readable but
	// not rewritable, rather than being silently re-encoded.
	v, _ := d.Get(ParsePath("game.uni"))
	if v.Kind != KindOpaque {
		t.Errorf(`\u escape should be opaque, got %v`, v.Kind)
	}
	if err := d.Set(ParsePath("game.name"), String(`say "hi"`)); err != nil {
		t.Fatal(err)
	}
	if v, _ := d.Get(ParsePath("game.name")); v.Str != `say "hi"` {
		t.Errorf("re-read as %q", v.Str)
	}
}

func TestGodotPathsAreInDeclarationOrder(t *testing.T) {
	d := doc(t, "version=1\n[video]\nmode=0\nsize=2\n[audio]\nmaster=10\n")
	var got []string
	for _, p := range d.Paths() {
		got = append(got, p.String())
	}
	want := "version video.mode video.size audio.master"
	if strings.Join(got, " ") != want {
		t.Errorf("Paths() = %v, want %s", got, want)
	}
}

func TestParseRejectsAnUnknownFormat(t *testing.T) {
	// A name no grammar will ever have, so adding a format does not send this test
	// looking for a new example.
	if _, err := Parse([]byte("x = 1"), Format("not-a-grammar")); err == nil {
		t.Error("an unimplemented format should be refused")
	}
}

// A bracketed literal that spans lines is measured whole, so an edit elsewhere
// cannot land inside it.
func TestGodotMultiLineLiteralIsOneOpaqueValue(t *testing.T) {
	const in = "[game]\nprofile={\n\"name\": \"sam\",\n\"slot\": 2\n}\nlang=\"en\"\n"
	d := doc(t, in)
	v, ok := d.Get(ParsePath("game.profile"))
	if !ok || v.Kind != KindOpaque {
		t.Fatalf("profile = %+v, ok = %v", v, ok)
	}
	if !strings.Contains(v.Raw, `"slot": 2`) {
		t.Errorf("the literal was cut short: %q", v.Raw)
	}
	if v, _ := d.Get(ParsePath("game.lang")); v.Str != "en" {
		t.Errorf("the key after a multi-line value was missed: %+v", v)
	}
	if err := d.Set(ParsePath("game.lang"), String("fr")); err != nil {
		t.Fatal(err)
	}
	if got, want := string(d.Bytes()), strings.Replace(in, `lang="en"`, `lang="fr"`, 1); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
