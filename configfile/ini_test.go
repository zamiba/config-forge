package configfile

import (
	"errors"
	"strings"
	"testing"
)

// interface.ini as Crash Bandicoot Recompiled's ConfigManager.SaveView writes
// one: a [RecompOne] header over .NET's own ToString spellings, then a blank
// line and the ImGui layout blob, appended verbatim. The blob is the reason this
// parser has to leave what it does not recognise exactly where it is.
const crashIni = `[RecompOne]
HideTopBar=False
Fullscreen=False
InternalResolution=4
NativeResolution=False
TextureFilter=2
TextureFilterStrength=0.5
Widescreen=True
IntegerScale=False
VSync=True
FrameRate=60
ShowDevHud=False
CheatMenuKey=
DevMenuSection=cheats
Panels.Output=True

[Window][Debug##Default]
Pos=60,60
Size=400,400
Collapsed=0
`

// pikmin_settings.conf as Open Nectar writes one: no headers at all, a comment
// at the top, spaces around every equals sign.
const nectarConf = `# Open Nectar settings (F1 in-game to change)
windowWidth = 1280
windowHeight = 720
displayMode = 0
aspectRatioMode = 0
refreshRate = 0
vsync = 1
language = en
renderScale = 0.666667
fpsMode = 0
naviHealthPct = 100
`

func ini(t *testing.T, s string) Doc {
	t.Helper()
	d, err := Parse([]byte(s), FormatINI)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestINIReadsValuesByTheirText(t *testing.T) {
	d := ini(t, crashIni)
	for _, tc := range []struct {
		path string
		kind Kind
		want string
	}{
		{"RecompOne.Widescreen", KindBool, "true"},
		{"RecompOne.HideTopBar", KindBool, "false"},
		{"RecompOne.InternalResolution", KindInt, "4"},
		{"RecompOne.TextureFilterStrength", KindFloat, "0.5"},
		{"RecompOne.DevMenuSection", KindString, "cheats"},
		{"RecompOne.CheatMenuKey", KindString, ""},
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
	// A key with a dot in it: the section is one element and the key is the
	// next, whatever the key happens to contain.
	if v, ok := d.Get(Path{"RecompOne", "Panels.Output"}); !ok || !v.Bool {
		t.Errorf("Panels.Output = %+v", v)
	}
	// The ImGui blob parses as sections of its own. Nothing here writes to them,
	// and reading them must not fail.
	if v, ok := d.Get(Path{"Window][Debug##Default", "Pos"}); !ok || v.Str != "60,60" {
		t.Errorf("the imgui blob should read as ordinary text: %+v", v)
	}
}

func TestINIReadsAHeaderlessFile(t *testing.T) {
	d := ini(t, nectarConf)
	for _, tc := range []struct {
		path string
		kind Kind
		want string
	}{
		{"windowWidth", KindInt, "1280"},
		{"vsync", KindInt, "1"},
		{"language", KindString, "en"},
		{"renderScale", KindFloat, "0.666667"},
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
}

// A word that a float parser would take is far more likely to be somebody's
// word: a language code, a preset name, a backend.
func TestINIDoesNotReadWordsAsNumbers(t *testing.T) {
	d := ini(t, "a = inf\nb = nan\nc = 0x10\nd = eu\ne = 1e3\n")
	for path, kind := range map[string]Kind{
		"a": KindString, "b": KindString, "c": KindString, "d": KindString,
		"e": KindFloat,
	} {
		if v, _ := d.Get(ParsePath(path)); v.Kind != kind {
			t.Errorf("%s = %v, want %v", path, v.Kind, kind)
		}
	}
}

func TestINIBytesWithoutEditsIsIdentical(t *testing.T) {
	for name, in := range map[string]string{
		"crash":               crashIni,
		"open nectar":         nectarConf,
		"empty":               "",
		"comments only":       "# nothing here\n; nor here\n",
		"no trailing newline": "[a]\nb=1",
		"crlf":                "[a]\r\nb = 1\r\n",
		"junk lines":          "[a]\nb=1\nnot an assignment\n\n[c]\n",
	} {
		d := ini(t, in)
		if got := string(d.Bytes()); got != in {
			t.Errorf("%s: reading changed the file:\n got %q\nwant %q", name, got, in)
		}
	}
}

func TestINISetKeepsTheFilesOwnSpelling(t *testing.T) {
	d := ini(t, crashIni)
	// .NET writes True and False, and a program that writes True may be one that
	// only reads True, so the capitalisation is kept.
	if err := d.Set(ParsePath("RecompOne.Widescreen"), Bool(false)); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(ParsePath("RecompOne.HideTopBar"), Bool(true)); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(ParsePath("RecompOne.InternalResolution"), Int(8)); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(ParsePath("RecompOne.TextureFilterStrength"), Float(0.75)); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(ParsePath("RecompOne.DevMenuSection"), String("gpu")); err != nil {
		t.Fatal(err)
	}
	want := crashIni
	for _, r := range [][2]string{
		{"Widescreen=True", "Widescreen=False"},
		{"HideTopBar=False", "HideTopBar=True"},
		{"InternalResolution=4", "InternalResolution=8"},
		{"TextureFilterStrength=0.5", "TextureFilterStrength=0.75"},
		{"DevMenuSection=cheats", "DevMenuSection=gpu"},
	} {
		want = strings.Replace(want, r[0], r[1], 1)
	}
	if got := string(d.Bytes()); got != want {
		t.Errorf("edits did not land where they belong:\n%s", got)
	}
	// Lower case in, lower case out.
	e := ini(t, "vsync = true\n")
	if err := e.Set(ParsePath("vsync"), Bool(false)); err != nil {
		t.Fatal(err)
	}
	if got := string(e.Bytes()); got != "vsync = false\n" {
		t.Errorf("got %q", got)
	}
}

func TestINISetRefusesAKindChangeAndAnUnwritableString(t *testing.T) {
	d := ini(t, crashIni)
	before := string(d.Bytes())
	for name, tc := range map[string]struct {
		path string
		v    Value
	}{
		"bool as int":     {"RecompOne.Widescreen", Int(1)},
		"int as string":   {"RecompOne.FrameRate", String("60")},
		"string as float": {"RecompOne.DevMenuSection", Float(1.5)},
	} {
		if err := d.Set(ParsePath(tc.path), tc.v); !errors.Is(err, ErrKindMismatch) {
			t.Errorf("%s: err = %v, want ErrKindMismatch", name, err)
		}
	}
	// A value the format cannot hold on one unquoted line.
	for name, s := range map[string]string{
		"a newline":        "a\nb",
		"a leading space":  " x",
		"a trailing space": "x ",
		"a comment start":  "# x",
		"a header start":   "[x]",
	} {
		if err := d.Set(ParsePath("RecompOne.DevMenuSection"), String(s)); !errors.Is(err, ErrUnsupportedValue) {
			t.Errorf("%s: err = %v, want ErrUnsupportedValue", name, err)
		}
	}
	if got := string(d.Bytes()); got != before {
		t.Error("a refused Set changed the file")
	}
}

func TestINICreateJoinsItsOwnSection(t *testing.T) {
	d := ini(t, crashIni)
	if !d.CanCreate(ParsePath("RecompOne.Dedither")) {
		t.Fatal("[RecompOne] exists, so a key can be added to it")
	}
	if err := d.Create(ParsePath("RecompOne.Dedither"), Bool(true)); err != nil {
		t.Fatal(err)
	}
	// After the section's last key, above the blank line and the ImGui blob.
	if !strings.Contains(string(d.Bytes()), "Panels.Output=True\nDedither=true\n\n[Window]") {
		t.Errorf("the key did not land at the end of [RecompOne]:\n%s", d.Bytes())
	}
	if v, ok := d.Get(ParsePath("RecompOne.Dedither")); !ok || !v.Bool {
		t.Errorf("reads back as %+v", v)
	}
	// The created key edits like any other, and nothing else moved.
	if err := d.Set(ParsePath("RecompOne.Dedither"), Bool(false)); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"RecompOne.Dedither": "false", "RecompOne.FrameRate": "60",
		"RecompOne.Widescreen": "true", "Window][Debug##Default.Pos": "60,60",
	} {
		if v, _ := d.Get(ParsePath(path)); v.Display() != want {
			t.Errorf("%s = %q, want %q", path, v.Display(), want)
		}
	}
}

func TestINICreateMatchesTheFilesSpacing(t *testing.T) {
	d := ini(t, nectarConf)
	if err := d.Create(ParsePath("fog"), Int(1)); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); !strings.HasSuffix(got, "naviHealthPct = 100\nfog = 1\n") {
		t.Errorf("spacing or position:\n%s", got)
	}
	// A file with no final newline keeps its shape too.
	e := ini(t, "[a]\nb=1")
	if err := e.Create(ParsePath("a.c"), Int(2)); err != nil {
		t.Fatal(err)
	}
	if got := string(e.Bytes()); got != "[a]\nb=1\nc=2" {
		t.Errorf("got %q", got)
	}
}

func TestINICreateRefusesAMissingSection(t *testing.T) {
	d := ini(t, crashIni)
	before := string(d.Bytes())
	if d.CanCreate(ParsePath("Audio.Volume")) {
		t.Error("CanCreate should be false for a section the file lacks")
	}
	if err := d.Create(ParsePath("Audio.Volume"), Int(1)); !errors.Is(err, ErrNoContainer) {
		t.Errorf("err = %v, want ErrNoContainer", err)
	}
	if err := d.Create(ParsePath("RecompOne.VSync"), Bool(true)); !errors.Is(err, ErrAlreadyPresent) {
		t.Errorf("err = %v, want ErrAlreadyPresent", err)
	}
	if err := d.Create(Path{"a", "b", "c"}, Int(1)); !errors.Is(err, ErrNoContainer) {
		t.Errorf("nothing nests in this grammar: err = %v", err)
	}
	if got := string(d.Bytes()); got != before {
		t.Error("a refused Create modified the file")
	}
}

// A headerless key in a file whose first line is a header, and in one that has
// nothing but comments: both have somewhere to go.
func TestINICreateAtTheTopOfTheFile(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"only a header": {"[a]\nb=1\n", "c=2\n[a]\nb=1\n"},
		// With no assignment to copy, the spacing falls back to the readable form.
		"only comments":  {"# hello\n", "c = 2\n# hello\n"},
		"nothing at all": {"", "c = 2\n"},
	} {
		d := ini(t, tc.in)
		if err := d.Create(ParsePath("c"), Int(2)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := string(d.Bytes()); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
		if v, ok := d.Get(ParsePath("c")); !ok || v.Int != 2 {
			t.Errorf("%s: reads back as %+v", name, v)
		}
	}
}

// A second key added to a headerless file goes after the first, not above it.
func TestINICreateTwiceInAHeaderlessFile(t *testing.T) {
	d := ini(t, nectarConf)
	if err := d.Create(ParsePath("fog"), Int(1)); err != nil {
		t.Fatal(err)
	}
	if err := d.Create(ParsePath("antialiasing"), Int(0)); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); !strings.HasSuffix(got, "fog = 1\nantialiasing = 0\n") {
		t.Errorf("got:\n%s", got)
	}
	if _, err := Parse(d.Bytes(), FormatINI); err != nil {
		t.Errorf("the result no longer parses: %v", err)
	}
}
