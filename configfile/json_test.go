package configfile

import (
	"errors"
	"strings"
	"testing"
)

// A libultraship config exactly as Ship::Config::Save writes one: nested objects,
// `dump(4)` indentation, and keys in the sorted order nlohmann's object gives
// them. Ghostship, Starship, Ship of Harkinian and SpaghettiKart all write this
// file, which is why its shape is what the parser is measured against.
//
// Two details from that class matter to the tests below. Its typed getters are
// strict — GetFloat wants is_number_float, GetInt wants is_number_integer — so a
// float that loses its decimal point stops being read at all. And a key may
// contain a space: "Game.Main Archive" is one of its own.
const shipCfg = `{
    "CVars": {
        "gAudioMaster": 1.0,
        "gEnhancements": {
            "Mods": {
                "AlternateAssets": 0
            },
            "TimeSavers": {
                "SkipIntro": 1
            }
        },
        "gGraphics": {
            "InternalResolution": 1.0,
            "MSAAValue": 1
        },
        "gWindowed": {
            "Position": [0, 0]
        }
    },
    "Game": {
        "Main Archive": "",
        "Patches Archive": "mods"
    },
    "Window": {
        "AudioBackend": "sdl",
        "Backend": {
            "Id": 0,
            "Name": "OpenGL"
        },
        "Fullscreen": {
            "Enabled": false,
            "Height": 1080,
            "Width": 1920
        },
        "Height": 720,
        "PositionX": 100,
        "PositionY": 100,
        "Width": 1280
    }
}
`

func js(t *testing.T, s string) Doc {
	t.Helper()
	d, err := Parse([]byte(s), FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestJSONReadsScalarsAndNesting(t *testing.T) {
	d := js(t, shipCfg)
	for _, tc := range []struct {
		path string
		kind Kind
		want string
	}{
		{"Window.Width", KindInt, "1280"},
		{"Window.Fullscreen.Enabled", KindBool, "false"},
		{"Window.Fullscreen.Height", KindInt, "1080"},
		{"Window.AudioBackend", KindString, "sdl"},
		{"Window.Backend.Name", KindString, "OpenGL"},
		{"CVars.gAudioMaster", KindFloat, "1"},
		{"CVars.gGraphics.MSAAValue", KindInt, "1"},
		{"CVars.gEnhancements.Mods.AlternateAssets", KindInt, "0"},
		{"CVars.gWindowed.Position", KindOpaque, "[0, 0]"},
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
	// A key containing a space, and one whose value is the empty string the
	// program treats as "unset". Both are readable; neither is a special case.
	if v, ok := d.Get(Path{"Game", "Main Archive"}); !ok || v.Kind != KindString || v.Str != "" {
		t.Errorf(`Game."Main Archive" = %+v`, v)
	}
	if v, ok := d.Get(Path{"Game", "Patches Archive"}); !ok || v.Str != "mods" {
		t.Errorf(`Game."Patches Archive" = %+v`, v)
	}

	// An object reads as its own literal as well as through its children.
	v, _ := d.Get(ParsePath("Window.Backend"))
	if v.Kind != KindOpaque || !strings.Contains(v.Raw, `"Name": "OpenGL"`) {
		t.Errorf("Window.Backend = %+v", v)
	}
	if _, ok := d.Get(ParsePath("Window.Nothing")); ok {
		t.Error("a key the file does not hold must report absent")
	}
}

// An array's elements are not addressable: a schema written against one release
// must not be able to reach into a slot that a later release reorders.
func TestJSONArrayElementsHaveNoPaths(t *testing.T) {
	d := js(t, shipCfg)
	for _, p := range d.Paths() {
		if strings.HasPrefix(p.String(), "CVars.gWindowed.Position.") {
			t.Errorf("an array element got a path: %s", p)
		}
	}
	if _, ok := d.Get(ParsePath("CVars.gWindowed.Position.0")); ok {
		t.Error("an array element must not be addressable")
	}
	err := d.Set(ParsePath("CVars.gWindowed.Position"), Int(0))
	if !errors.Is(err, ErrKindMismatch) {
		t.Errorf("Set on an array = %v, want ErrKindMismatch", err)
	}
}

func TestJSONNullIsOpaque(t *testing.T) {
	d := js(t, `{"a": null}`)
	v, ok := d.Get(ParsePath("a"))
	if !ok || v.Kind != KindOpaque || v.Raw != "null" {
		t.Errorf("a = %+v", v)
	}
	// The program recorded the absence of a value. Which kind belongs there
	// instead is its business, not this package's guess.
	if err := d.Set(ParsePath("a"), Bool(true)); !errors.Is(err, ErrKindMismatch) {
		t.Errorf("Set over null = %v, want ErrKindMismatch", err)
	}
}

func TestJSONBytesWithoutEditsIsIdentical(t *testing.T) {
	for name, in := range map[string]string{
		"as the game writes it": shipCfg,
		"compact":               `{"a":1,"b":false,"c":"x","d":{"e":[1,2]}}`,
		"empty object":          "{}\n",
		"empty nested object":   "{\n    \"a\": {}\n}\n",
		"no trailing newline":   "{\n  \"a\": 1\n}",
		"crlf":                  "{\r\n  \"a\": 1\r\n}\r\n",
		"leading bom":           "\ufeff{\n  \"a\": 1\n}\n",
		"tab indented":          "{\n\t\"a\": 1\n}\n",
	} {
		d := js(t, in)
		if got := string(d.Bytes()); got != in {
			t.Errorf("%s: reading changed the file:\n got %q\nwant %q", name, got, in)
		}
	}
}

func TestJSONRejectsWhatNoWriterEmits(t *testing.T) {
	for name, in := range map[string]string{
		"a comment":       "{\n  // note\n  \"a\": 1\n}",
		"trailing comma":  `{"a": 1,}`,
		"unquoted key":    `{a: 1}`,
		"single quotes":   `{'a': 1}`,
		"array root":      `[1, 2]`,
		"scalar root":     `42`,
		"empty":           ``,
		"missing colon":   `{"a" 1}`,
		"missing comma":   `{"a": 1 "b": 2}`,
		"unterminated":    `{"a": 1`,
		"trailing junk":   `{"a": 1} x`,
		"not a number":    `{"a": 1.2.3}`,
		"bad literal":     `{"a": tru}`,
		"unquoted string": `{"a": hello}`,
	} {
		if _, err := Parse([]byte(in), FormatJSON); err == nil {
			t.Errorf("%s: parsed %q, which no writer emits", name, in)
		}
	}
}

func TestJSONSetTouchesOnlyItsOwnValue(t *testing.T) {
	d := js(t, shipCfg)
	if err := d.Set(ParsePath("Window.Width"), Int(1920)); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(shipCfg, `"Width": 1280`, `"Width": 1920`, 1)
	if got := string(d.Bytes()); got != want {
		t.Errorf("an edit moved more than its value:\n%s", got)
	}
}

// Values inside nested objects are what the span bookkeeping has to get right:
// every parent encloses the edit, so each end moves while its start does not.
func TestJSONEditsInsideNestedObjects(t *testing.T) {
	d := js(t, shipCfg)
	for _, e := range []struct {
		path string
		v    Value
	}{
		{"CVars.gEnhancements.Mods.AlternateAssets", Int(1)},
		{"CVars.gEnhancements.TimeSavers.SkipIntro", Int(0)},
		{"CVars.gGraphics.InternalResolution", Float(1.5)},
		{"Window.Backend.Name", String("Metal")},
		{"Window.Fullscreen.Enabled", Bool(true)},
		{"Window.PositionY", Int(0)},
		{"Game.Main Archive", String("")},
	} {
		if err := d.Set(ParsePath(e.path), e.v); err != nil {
			t.Fatalf("%s: %v", e.path, err)
		}
	}
	want := shipCfg
	for _, r := range [][2]string{
		{`"AlternateAssets": 0`, `"AlternateAssets": 1`},
		{`"SkipIntro": 1`, `"SkipIntro": 0`},
		{`"InternalResolution": 1.0`, `"InternalResolution": 1.5`},
		{`"Name": "OpenGL"`, `"Name": "Metal"`},
		{`"Enabled": false`, `"Enabled": true`},
		{`"PositionY": 100`, `"PositionY": 0`},
	} {
		want = strings.Replace(want, r[0], r[1], 1)
	}
	if got := string(d.Bytes()); got != want {
		t.Errorf("edits did not land where they belong:\n%s", got)
	}
	// And the result still parses to the values that were written, which is the
	// invariant the offsets exist to keep.
	back := js(t, string(d.Bytes()))
	if v, _ := back.Get(ParsePath("CVars.gGraphics.InternalResolution")); v.Kind != KindFloat || v.Float != 1.5 {
		t.Errorf("after re-reading: InternalResolution = %+v", v)
	}
	if v, _ := back.Get(Path{"Game", "Main Archive"}); v.Str != "" {
		t.Errorf("after re-reading: Main Archive = %+v", v)
	}
}

func TestJSONSetRefusesAKindChange(t *testing.T) {
	d := js(t, shipCfg)
	for _, tc := range []struct {
		path string
		v    Value
	}{
		{"Window.Width", String("1280")},
		{"Window.Fullscreen.Enabled", Int(1)},
		{"Window.AudioBackend", Int(0)},
		{"Window.Backend", String("OpenGL")},
		{"CVars.gAudioMaster", Int(1)},
	} {
		if err := d.Set(ParsePath(tc.path), tc.v); !errors.Is(err, ErrKindMismatch) {
			t.Errorf("Set(%s, %v) = %v, want ErrKindMismatch", tc.path, tc.v.Kind, err)
		}
	}
	if got := string(d.Bytes()); got != shipCfg {
		t.Error("a refused Set changed the file")
	}
}

// JSON has one number type but the program reading the file does not:
// libultraship's GetFloat ignores a value that is not is_number_float, so a float
// that came back as `2` would silently stop being read.
func TestJSONFloatKeepsItsPoint(t *testing.T) {
	d := js(t, `{"vol": 1.0, "res": 2.5}`)
	if err := d.Set(ParsePath("vol"), Float(2)); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(ParsePath("res"), Float(1)); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != `{"vol": 2.0, "res": 1.0}` {
		t.Errorf("got %s", got)
	}
	back := js(t, string(d.Bytes()))
	if v, _ := back.Get(ParsePath("vol")); v.Kind != KindFloat {
		t.Errorf("a float read back as %v", v.Kind)
	}
}

func TestJSONStringEscaping(t *testing.T) {
	d := js(t, `{"path": "x", "name": "y"}`)
	if err := d.Set(ParsePath("path"), String(`C:\Games\"N64"`+"\n\t")); err != nil {
		t.Fatal(err)
	}
	// Text above the control range stays as the UTF-8 the program itself writes.
	if err := d.Set(ParsePath("name"), String("Zelda – 日本語")); err != nil {
		t.Fatal(err)
	}
	want := `{"path": "C:\\Games\\\"N64\"\n\t", "name": "Zelda – 日本語"}`
	if got := string(d.Bytes()); got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
	back := js(t, string(d.Bytes()))
	if v, _ := back.Get(ParsePath("path")); v.Str != `C:\Games\"N64"`+"\n\t" {
		t.Errorf("path did not survive the round trip: %q", v.Str)
	}
	if v, _ := back.Get(ParsePath("name")); v.Str != "Zelda – 日本語" {
		t.Errorf("name did not survive the round trip: %q", v.Str)
	}
}

// The escapes this package does not emit still have to be readable, because the
// program on the other side does emit them.
func TestJSONReadsEscapesItDoesNotWrite(t *testing.T) {
	d := js(t, `{"a": "\u0041\u00e9\ud83c\udfae\/", "b": "\b\f"}`)
	if v, _ := d.Get(ParsePath("a")); v.Str != "Aé🎮/" {
		t.Errorf("a = %q", v.Str)
	}
	if v, _ := d.Get(ParsePath("b")); v.Str != "\b\f" {
		t.Errorf("b = %q", v.Str)
	}
}

func TestJSONPathsAreInFileOrder(t *testing.T) {
	d := js(t, `{"b": 1, "a": {"d": 2, "c": 3}}`)
	var got []string
	for _, p := range d.Paths() {
		got = append(got, p.String())
	}
	want := []string{"b", "a.d", "a.c", "a"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Paths() = %v, want %v", got, want)
	}
}
