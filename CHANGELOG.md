# Changelog

## v0.0.4 - 2026-09-25

### Added

- **`toml`, in the subset a settings file uses.** `key = value` lines under
  optional `[table]` headers, with `#` comments, dotted and quoted keys, and all
  four of TOML's string forms.

  It is not a general TOML parser and does not try to be. What it guarantees is
  what every grammar here guarantees — that it knows exactly where a value begins
  and ends — and for TOML that means measuring the constructs it does *not*
  interpret as carefully as the ones it does. An array, an inline table, a
  multi-line string and a date are each located, spanned across however many
  lines they occupy, reported as `KindOpaque` and refused by `Set`. Getting their
  extent wrong would be worse than not reading them at all: a line-oriented
  parser that mistook the middle of a multi-line array for a key would write a
  setting into the middle of somebody's list.

  **An array of tables, `[[thing]]`, is refused, and the whole file with it.**
  Every other construct has one place a given path can be; a repeated table does
  not, and a schema pointing into one would be addressing whichever copy came
  first.

  A multi-line string is readable and not rewritable, for the same reason a
  string carrying an unreproducible escape is: writing it back would mean
  rendering it as one escaped line, which is the same value in a layout the
  program did not choose.

- **`spaceSeparated`: a key and a value with whitespace between them.** No
  sections, no equals sign — what a C program writes with `fprintf` and reads
  back with a tokeniser.

  **The value is the rest of the line**, which is deliberate rather than lazy.
  Files in this shape hold entries whose value is several tokens — a controller
  binding is three hex numbers on one line — and a parser that took only the
  first would report a binding as the number 38 and let something overwrite the
  rest. So a value of more than one token is located whole and reported as
  opaque. A quoted run is one value however many spaces are inside it, because
  that is what `std::quoted` reads.

  Neither writer of this format emits a comment, and neither reads one except on
  a line of its own, so a trailing `#` is not treated as one here either: a line
  carrying one simply has a value of several tokens, and is therefore opaque,
  which is the safe reading.

  The two programs that write it do not agree on how to spell things, and that is
  the schema's business rather than the parser's: one writes a bool as `true` and
  a float with six decimal places, the other writes a bool as `1` and a float
  through `operator<<`, which drops the point on a whole number — so its floats
  declare `kind: "number"`.

- **Completing a config from a reference copy of it: `schema.Complete`,
  `schema.ResolveWith` and `Doc.CreateContainer`.**

  A config holds only what its program has written, and `Create` can add a leaf
  into a container that is already there but not the container itself — so a
  setting whose *section* the program has never written could not be edited at all,
  and there was nothing to be done about it from inside this package. A reference
  copy changes what is known: a complete copy of the config, authored from the
  program's own source, with every setting at the value the program itself uses.
  Given one, the section being added is **copied rather than invented**, which is
  the whole of the justification.

  `CreateContainer` is the one thing here that adds structure, and each grammar
  renders its own kind of empty container — a `[section]`, a `[table]`, `{}`.
  `Complete` drives it, bounded by the **schema** rather than by the reference: a
  setting the reference holds that no field names is not written, and neither is one
  whose version bounds put it outside the installed release, so a reference taken
  from a newer build cannot drag that build's settings into an older one. Where the
  two disagree about a setting's kind, neither is written — one of them is wrong and
  writing either would be writing a guess. `ResolveWith` is `Resolve` with the
  reference in hand, so a setting the reference makes addable comes back editable and
  `Unset` instead of unavailable.

  A container is attested by asking the reference two ways, because the grammars
  disagree about what a container is: a JSON object and a Lua table are values in
  their own right, while a Godot, ini or TOML section is not a value at all and shows
  up only as the prefix of the paths inside it.

- **`File.Reference`: a schema names the reference copies it ships.** One object, or
  an array of them, each with a `file` and optional `sinceVersion`/`untilVersion` —
  the same two spellings a spec's `userDataPaths` accepts.

  The alternative was a naming convention, deriving the copy's name from the config
  file's. Declaring it makes the relationship a statement rather than a coincidence:
  a schema can say it deliberately has *no* reference, one copy can cover a range of
  releases instead of needing one file per release, and a program whose config changes
  shape between releases says so where somebody reading the catalog will see it.
  `ReferenceFor` picks the copy that applies to a release, in declaration order, so an
  unbounded one placed last reads as the fallback it is.

  `Validate` refuses a reference with no file, one that is a path rather than a bare
  name beside the schema, one named `.json` — `LoadDir` reads every `.json` in the
  folder as a schema, so such a copy would be parsed as one and fail — and the same
  file named twice. `ValidateAgainstVersions` holds a reference's bounds to the same
  rule a field's: a bound naming no declared release would silently apply to nothing.

  **Folding the copy into the schema as structure was considered and rejected.** A
  reference's authority comes from being a copy of a real file, which is what makes
  adding a section copying rather than inventing — and what lets somebody `diff` it
  against a config captured from an actual run. A tree inside a schema cannot be
  diffed against a real file, and turning one into a file would need a serialiser per
  grammar, which is the thing this package has not had since v0.0.1 and should not
  gain.

- **`Value.Equal`**, which compares two values by what they are rather than by how
  they were spelled.

### Fixed

- **A select no longer fails to recognise the option its file is already set to.**
  `checkFieldValue` compared `configfile.Value` structs directly, and a value parsed
  out of a file carries its literal in `Raw` while one declared in a schema does not
  — so a `mode=2` in the file matched no option, and a toggle set to its own `on`
  value matched neither `on` nor `off`. Both now compare with `Value.Equal`. It went
  unnoticed because every value that previously reached these checks came from a
  schema or from a host decoding JSON, neither of which sets `Raw`; resolving against
  a reference copy is the first thing to compare a file's own value against a
  schema's.

### Changed

- **A schema is named `<something>.schema.json`, and `LoadDir` reads only those.**
  `schema.SchemaSuffix` is the constant.

  Reading every `.json` was a convention; reading only these is a contract. Everything
  else in the folder — a reference copy, a capture from a real run, anything somebody
  keeps beside them — is ignored rather than parsed as a schema and failing. It also
  settles an ambiguity that had already arrived: Ship of Harkinian's folder held
  `shipofharkinian.json` (the schema) next to `shipofharkinian.json.example` (a copy of
  the game's config, which is *also* called `shipofharkinian.json`), and the second read
  as an example of the first. `shipofharkinian.schema.json` says which is which.

  A consequence worth having: a reference copy may now be named plainly, `settings.json`
  included, because the suffix is what marks a schema. `Validate` refuses only a
  reference named `.schema.json`.

  **Breaking:** a schema not named this way is no longer found. It is not an error —
  a folder with no schemas is how a program with no editable config looks — so a host
  that keeps schemas of its own should check that a folder it expects to yield some
  does.

- **`Doc` gained `CreateContainer`.** Every implementation in this module has it; one
  outside it does not compile until it does.

- **`Resolve`'s contract is spelled out: reported is not the same as worth
  drawing.** A field that is not `Editable` is one nobody can act on, and a row
  saying so states a fact about the schema rather than offering a choice, so a host
  is expected to leave it out of what it draws. It is still reported, because that
  is the only signal a schema has drifted away from its program and the host is the
  only thing that can log it. Nothing changed in the code; the doc comment
  previously argued the opposite, and it was wrong.

- **`UntypedNumbers` now includes `spaceSeparated`**, so its numeric settings can
  declare `kind: "number"` and be written in whichever spelling the value carries.
  `godot`, `luaTable` and `toml` still give their numbers a type of their own, and
  `Validate` still refuses the declaration against all three.

## v0.0.3 - 2026-09-24

### Added

- **`json`: strict JSON with an object at its root.** It is what the great
  majority of these programs write, by a distance — twelve of the twenty-two
  config files across the catalog, from four separate writers: nlohmann::json in
  every libultraship port, librecomp's own `Config` in Banjo Recompiled, a hand-
  built `nlohmann::json` in Snap64 Recomp, and C#'s `System.Text.Json` in Crash
  Bandicoot Recompiled.

  No comments, no trailing commas, no unquoted keys: a file carrying any of those
  was not written by the program whose settings are being edited. An object is
  addressable twice over, as an opaque value at its own path and through its
  children, the way a Lua table is. An **array is opaque and its elements have no
  paths of their own** — addressing into one by index would let a schema written
  against one release quietly rewrite the wrong slot in the next — and `null` is
  opaque for the same reason: the program recorded the absence of a value, and
  which kind belongs there instead is not this package's guess to make.

  A created member brings the comma the object now needs and takes its siblings'
  own indentation, whether that is tabs, two spaces or four; an object written on
  one line is added to on that line rather than reformatted.

  **A float keeps its decimal point.** JSON has one number type but the program
  reading the file does not: libultraship's `Config::GetFloat` ignores a value
  that is not a JSON float, so a setting that came back as `2` would silently stop
  being read at all.

- **`ini`: a plain key/value config.** `key = value` lines under optional
  `[section]` headers, with `#` or `;` comments. Crash Bandicoot Recompiled's
  `interface.ini` and Open Nectar's `pikmin_settings.conf` are both this.

  Unlike Godot's `ConfigFile`, which it resembles, the values are not literals of
  any language: they are text, and what one means is decided by whatever parses it
  on the other side. So the kind is read from the text — `true` or `false` in any
  capitalisation is a bool, a whole number an int, a decimal a float, everything
  else an unquoted string. **Rewriting a bool keeps the capitalisation the file
  already used**, because a program that writes `True` may well be one that only
  reads `True`. A value that could not survive the round trip — one carrying a
  newline, one that would be trimmed back, one starting `#`, `;` or `[` — is
  refused rather than written.

  A file may hold things that are not this grammar at all: `interface.ini` ends
  with an ImGui layout blob, appended verbatim after a blank line. An unrecognised
  line is left exactly where it is.

- **`kind: "number"`, and `configfile.NumberSetter`.** A number whose spelling the
  program does not care about, for a grammar that does not care either.

  This is the one thing the catalog sweep turned up that no existing kind could
  express. C#'s `System.Text.Json` writes a float of `1.0f` as `1`, and C++'s
  `operator<<` does the same, so a setting like Crash's `MasterVolume` or Open
  Nectar's `renderScale` sits in the file as a whole number at exactly its own
  default and as a decimal the moment anyone moves it. Declaring `float` left it
  unavailable at its default; declaring `int` refused every value between.

  Whether the substitution is safe is a fact about the **program**, not about the
  grammar — both of the cases above are JSON, and so is libultraship, where the
  refusal is exactly right — which is why it is a second method, `SetNumber`,
  rather than a loosening of `Set`, and why the schema is what chooses between
  them. Only a format whose numbers are untyped implements it, and `Validate`
  refuses `kind: "number"` declared against any other, so a Godot or Lua schema
  still has to say which of the two a setting is.

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
