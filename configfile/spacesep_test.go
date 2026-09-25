package configfile

import (
	"errors"
	"strings"
	"testing"
)

// sm64config.txt as Render96's configfile_save writes one: every option on every
// save, a bool as true or false, a uint with %u, a float with %f — six decimal
// places — and a controller binding as three %04x tokens with a trailing space.
const sm64Config = `fullscreen false
window_x 65535
window_y 65535
window_w 640
window_h 480
vsync true
texture_filtering 1
master_volume 127
music_volume 127
sfx_volume 127
env_volume 127
key_a 0026 1000 1103 
key_b 0033 1002 1101 
stick_deadzone 16
rumble_strength 50
precache true
skip_intro false
`

// launcher.cfg as melee-pc's save_preferences writes one: std::quoted strings, a
// bool through operator<< as 0 or 1, a float through operator<< which drops the
// point on a whole number, and the install id in hex.
const meleeCfg = `net_name "PLAYER"
net_target ""
net_delay -1
net_port 0
disc "/home/sam/melee.ciso"
vsync 1
fullscreen 0
scale 1
render_scale 0
volume 1
msaa 1
anisotropy 16
widescreen 1
mute 0
fps 0
filter_mode 0
backend 0
check_updates 1
custom_textures 1
unlock_all 0
hud_mode 0
frozen_stadium 0
free_camera 0
ucf 0
music_volume 0.75
sfx_volume 1
install_id 1a2b3c4d5e6f7081
reverb 1
`

func ss(t *testing.T, s string) Doc {
	t.Helper()
	d, err := Parse([]byte(s), FormatSpaceSeparated)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestSpaceSepReadsBothWritersSpellings(t *testing.T) {
	d := ss(t, sm64Config)
	for _, tc := range []struct {
		path string
		kind Kind
		want string
	}{
		{"fullscreen", KindBool, "false"},
		{"vsync", KindBool, "true"},
		{"window_w", KindInt, "640"},
		{"texture_filtering", KindInt, "1"},
		{"master_volume", KindInt, "127"},
		{"precache", KindBool, "true"},
	} {
		v, ok := d.Get(ParsePath(tc.path))
		if !ok {
			t.Errorf("%s: not found", tc.path)
			continue
		}
		if v.Kind != tc.kind || v.Display() != tc.want {
			t.Errorf("%s = %q (%v), want %q (%v)", tc.path, v.Display(), v.Kind, tc.want, tc.kind)
		}
	}

	m := ss(t, meleeCfg)
	for _, tc := range []struct {
		path string
		kind Kind
		want string
	}{
		{"net_name", KindString, "PLAYER"},
		{"net_target", KindString, ""},
		{"disc", KindString, "/home/sam/melee.ciso"},
		{"net_delay", KindInt, "-1"},
		{"vsync", KindInt, "1"},
		{"music_volume", KindFloat, "0.75"},
		// operator<< dropped the point, so a float sits here as an int. This is
		// what kind "number" is for.
		{"sfx_volume", KindInt, "1"},
		{"scale", KindInt, "1"},
	} {
		v, ok := m.Get(ParsePath(tc.path))
		if !ok {
			t.Errorf("%s: not found", tc.path)
			continue
		}
		if v.Kind != tc.kind || v.Display() != tc.want {
			t.Errorf("%s = %q (%v), want %q (%v)", tc.path, v.Display(), v.Kind, tc.want, tc.kind)
		}
	}
	// Hex with no prefix is a word, not a number, and is never rewritten.
	if v, _ := m.Get(ParsePath("install_id")); v.Kind != KindOpaque {
		t.Errorf("install_id = %v, want opaque", v.Kind)
	}
}

// A binding is three tokens on one line. A parser that took only the first would
// report it as a number and let something overwrite the rest.
func TestSpaceSepMultipleTokensAreOpaque(t *testing.T) {
	d := ss(t, sm64Config)
	v, ok := d.Get(ParsePath("key_a"))
	if !ok {
		t.Fatal("key_a: not found")
	}
	if v.Kind != KindOpaque || v.Raw != "0026 1000 1103" {
		t.Errorf("key_a = %q (%v), want the whole binding, opaque", v.Raw, v.Kind)
	}
	if err := d.Set(ParsePath("key_a"), Int(38)); !errors.Is(err, ErrKindMismatch) {
		t.Errorf("Set on a binding = %v, want ErrKindMismatch", err)
	}
	// And the value after it still reads correctly, which is what proves the span
	// stopped where it should.
	if v, _ := d.Get(ParsePath("key_b")); v.Raw != "0033 1002 1101" {
		t.Errorf("key_b = %q", v.Raw)
	}
	if v, _ := d.Get(ParsePath("stick_deadzone")); v.Int != 16 {
		t.Errorf("stick_deadzone = %+v", v)
	}
}

func TestSpaceSepBytesWithoutEditsIsIdentical(t *testing.T) {
	for name, in := range map[string]string{
		"sm64config":          sm64Config,
		"launcher.cfg":        meleeCfg,
		"empty":               "",
		"comments and blanks": "# a note\n\na 1\n\n# another\n",
		"no trailing newline": "a 1\nb 2",
		"crlf":                "a 1\r\nb 2\r\n",
		"tabs":                "a\t1\nb\t\t2\n",
		"key with no value":   "a\nb 2\n",
	} {
		d := ss(t, in)
		if got := string(d.Bytes()); got != in {
			t.Errorf("%s: reading changed the file:\n got %q\nwant %q", name, got, in)
		}
	}
	// A line with a key and nothing after it is not an assignment.
	d := ss(t, "a\nb 2\n")
	if _, ok := d.Get(ParsePath("a")); ok {
		t.Error("a key with no value should not be an entry")
	}
	if v, _ := d.Get(ParsePath("b")); v.Int != 2 {
		t.Errorf("b = %+v", v)
	}
	// Tabs are separators like spaces.
	e := ss(t, "a\t1\n")
	if v, ok := e.Get(ParsePath("a")); !ok || v.Int != 1 {
		t.Errorf("a = %+v", v)
	}
}

func TestSpaceSepSetTouchesOnlyItsOwnValue(t *testing.T) {
	d := ss(t, sm64Config)
	for _, e := range []struct {
		path string
		v    Value
	}{
		{"fullscreen", Bool(true)},
		{"window_w", Int(1920)},
		{"texture_filtering", Int(0)},
		{"master_volume", Int(80)},
	} {
		if err := d.Set(ParsePath(e.path), e.v); err != nil {
			t.Fatalf("%s: %v", e.path, err)
		}
	}
	want := sm64Config
	for _, r := range [][2]string{
		{"fullscreen false", "fullscreen true"},
		{"window_w 640", "window_w 1920"},
		{"texture_filtering 1", "texture_filtering 0"},
		{"master_volume 127", "master_volume 80"},
	} {
		want = strings.Replace(want, r[0], r[1], 1)
	}
	if got := string(d.Bytes()); got != want {
		t.Errorf("edits did not land where they belong:\n%s", got)
	}
}

// A quoted value keeps its quoting, and the escapes std::quoted reads.
func TestSpaceSepQuotesStrings(t *testing.T) {
	d := ss(t, meleeCfg)
	if err := d.Set(ParsePath("disc"), String(`C:\Games\my "best" dump.ciso`)); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(ParsePath("net_name"), String("SAM")); err != nil {
		t.Fatal(err)
	}
	out := string(d.Bytes())
	if !strings.Contains(out, `disc "C:\\Games\\my \"best\" dump.ciso"`) {
		t.Errorf("escaping:\n%s", out)
	}
	back := ss(t, out)
	if v, _ := back.Get(ParsePath("disc")); v.Str != `C:\Games\my "best" dump.ciso` {
		t.Errorf("the path did not survive the round trip: %q", v.Str)
	}
	if v, _ := back.Get(ParsePath("net_name")); v.Str != "SAM" {
		t.Errorf("net_name = %q", v.Str)
	}
	// A value that cannot be one line is refused rather than written.
	if err := d.Set(ParsePath("net_name"), String("two\nlines")); !errors.Is(err, ErrUnsupportedValue) {
		t.Errorf("err = %v, want ErrUnsupportedValue", err)
	}
}

// melee-pc's operator<< drops the point on a whole float, so the same setting is
// an int in the file at one value and a decimal at the next. That is what
// SetNumber is for, and it is the schema that asks for it.
func TestSpaceSepSetNumberTakesEitherSpelling(t *testing.T) {
	d := ss(t, meleeCfg)
	n, ok := d.(NumberSetter)
	if !ok {
		t.Fatal("this grammar's numbers are untyped, so it should be a NumberSetter")
	}
	if !UntypedNumbers(FormatSpaceSeparated) {
		t.Error("UntypedNumbers should say so too")
	}
	// A decimal replacing the whole number operator<< left behind.
	if err := n.SetNumber(ParsePath("sfx_volume"), Float(0.4)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(d.Bytes()), "sfx_volume 0.4") {
		t.Errorf("got:\n%s", d.Bytes())
	}
	// And back to a whole one, as the program itself would write it.
	if err := n.SetNumber(ParsePath("sfx_volume"), Int(1)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(d.Bytes()), "sfx_volume 1\n") {
		t.Errorf("got:\n%s", d.Bytes())
	}
	// Set itself is unchanged: it still holds the two apart.
	if err := d.Set(ParsePath("sfx_volume"), Float(0.4)); !errors.Is(err, ErrKindMismatch) {
		t.Errorf("Set = %v, want ErrKindMismatch", err)
	}
	// Neither side may be something other than a number.
	if err := n.SetNumber(ParsePath("net_name"), Float(1)); !errors.Is(err, ErrKindMismatch) {
		t.Errorf("SetNumber over a string = %v, want ErrKindMismatch", err)
	}
}

func TestSpaceSepCreateAppends(t *testing.T) {
	d := ss(t, sm64Config)
	if !d.CanCreate(ParsePath("bettercam_enable")) {
		t.Fatal("a flat file's top level is always there")
	}
	if err := d.Create(ParsePath("bettercam_enable"), Bool(true)); err != nil {
		t.Fatal(err)
	}
	if err := d.Create(ParsePath("rumble_scale"), Float(1)); err != nil {
		t.Fatal(err)
	}
	got := string(d.Bytes())
	if !strings.HasSuffix(got, "skip_intro false\nbettercam_enable true\nrumble_scale 1.0\n") {
		t.Errorf("the keys did not land at the end:\n%s", got)
	}
	back := ss(t, got)
	for path, want := range map[string]string{
		"bettercam_enable": "true", "rumble_scale": "1",
		"skip_intro": "false", "master_volume": "127",
	} {
		if v, ok := back.Get(ParsePath(path)); !ok || v.Display() != want {
			t.Errorf("%s = %q, want %q", path, v.Display(), want)
		}
	}
	if v, _ := back.Get(ParsePath("key_a")); v.Raw != "0026 1000 1103" {
		t.Errorf("a binding was disturbed: %q", v.Raw)
	}
}

func TestSpaceSepCreateRefusesWhatItShould(t *testing.T) {
	d := ss(t, sm64Config)
	before := string(d.Bytes())
	if err := d.Create(ParsePath("vsync"), Bool(false)); !errors.Is(err, ErrAlreadyPresent) {
		t.Errorf("err = %v, want ErrAlreadyPresent", err)
	}
	// Nothing nests in this grammar, so a two-element path has no container.
	if d.CanCreate(Path{"a", "b"}) {
		t.Error("CanCreate should be false for a nested path")
	}
	if err := d.Create(Path{"a", "b"}, Int(1)); !errors.Is(err, ErrNoContainer) {
		t.Errorf("err = %v, want ErrNoContainer", err)
	}
	if got := string(d.Bytes()); got != before {
		t.Error("a refused Create modified the file")
	}
}

// Into a file that holds no assignment yet, the key goes above whatever comments
// or blank lines are there rather than after them.
func TestSpaceSepCreateIntoAFileWithNoEntries(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"empty":         {"", "a 1\n"},
		"only comments": {"# hi\n", "a 1\n# hi\n"},
	} {
		d := ss(t, tc.in)
		if err := d.Create(ParsePath("a"), Int(1)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := string(d.Bytes()); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
		if v, ok := d.Get(ParsePath("a")); !ok || v.Int != 1 {
			t.Errorf("%s: reads back as %+v", name, v)
		}
	}
}

// Create after an edit with a non-zero net delta: a stale end offset would put the
// new key inside the last value.
func TestSpaceSepCreateAfterEdits(t *testing.T) {
	d := ss(t, meleeCfg)
	if err := d.Set(ParsePath("disc"), String("/a/much/longer/path/to/a/melee.ciso")); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(ParsePath("anisotropy"), Int(2)); err != nil {
		t.Fatal(err)
	}
	if err := d.Create(ParsePath("crt_filter"), Int(0)); err != nil {
		t.Fatal(err)
	}
	got := string(d.Bytes())
	if !strings.HasSuffix(got, "reverb 1\ncrt_filter 0\n") {
		t.Errorf("the key did not land at the end:\n%s", got)
	}
	back := ss(t, got)
	for path, want := range map[string]string{
		"disc": "/a/much/longer/path/to/a/melee.ciso", "anisotropy": "2",
		"crt_filter": "0", "reverb": "1", "net_name": "PLAYER",
	} {
		if v, ok := back.Get(ParsePath(path)); !ok || v.Display() != want {
			t.Errorf("%s = %q, want %q", path, v.Display(), want)
		}
	}
}

// A quoted value is one value however many spaces are inside it — std::quoted
// reads it that way and melee-pc's disc path is exactly this case. A run that does
// not enclose the whole of the rest of the line is not one string.
func TestSpaceSepQuotedValueMayHoldSpaces(t *testing.T) {
	d := ss(t, `disc "/home/sam/My Games/melee.ciso"`+"\nother \"a\" \"b\"\n")
	v, ok := d.Get(ParsePath("disc"))
	if !ok || v.Kind != KindString || v.Str != "/home/sam/My Games/melee.ciso" {
		t.Errorf("disc = %+v", v)
	}
	if v, _ := d.Get(ParsePath("other")); v.Kind != KindOpaque {
		t.Errorf("other = %v, want opaque", v.Kind)
	}
}
