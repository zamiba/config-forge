package configfile

import (
	"errors"
	"strings"
	"testing"
)

// dbz3_user.toml as rex::cvar::SaveConfig writes one: its own comment line, then
// flat `name = value` for each flag that differs from its default, and nothing
// else. No headers, because SerializeToTOML emits none. A double's value comes
// from std::to_string, which is why the sharpness has six decimal places.
const rexToml = `# Auto-generated cvar configuration
dbz3_resolution_scale = 2
dbz3_language = 3
dbz3_region = "eu"
dbz3_vsync = false
dbz3_frame_cap = 120
dbz3_fsr_sharpness = 0.200000
dbz3_present_effect = "fsr"
dbz3_game_dir = "D:\\Games\\DBZ"
`

// A TOML file with everything this package locates but will not rewrite, laid out
// so a mistake in any one span would corrupt a neighbour.
const tomlOddities = `title = "start"

[server]
ports = [
  8000,   # first
  8001,
]
limits = { max = 5, min = 1 }
motto = """
a multi-line
string with ] and } in it
"""
raw = 'C:\no\escapes'
started = 2026-09-24T10:00:00Z
ratio = 0.5
big = 1_000_000
mask = 0xdead
after = "end"
`

func tml(t *testing.T, s string) Doc {
	t.Helper()
	d, err := Parse([]byte(s), FormatTOML)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestTOMLReadsAFlatRexFile(t *testing.T) {
	d := tml(t, rexToml)
	for _, tc := range []struct {
		path string
		kind Kind
		want string
	}{
		{"dbz3_resolution_scale", KindInt, "2"},
		{"dbz3_language", KindInt, "3"},
		{"dbz3_region", KindString, "eu"},
		{"dbz3_vsync", KindBool, "false"},
		{"dbz3_frame_cap", KindInt, "120"},
		{"dbz3_fsr_sharpness", KindFloat, "0.2"},
		{"dbz3_present_effect", KindString, "fsr"},
		{"dbz3_game_dir", KindString, `D:\Games\DBZ`},
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
	// A flag at its default is simply not in the file, because SerializeToTOML
	// writes only what differs.
	if _, ok := d.Get(ParsePath("dbz3_mute")); ok {
		t.Error("a flag the file does not hold must report absent")
	}
}

func TestTOMLReadsTablesAndDottedKeys(t *testing.T) {
	d := tml(t, "a = 1\n\n[x]\nb = 2\n\n[x.y]\nc = 3\nd.e = 4\n\"f.g\" = 5\n")
	for path, want := range map[string]string{
		"a": "1", "x.b": "2", "x.y.c": "3", "x.y.d.e": "4",
	} {
		if v, ok := d.Get(ParsePath(path)); !ok || v.Display() != want {
			t.Errorf("%s = %q (found %v), want %q", path, v.Display(), ok, want)
		}
	}
	// A quoted key containing a dot is one element, which a dotted pointer cannot
	// express — the same reason Path is a slice.
	if v, ok := d.Get(Path{"x", "y", "f.g"}); !ok || v.Int != 5 {
		t.Errorf(`x.y."f.g" = %+v`, v)
	}
	if _, ok := d.Get(ParsePath("x.y.f.g")); ok {
		t.Error("a dotted path must not reach through a quoted key that contains a dot")
	}
}

// Everything this package locates but will not rewrite, spanned across whatever
// lines it occupies. A span that stopped at the end of its first line would put a
// later edit inside somebody's array.
func TestTOMLSpansWhatItWillNotRewrite(t *testing.T) {
	d := tml(t, tomlOddities)
	for _, tc := range []struct{ path, contains string }{
		{"server.ports", "8001"},
		{"server.limits", "max = 5"},
		{"server.motto", "] and }"},
		{"server.started", "2026-09-24T10:00:00Z"},
		{"server.mask", "0xdead"},
	} {
		v, ok := d.Get(ParsePath(tc.path))
		if !ok {
			t.Errorf("%s: not found", tc.path)
			continue
		}
		if v.Kind != KindOpaque {
			t.Errorf("%s = %v, want opaque", tc.path, v.Kind)
		}
		if !strings.Contains(v.Raw, tc.contains) {
			t.Errorf("%s raw = %q, want it to contain %q", tc.path, v.Raw, tc.contains)
		}
		if err := d.Set(ParsePath(tc.path), Int(1)); !errors.Is(err, ErrKindMismatch) {
			t.Errorf("%s: Set = %v, want ErrKindMismatch", tc.path, err)
		}
	}
	// The values around them still read correctly, which is the real proof the
	// spans are right.
	for path, want := range map[string]string{
		"title": "start", "server.raw": `C:\no\escapes`,
		"server.ratio": "0.5", "server.big": "1000000", "server.after": "end",
	} {
		if v, ok := d.Get(ParsePath(path)); !ok || v.Display() != want {
			t.Errorf("%s = %q (found %v), want %q", path, v.Display(), ok, want)
		}
	}
	// A multi-line string is spanned whole, delimiters included, so the lines
	// inside it are never mistaken for assignments of their own.
	if v, _ := d.Get(ParsePath("server.motto")); !strings.HasPrefix(v.Raw, `"""`) ||
		!strings.HasSuffix(v.Raw, `"""`) {
		t.Errorf("motto raw = %q", v.Raw)
	}
	if _, ok := d.Get(ParsePath("string")); ok {
		t.Error("a line inside a multi-line string was read as an assignment")
	}
}

func TestTOMLBytesWithoutEditsIsIdentical(t *testing.T) {
	for name, in := range map[string]string{
		"as the sdk writes it": rexToml,
		"oddities":             tomlOddities,
		"empty":                "",
		"comments only":        "# nothing\n",
		"no trailing newline":  "[a]\nb = 1",
		"crlf":                 "a = 1\r\nb = 2\r\n",
		"inline comment":       "a = 1 # note\n",
	} {
		d := tml(t, in)
		if got := string(d.Bytes()); got != in {
			t.Errorf("%s: reading changed the file:\n got %q\nwant %q", name, got, in)
		}
	}
}

// An array of tables has no single place a path can address, so the file is
// refused rather than half-read.
func TestTOMLRefusesWhatItCannotAddress(t *testing.T) {
	for name, in := range map[string]string{
		"array of tables":     "[[product]]\nname = \"a\"\n\n[[product]]\nname = \"b\"\n",
		"unterminated header": "[server\na = 1\n",
		"unterminated array":  "a = [1, 2\n",
		"unterminated string": "a = \"oops\n",
		"no value":            "a =\n",
		"no value, comment":   "a = # nothing\n",
		"not an assignment":   "just words\n",
		"mismatched brackets": "a = [1, 2}\n",
	} {
		if _, err := Parse([]byte(in), FormatTOML); err == nil {
			t.Errorf("%s: parsed %q", name, in)
		}
	}
}

func TestTOMLSetTouchesOnlyItsOwnValue(t *testing.T) {
	d := tml(t, rexToml)
	for _, e := range []struct {
		path string
		v    Value
	}{
		{"dbz3_resolution_scale", Int(3)},
		{"dbz3_vsync", Bool(true)},
		{"dbz3_region", String("us")},
		{"dbz3_fsr_sharpness", Float(0.45)},
		{"dbz3_game_dir", String(`E:\DBZ`)},
	} {
		if err := d.Set(ParsePath(e.path), e.v); err != nil {
			t.Fatalf("%s: %v", e.path, err)
		}
	}
	want := rexToml
	for _, r := range [][2]string{
		{"dbz3_resolution_scale = 2", "dbz3_resolution_scale = 3"},
		{"dbz3_vsync = false", "dbz3_vsync = true"},
		{`dbz3_region = "eu"`, `dbz3_region = "us"`},
		{"dbz3_fsr_sharpness = 0.200000", "dbz3_fsr_sharpness = 0.45"},
		{`dbz3_game_dir = "D:\\Games\\DBZ"`, `dbz3_game_dir = "E:\\DBZ"`},
	} {
		want = strings.Replace(want, r[0], r[1], 1)
	}
	if got := string(d.Bytes()); got != want {
		t.Errorf("edits did not land where they belong:\n%s", got)
	}
	back := tml(t, string(d.Bytes()))
	if v, _ := back.Get(ParsePath("dbz3_game_dir")); v.Str != `E:\DBZ` {
		t.Errorf("the path did not survive the round trip: %q", v.Str)
	}
	// A float keeps its point, or it comes back as TOML's other number type.
	if v, _ := back.Get(ParsePath("dbz3_fsr_sharpness")); v.Kind != KindFloat {
		t.Errorf("the float read back as %v", v.Kind)
	}
}

// A literal string has no escapes by definition, so it keeps its own form where
// the new value allows and becomes a basic string where it does not.
func TestTOMLKeepsAStringsOwnForm(t *testing.T) {
	d := tml(t, "a = 'C:\\raw'\nb = 'plain'\n")
	if err := d.Set(ParsePath("a"), String(`D:\raw`)); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(ParsePath("b"), String("it's quoted")); err != nil {
		t.Fatal(err)
	}
	const want = "a = 'D:\\raw'\nb = \"it's quoted\"\n"
	if got := string(d.Bytes()); got != want {
		t.Errorf("\n got %q\nwant %q", got, want)
	}
	back := tml(t, string(d.Bytes()))
	if v, _ := back.Get(ParsePath("a")); v.Str != `D:\raw` {
		t.Errorf("a = %q", v.Str)
	}
	if v, _ := back.Get(ParsePath("b")); v.Str != "it's quoted" {
		t.Errorf("b = %q", v.Str)
	}
}

func TestTOMLSetRefusesAKindChange(t *testing.T) {
	d := tml(t, rexToml)
	before := string(d.Bytes())
	for _, tc := range []struct {
		path string
		v    Value
	}{
		{"dbz3_vsync", Int(1)},
		{"dbz3_frame_cap", String("120")},
		{"dbz3_region", Int(0)},
		{"dbz3_fsr_sharpness", Int(1)},
	} {
		if err := d.Set(ParsePath(tc.path), tc.v); !errors.Is(err, ErrKindMismatch) {
			t.Errorf("Set(%s) = %v, want ErrKindMismatch", tc.path, err)
		}
	}
	if got := string(d.Bytes()); got != before {
		t.Error("a refused Set changed the file")
	}
}

// rex writes only the flags that differ from their defaults, so most settings are
// absent and adding them is the ordinary case, not the exception.
func TestTOMLCreateAddsAFlagTheFileLacks(t *testing.T) {
	d := tml(t, rexToml)
	if !d.CanCreate(ParsePath("dbz3_mute")) {
		t.Fatal("a flat file's top level is always there")
	}
	if err := d.Create(ParsePath("dbz3_mute"), Bool(false)); err != nil {
		t.Fatal(err)
	}
	if err := d.Create(ParsePath("dbz3_master_volume"), Float(1)); err != nil {
		t.Fatal(err)
	}
	got := string(d.Bytes())
	if !strings.HasSuffix(got, "dbz3_mute = false\ndbz3_master_volume = 1.0\n") {
		t.Errorf("the flags did not land at the end:\n%s", got)
	}
	if _, err := Parse([]byte(got), FormatTOML); err != nil {
		t.Fatalf("the result no longer parses: %v", err)
	}
	back := tml(t, got)
	for path, want := range map[string]string{
		"dbz3_mute": "false", "dbz3_master_volume": "1",
		"dbz3_region": "eu", "dbz3_game_dir": `D:\Games\DBZ`,
	} {
		if v, ok := back.Get(ParsePath(path)); !ok || v.Display() != want {
			t.Errorf("%s = %q, want %q", path, v.Display(), want)
		}
	}
}

func TestTOMLCreateJoinsItsOwnTable(t *testing.T) {
	d := tml(t, "top = 1\n\n[a]\nx = 1\n\n[b]\ny = 2\n")
	if err := d.Create(ParsePath("a.z"), Int(9)); err != nil {
		t.Fatal(err)
	}
	if err := d.Create(ParsePath("second"), Int(8)); err != nil {
		t.Fatal(err)
	}
	const want = "top = 1\nsecond = 8\n\n[a]\nx = 1\nz = 9\n\n[b]\ny = 2\n"
	if got := string(d.Bytes()); got != want {
		t.Errorf("\n got %q\nwant %q", got, want)
	}
	if _, err := Parse(d.Bytes(), FormatTOML); err != nil {
		t.Errorf("the result no longer parses: %v", err)
	}
}

func TestTOMLCreateRefusesAMissingTable(t *testing.T) {
	d := tml(t, rexToml)
	before := string(d.Bytes())
	if d.CanCreate(ParsePath("graphics.msaa")) {
		t.Error("CanCreate should be false for a table the file lacks")
	}
	if err := d.Create(ParsePath("graphics.msaa"), Int(1)); !errors.Is(err, ErrNoContainer) {
		t.Errorf("err = %v, want ErrNoContainer", err)
	}
	if err := d.Create(ParsePath("dbz3_vsync"), Bool(true)); !errors.Is(err, ErrAlreadyPresent) {
		t.Errorf("err = %v, want ErrAlreadyPresent", err)
	}
	if got := string(d.Bytes()); got != before {
		t.Error("a refused Create modified the file")
	}
}

// Into a file with nothing in it, and into one whose first line is a header.
func TestTOMLCreateAtTheTopOfTheFile(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"empty":         {"", "a = 1\n"},
		"only a header": {"[x]\nb = 2\n", "a = 1\n[x]\nb = 2\n"},
		"only comments": {"# hi\n", "a = 1\n# hi\n"},
	} {
		d := tml(t, tc.in)
		if err := d.Create(ParsePath("a"), Int(1)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := string(d.Bytes()); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
		if _, err := Parse(d.Bytes(), FormatTOML); err != nil {
			t.Errorf("%s: the result no longer parses: %v", name, err)
		}
	}
}

// Create after Set, with a non-zero net delta, so a stale table offset would put
// the new key inside another value or past the end.
func TestTOMLCreateAfterEdits(t *testing.T) {
	d := tml(t, tomlOddities)
	if err := d.Set(ParsePath("title"), String("a much longer title")); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(ParsePath("server.ratio"), Float(0.1)); err != nil {
		t.Fatal(err)
	}
	if err := d.Create(ParsePath("server.added"), Int(7)); err != nil {
		t.Fatal(err)
	}
	got := string(d.Bytes())
	if !strings.Contains(got, `after = "end"`+"\nadded = 7") {
		t.Errorf("the key did not land at the end of [server]:\n%s", got)
	}
	back := tml(t, got)
	for path, want := range map[string]string{
		"title": "a much longer title", "server.ratio": "0.1", "server.added": "7",
		"server.after": "end", "server.raw": `C:\no\escapes`,
	} {
		if v, ok := back.Get(ParsePath(path)); !ok || v.Display() != want {
			t.Errorf("%s = %q, want %q", path, v.Display(), want)
		}
	}
	if v, _ := back.Get(ParsePath("server.ports")); !strings.Contains(v.Raw, "8001") {
		t.Errorf("the array was disturbed: %q", v.Raw)
	}
}

func TestTOMLReadsEscapesItDoesNotWrite(t *testing.T) {
	d := tml(t, `a = "\u00e9\U0001F3AE\t"`+"\n")
	if v, _ := d.Get(ParsePath("a")); v.Str != "é🎮\t" {
		t.Errorf("a = %q", v.Str)
	}
}
