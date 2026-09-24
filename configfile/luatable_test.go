package configfile

import (
	"errors"
	"strings"
	"testing"
)

// options.lua exactly as Gen1Recomp's SaveSerializer writes one: `return ` then
// two-space indentation, keys sorted, a trailing comma on every entry. The keys
// and values are real ones from SaveData.defaultOptions.
const gen1 = `return {
  animations = true,
  battleBg = "white",
  battleStyle = "shift",
  cartOptions = {},
  faithfulRes = 0,
  fpsCap = 60,
  hotbar = true,
  logicClock = "60",
  modOptions = {
    ["some.mod"] = {
      difficulty = "hard",
      level = 3,
    },
  },
  mods = {},
  musicVol = 7,
  safeMode = false,
  saveSync = {
    enabled = false,
    lastSyncAt = 0,
  },
  textSpeed = 3,
  tilt = 0,
  touchControls = {
    enabled = true,
  },
}
`

func lua(t *testing.T, s string) Doc {
	t.Helper()
	d, err := Parse([]byte(s), FormatLuaTable)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestLuaReadsScalarsAndNesting(t *testing.T) {
	d := lua(t, gen1)
	for _, tc := range []struct {
		path string
		kind Kind
		want string
	}{
		{"animations", KindBool, "true"},
		{"safeMode", KindBool, "false"},
		{"battleBg", KindString, "white"},
		{"logicClock", KindString, "60"},
		{"musicVol", KindInt, "7"},
		{"faithfulRes", KindInt, "0"},
		{"touchControls.enabled", KindBool, "true"},
		{"saveSync.lastSyncAt", KindInt, "0"},
		{"cartOptions", KindOpaque, "{}"},
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
	// A key containing a dot is why Path is a slice: ParsePath cannot express
	// ["some.mod"], and splitting on dots would address the wrong thing.
	if v, ok := d.Get(Path{"modOptions", "some.mod", "difficulty"}); !ok || v.Str != "hard" {
		t.Errorf(`modOptions["some.mod"].difficulty = %+v`, v)
	}
	if v, ok := d.Get(Path{"modOptions", "some.mod", "level"}); !ok || v.Int != 3 {
		t.Errorf(`modOptions["some.mod"].level = %+v`, v)
	}
	if _, ok := d.Get(ParsePath("modOptions.some.mod.difficulty")); ok {
		t.Error("a dotted path must not reach through a key that contains a dot")
	}

	// A nested table reads as its own literal as well as through its children.
	v, _ := d.Get(ParsePath("touchControls"))
	if v.Kind != KindOpaque || !strings.Contains(v.Raw, "enabled = true") {
		t.Errorf("touchControls = %+v", v)
	}
}

func TestLuaBytesWithoutEditsIsIdentical(t *testing.T) {
	for name, in := range map[string]string{
		"as the game writes it": gen1,
		"hand edited":           "-- notes\nreturn {\n  a = 1,   -- inline\n  b = \"x\",\n}\n",
		"compact":               `return {a=1,b=false,c="x"}`,
		"empty table":           "return {}\n",
		"no trailing newline":   "return {\n  a = 1,\n}",
		"semicolon separators":  "return {\n  a = 1;\n  b = 2;\n}\n",
	} {
		d := lua(t, in)
		if got := string(d.Bytes()); got != in {
			t.Errorf("%s: reading changed the file:\n got %q\nwant %q", name, got, in)
		}
	}
}

func TestLuaSetTouchesOnlyItsOwnValue(t *testing.T) {
	d := lua(t, gen1)
	if err := d.Set(ParsePath("musicVol"), Int(3)); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(gen1, "musicVol = 7,", "musicVol = 3,", 1)
	if got := string(d.Bytes()); got != want {
		t.Errorf("an edit moved more than its value:\n%s", got)
	}
}

// A value inside a nested table is the case the span bookkeeping has to get
// right: its parents enclose it, so their ends move while their starts do not.
func TestLuaEditInsideNestedTables(t *testing.T) {
	d := lua(t, gen1)
	if err := d.Set(Path{"modOptions", "some.mod", "difficulty"}, String("normal")); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(ParsePath("touchControls.enabled"), Bool(false)); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(ParsePath("textSpeed"), Int(1)); err != nil {
		t.Fatal(err)
	}
	want := gen1
	want = strings.Replace(want, `difficulty = "hard"`, `difficulty = "normal"`, 1)
	want = strings.Replace(want, "enabled = true", "enabled = false", 1)
	want = strings.Replace(want, "textSpeed = 3", "textSpeed = 1", 1)
	if got := string(d.Bytes()); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	// Every other value still reads correctly from the moved buffer, and the
	// enclosing tables still bracket their children.
	for path, want := range map[string]string{
		"saveSync.lastSyncAt": "0",
		"musicVol":            "7",
		"animations":          "true",
	} {
		if v, _ := d.Get(ParsePath(path)); v.Display() != want {
			t.Errorf("%s = %q, want %q", path, v.Display(), want)
		}
	}
	if v, _ := d.Get(Path{"modOptions", "some.mod", "level"}); v.Int != 3 {
		t.Errorf("a sibling inside the edited table = %+v", v)
	}
	if v, _ := d.Get(ParsePath("modOptions")); !strings.Contains(v.Raw, `difficulty = "normal"`) || !strings.Contains(v.Raw, "level = 3") {
		t.Errorf("the parent table's span no longer covers its children: %q", v.Raw)
	}
}

func TestLuaRefusesWhatItCannotDo(t *testing.T) {
	d := lua(t, gen1)
	before := string(d.Bytes())
	if err := d.Set(ParsePath("cartOptions"), String("{}")); !errors.Is(err, ErrKindMismatch) {
		t.Errorf("string over a table: %v", err)
	}
	if err := d.Set(ParsePath("musicVol"), Bool(true)); !errors.Is(err, ErrKindMismatch) {
		t.Errorf("bool over an int: %v", err)
	}
	if err := d.Set(ParsePath("nothingHere"), Int(1)); !errors.Is(err, ErrNoSuchPath) {
		t.Errorf("unknown path: %v", err)
	}
	if got := string(d.Bytes()); got != before {
		t.Error("a refused Set modified the file")
	}
}

// The grammar is the safety property: anything that is a program rather than
// data must fail to parse, not be half-understood.
func TestLuaRefusesAnythingThatIsNotData(t *testing.T) {
	for name, in := range map[string]string{
		"no return":       `{ a = 1 }`,
		"function call":   `return { a = os.time() }`,
		"concatenation":   `return { a = "x" .. "y" }`,
		"arithmetic":      `return { a = 1 + 2 }`,
		"sequence entry":  `return { "first", "second" }`,
		"a whole program": "local x = 1\nreturn { a = x }",
		"unterminated":    `return { a = 1`,
		"trailing junk":   "return { a = 1 }\nprint(1)\n",
		"not a table":     `return 5`,
		"unclosed string": `return { a = "x }`,
	} {
		if _, err := Parse([]byte(in), FormatLuaTable); err == nil {
			t.Errorf("%s: %q should have been refused", name, in)
		}
	}
}

func TestLuaStringEscapes(t *testing.T) {
	d := lua(t, "return {\n  quoted = \"say \\\"hi\\\"\",\n  win = \"C:\\\\Games\",\n  nl = \"a\\nb\",\n  ctrl = \"a\\7b\",\n}\n")
	for path, want := range map[string]string{
		"quoted": `say "hi"`,
		"win":    `C:\Games`,
		"nl":     "a\nb",
		"ctrl":   "a\x07b",
	} {
		v, ok := d.Get(ParsePath(path))
		if !ok || v.Kind != KindString || v.Str != want {
			t.Errorf("%s = %+v, want %q", path, v, want)
		}
	}
	// Written back in the decimal form the program's own writer uses.
	if err := d.Set(ParsePath("ctrl"), String("x\x01y")); err != nil {
		t.Fatal(err)
	}
	if v, _ := d.Get(ParsePath("ctrl")); v.Raw != `"x\1y"` || v.Str != "x\x01y" {
		t.Errorf("control character written as %q, read back as %q", v.Raw, v.Str)
	}
}

func TestLuaBracketedKeys(t *testing.T) {
	d := lua(t, "return {\n  [1] = \"first\",\n  [\"with space\"] = 2,\n  plain = 3,\n}\n")
	for path, want := range map[string]string{
		"1":          "first",
		"with space": "2",
		"plain":      "3",
	} {
		if v, ok := d.Get(ParsePath(path)); !ok || v.Display() != want {
			t.Errorf("%s = %+v, want %q", path, v, want)
		}
	}
	if err := d.Set(ParsePath("with space"), Int(9)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(d.Bytes()), `["with space"] = 9`) {
		t.Errorf("bracketed key edit: %s", d.Bytes())
	}
}
