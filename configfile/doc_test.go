package configfile

import (
	"errors"
	"testing"
)

// The document for a file that is not there: everything absent, nothing writable,
// and no bytes to write back.
func TestEmptyDoc(t *testing.T) {
	d := Empty()
	if _, ok := d.Get(ParsePath("video.mode")); ok {
		t.Error("an empty document contains nothing")
	}
	if len(d.Paths()) != 0 {
		t.Errorf("Paths() = %v", d.Paths())
	}
	if len(d.Bytes()) != 0 {
		t.Errorf("Bytes() = %q", d.Bytes())
	}
	if err := d.Set(ParsePath("video.mode"), Int(1)); !errors.Is(err, ErrNoSuchPath) {
		t.Errorf("Set = %v, want ErrNoSuchPath", err)
	}
}

// Parsing empty input is a different question, and a grammar with a required
// preamble is right to refuse it.
func TestParsingEmptyInputIsNotAnEmptyDoc(t *testing.T) {
	if _, err := Parse(nil, FormatLuaTable); err == nil {
		t.Error("an empty lua data file has no `return` and should be refused")
	}
	if _, err := Parse(nil, FormatGodot); err != nil {
		t.Errorf("an empty ConfigFile is legitimately empty: %v", err)
	}
}
