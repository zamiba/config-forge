# Changelog

## v0.0.2 - 2026-09-24

### Added

- **`Create` and `CanCreate` on `Doc`: a setting the program has not written
  yet.** A config file holds only what its program has bothered to write, which
  for a program whose defaults grew between releases is a fraction of what it
  reads. Until now such a setting could be shown but never edited, because `Set`
  refuses a path the file does not hold.

  `Create` adds it, under one rule: **a leaf into a container that already
  exists, never the container itself.** The distinction is not fastidiousness. A
  program reads its config into a fixed set of sections, and a section it does not
  recognise can break that read outright — Super Mario Bros. Remastered indexes
  `file[section][key]`, so an unknown section is an error against a dictionary with
  no such key — while an unknown *key* inside a section it does recognise is loaded,
  written back and otherwise ignored. So the key is addable and the section is not.
  `CanCreate` answers the same question in advance, so a host can present a setting
  as editable only where writing it would actually work, rather than finding out on
  save. Two new errors, `ErrAlreadyPresent` and `ErrNoContainer`, say which case a
  refusal is.

  Godot inserts the key at the end of its own section rather than the end of the
  file, so a section's keys stay together. A Lua table takes it as a line above the
  closing brace at the surrounding indentation, or inline when the table was written
  inline, because reformatting a table the program wrote is not this package's
  business. A key that is not a bare identifier is bracketed and quoted, as the
  program's own writer does.

- **`Field.Default` and `FieldState.Unset`.** A default is **the program's own**
  value for a setting — the one it falls back to when the key is absent — not a
  value this package or its host invents. Given one, `Resolve` reports a setting
  the file lacks as editable anyway, with `Unset` marking that the value shown is
  what the program would use rather than something it recorded, and `WriteField`
  adds the key when it is saved. Without one the setting stays unavailable exactly
  as before: a schema with no default does not claim to know what the program does,
  so nothing is guessed. `WriteField` will not create a path for a field that has
  no default either, so the write path cannot be used to put arbitrary keys into
  someone else's file.

  Recording a default changes nothing about how the program behaves — it is the
  value already in force — which is what makes writing one safe rather than
  presumptuous. What it buys is a file that holds every setting, legible to anyone
  who opens it.

- **`Validate` holds a default to the field's own rules.** The options a select
  offers, the two values a toggle writes, a number's bounds: the checks
  `WriteField` applies to a person's edit now apply to a declared default too,
  because a default those rules reject would be offered on the page and refused on
  save. A default of the wrong kind for the file is reported the same way.

### Breaking

- **`Doc` gained two methods**, `Create` and `CanCreate`. Every implementation in
  this module has them; an implementation outside it does not compile until it
  does. Nothing else changed shape, and a caller that only reads and sets is
  unaffected.

## v0.0.1 - 2026-09-23

First release.

### Added

- **`configfile`: read and write individual settings inside a program's own
  configuration file, without taking ownership of the file.** A `Doc` is the
  file's bytes plus the offsets of the values within them. `Set` splices one
  literal; `Bytes` returns the original buffer with only those splices applied.
  Comments, key order, blank lines, odd spacing and every entry nobody wrote to
  come back byte for byte — including, by construction, entries this package does
  not understand.

  The alternative shape, a template of the whole file with variables in it, is
  rejected on purpose and the README says why: the program is also an author, it
  writes far more than any editor will model, and a file written from a template
  loses every key the template does not mention.

- **Two grammars.** `godot` is Godot's `ConfigFile`: `[section]` headers over
  `key=value` lines of Godot variant literals. `luaTable` is a Lua data file,
  `return { key = value, … }`, in the restricted grammar a deterministic writer
  emits — keyed tables, numbers, booleans and quoted strings, and nothing else. It
  is not general Lua and must not be: a function call, a concatenation, an
  arithmetic expression or a bare sequence entry is refused, because this package
  cannot say where such a value begins and ends and will not guess.

  **The format is declared, never inferred from the extension.** An extension says
  nothing useful: `settings.cfg` is Godot's `ConfigFile` in one program and an
  unrelated grammar in the next.

- **Failing closed, everywhere.** A value this package can locate but not safely
  rewrite — an array, a dictionary, a `Vector2(…)` — is readable as `KindOpaque`
  with its literal, and refused by `Set`. A string carrying an escape that would
  not be reproduced byte for byte is the same. `Set` on a path the file lacks is
  `ErrNoSuchPath`, and on a kind the file does not hold there is
  `ErrKindMismatch`, so a program's `[1024, 960]` never becomes a string and its
  `1` never becomes `true`.

- **`Empty()`**, the document for a file that is not there. Parsing empty bytes is
  a different question and a grammar with a required preamble is right to refuse
  it, but a host still needs to resolve a schema against "the program has not
  written this file yet" to describe the settings it would offer once it has.

- **`Path` is a slice**, with `ParsePath` as the convenience for the common dotted
  case. A key may itself contain a dot — `modOptions["some.mod"].difficulty` is
  three elements, not four — and splitting on dots would address the wrong thing.

- **`schema`: which settings are worth showing, and how.** `File`, `Section` and
  `Field`, with eight widgets — `toggle`, `checkbox`, `select`, `radio`, `text`,
  `number`, `slider`, `path` — over four kinds: `bool`, `int`, `float`, `string`.
  Nothing here draws anything; a widget is a name the host interprets.

  **`kind` and `widget` are separate questions on purpose.** `kind` is what the
  file holds, `widget` is how a person edits it. A program storing `1` and `0` for
  a setting still deserves a toggle, and must still be written `1` and `0`, which
  is what `on` and `off` are for. Conflating the two is how an editor writes `true`
  into a program that counts.

- **`ReadOnly`**, for a value worth seeing and not safely rewritable. Such a field
  may declare `kind: "opaque"`, which no editable field may, and everything that
  configures a control on it — a widget, options, bounds, a unit, `on`/`off` — is
  reported, since an author who set one expected the field to be editable.

- **`Unit`** on a number or a slider, because a bare "70" or "5" says nothing.

- **`Validate` reports every fault at once** rather than the first, so an author
  fixing a catalog entry sees the whole list: a slider without bounds, an option of
  the wrong kind, two fields editing one value, text on a number.
  `ValidateAgainstVersions` additionally checks that `sinceVersion` and
  `untilVersion` name releases that exist, which depends on the spec a schema is
  paired with and so cannot be checked alone.

- **`Resolve`** reads a schema against an open file and returns what to draw: the
  current value, whether the setting is present, whether it is editable, and if
  not, why — as a `Reason` code rather than a sentence, so the words shown to a
  person belong to whoever is drawing the page. A schema that has fallen behind its
  program reports *the file holds bool here, but the schema expects int* instead of
  corrupting the setting.

- **`WriteField`** applies an edit through the field's own rules — a select's
  options, a slider's bounds, a toggle's two values — so a host cannot skip them by
  reaching for the `Doc` directly.

- **`LoadDir` and `ValidateAll`** read a directory of schemas in the order a page
  should show them, by `Order` then `Title`, and report the fault only the whole
  set reveals: two schemas describing one config file, each valid alone.
