package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LoadDir reads every .json file in a directory as a config schema and returns
// them in the order a page should show them: by Order, then by Title. A missing
// directory is not an error — a program with no editable config has no schemas,
// which is the ordinary case, not a fault.
//
// The directory is the host's convention, not this package's: PortForge keeps
// them in a .configs folder beside the item's other metadata. Nothing here knows
// that name.
// SchemaSuffix is what names a schema in a config folder. Reading only these rather
// than every .json gives the folder a contract instead of a convention: everything
// else in it — a reference copy of a program's config, a capture from a real run,
// anything somebody keeps beside them — is ignored rather than parsed as a schema and
// failing. It also settles a real ambiguity, since a reference copy of a config file
// that is itself JSON would otherwise sit beside a schema looking exactly like one.
const SchemaSuffix = ".schema.json"

func isSchemaName(name string) bool {
	return len(name) > len(SchemaSuffix) && strings.EqualFold(name[len(name)-len(SchemaSuffix):], SchemaSuffix)
}

func LoadDir(dir string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !isSchemaName(e.Name()) {
			continue
		}
		names = append(names, e.Name())
	}
	// Read in a fixed order so a failure reports the same file every time.
	sort.Strings(names)

	out := make([]File, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		var f File
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Join(dir, name), err)
		}
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].Title < out[j].Title
	})
	return out, nil
}

// ValidateAll validates each file and the set as a whole. Two schemas naming one
// config file is the fault only visible here: each is valid alone, and together
// they would edit the same bytes from two pages.
func ValidateAll(files []File) []error {
	var errs []error
	seen := map[string]string{}
	for _, f := range files {
		errs = append(errs, Validate(f)...)
		if prev, dup := seen[f.Path]; dup {
			errs = append(errs, fmt.Errorf("%s is described by two schemas (%q and %q)", f.Path, prev, f.Title))
		}
		seen[f.Path] = f.Title
	}
	return errs
}
