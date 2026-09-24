package schema

import (
	"os"
	"path/filepath"
	"testing"
)

// A directory of schemas is read in the order a page should show them, and a
// program with no editable config has none rather than an error.
func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	field := `"sections":[{"title":"S","fields":[{"pointer":"a.b","kind":"int","widget":"number","label":"L"}]}]`
	write("sound.json", `{"title":"Sound","order":3,"path":"install/sound.json","format":"godot",`+field+`}`)
	write("graphics.json", `{"title":"Graphics","order":1,"path":"install/graphics.json","format":"godot",`+field+`}`)
	write("controls.json", `{"title":"Controls","order":1,"path":"install/controls.json","format":"godot",`+field+`}`)
	write("notes.txt", "ignored")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	files, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, f.Title)
	}
	// Order first, then title within an order.
	if len(got) != 3 || got[0] != "Controls" || got[1] != "Graphics" || got[2] != "Sound" {
		t.Errorf("order = %v, want [Controls Graphics Sound]", got)
	}

	if files, err := LoadDir(filepath.Join(dir, "nothing-here")); err != nil || files != nil {
		t.Errorf("a missing directory should be no schemas and no error, got %v, %v", files, err)
	}

	write("broken.json", `{"title":`)
	if _, err := LoadDir(dir); err == nil {
		t.Error("a malformed schema should be an error, not silence")
	}
}

// Two schemas describing one file is the fault only a whole-directory check sees.
func TestValidateAllCatchesTwoSchemasForOneFile(t *testing.T) {
	mk := func(title, path string) File {
		return File{Title: title, Path: path, Format: "godot", Sections: []Section{{
			Title: "S", Fields: []Field{{Pointer: "a.b", Kind: KindInt, Widget: WidgetNumber, Label: "L"}},
		}}}
	}
	if errs := ValidateAll([]File{mk("A", "install/a.cfg"), mk("B", "install/b.cfg")}); len(errs) > 0 {
		t.Errorf("distinct files should validate: %v", errs)
	}
	errs := ValidateAll([]File{mk("A", "install/a.cfg"), mk("B", "install/a.cfg")})
	if len(errs) != 1 {
		t.Fatalf("want one error, got %v", errs)
	}
}

func TestAFileNeedsATitle(t *testing.T) {
	f := File{Path: "install/a.cfg", Format: "godot", Sections: []Section{{
		Title: "S", Fields: []Field{{Pointer: "a.b", Kind: KindInt, Widget: WidgetNumber, Label: "L"}},
	}}}
	if errs := Validate(f); len(errs) != 1 {
		t.Errorf("a file without a title should be reported once, got %v", errs)
	}
}
