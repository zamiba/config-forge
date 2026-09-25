# config-forge

Reads and writes individual settings inside a program's own configuration file,
without taking ownership of the file.

It is a Go library in two packages, and it does not depend on
[forge](https://github.com/zamiba/forge) — the name is familial, not structural.
Editing a config has no role in a build, so a config schema does **not** belong in
a `.forge.json`; the host decides where schemas live and hands this package a path.

```bash
go get github.com/zamiba/config-forge
```

---

## Why not template the file

The obvious approach is to keep a copy of the config with placeholders in it, fill
them in, and write the result. It fails for one reason: **the program is also an
author.** A config holds far more than any editor will model — window geometry, the
last controller used, keys a newer release added, keys the program writes at
runtime — and a file written from a template loses every one of them. Reading is no
better: recovering current values means matching the template backwards, which
stops working the moment the program rewrites the file in its own order.

So this package never writes a file. It writes *values*, in place.

```go
doc, err := configfile.Open("install/config/settings.cfg", configfile.FormatGodot)
v, ok := doc.Get(configfile.ParsePath("video.mode"))     // 0, present
err = doc.Set(configfile.ParsePath("video.mode"), configfile.Int(3))
out := doc.Bytes()                                        // only those bytes moved
```

A `Doc` is the file's bytes plus the offsets of the values inside them. `Set`
splices one literal; `Bytes` returns the original buffer with those splices
applied. Comments, key order, blank lines, odd spacing, trailing notes and every
entry no one wrote to come back byte for byte — including, by construction,
entries this package does not understand.

**The format is declared, never inferred from the extension.** An extension says
nothing useful: `settings.cfg` is Godot's `ConfigFile` in one program and an
unrelated grammar in the next.

## Failing closed

Every uncertainty resolves to *refuse and report*, never to *write and hope*:

| Situation | What happens |
|---|---|
| Value is an array, a dictionary, a `Vector2(…)` | Readable as `KindOpaque` with its literal; `Set` refuses |
| A string carries an escape this package would not reproduce exactly | Same — readable, not rewritable |
| `Set` on a path the file does not contain | `ErrNoSuchPath`. `Create` adds a leaf into an existing container; it never adds the container |
| `Set` with a kind the file does not hold there | `ErrKindMismatch` — a program's `[1024, 960]` never becomes a string, and its JSON `1.0` never becomes `1` |
| Schema names a setting the file has lost, or whose type changed | `Resolve` marks it uneditable and says why |

That last row is the one that matters in practice. A schema written against one
release, read against a later one, reports *"the file holds bool here, but the
schema expects int"* instead of corrupting the setting.

## Formats

| `Format` | Grammar |
|---|---|
| `godot` | Godot `ConfigFile`: `[section]` headers over `key=value` lines of Godot variant literals |
| `ini` | A plain key/value config: `key = value` lines under optional `[section]` headers, `#` or `;` comments, untyped unquoted values |
| `json` | Strict JSON with an object at its root: no comments, no trailing commas, no unquoted keys |
| `luaTable` | A Lua data file — `return { key = value, … }` — in the restricted grammar a deterministic writer emits |
| `spaceSeparated` | A key and a value with whitespace between them, one per line, no sections; a value of several tokens is opaque |
| `toml` | TOML in the subset a settings file uses: `key = value` under optional `[table]` headers; arrays, inline tables, multi-line strings and dates are opaque, and `[[arrays of tables]]` are refused |

One format per grammar, added when a program needs it, and only where a value's
position in the file can be known exactly.

`luaTable` is **not general Lua, and must not be.** The grammar is keyed tables,
numbers, booleans and quoted strings — nothing else. A file containing a function
call, a concatenation, an arithmetic expression or a bare sequence entry is
refused, because this package cannot say where such a value begins and ends and
will not guess. That is the same line the programs themselves draw: Gen1Recomp's
own reader replaces `load()` for exactly this reason, so a tampered save fails to
parse instead of executing.

A nested table is addressable twice: as an opaque value at its own path, and
through its children. So `touchControls` reads as a literal and
`touchControls.enabled` is an editable bool. A JSON object works the same way.

A JSON **array is opaque and its elements have no paths**. Addressing into one by
index would let a schema written against one release quietly rewrite the wrong slot
in the next, so a host shows the value and leaves it to the program. `null` is
opaque too: the program chose to record the absence of a value, and which kind
belongs there instead is not this package's guess.

`toml` and `spaceSeparated` both measure what they will not rewrite as carefully as
what they will, because both can hold a value that runs past the end of its own
line — a multi-line array, a triple-quoted string — and a span that stopped at the
newline would let a later edit land inside one. In `spaceSeparated` a value of
several tokens is opaque for the same reason: a controller binding is three numbers
on one line, and reading only the first would report it as a number and let
something overwrite the rest.

`ini` is the odd one out in that its values are not literals of any language, just
text. The kind is read from that text, so a string whose text happens to read as a
number or as `true` is indistinguishable from one and comes back as that kind.
Rewriting a bool keeps the capitalisation the file already used, because a program
that writes `True` may only read `True`. An inline `#` on a value line is not
treated as a comment, since nothing says it is not part of the value.

## Schemas

`schema` says which settings are worth showing and what sort of control belongs on
each. It draws nothing: a widget is a name the host interprets.

```json
{
  "path": "install/config/settings.cfg",
  "format": "godot",
  "sections": [{
    "title": "Video",
    "fields": [
      { "pointer": "video.mode", "kind": "int", "widget": "select", "label": "Window mode",
        "options": [{ "label": "Windowed", "value": 0 }, { "label": "Fullscreen", "value": 3 }] },
      { "pointer": "video.vsync", "kind": "int", "widget": "toggle", "label": "V-sync",
        "on": 1, "off": 0 }
    ]
  }]
}
```

**`kind` and `widget` are separate questions on purpose.** `kind` is what the file
holds; `widget` is how a person edits it. A program storing `1` and `0` for a
setting still deserves a toggle — and must still be written `1` and `0`, which is
what `on` and `off` are for. Conflating the two is how an editor writes `true` into
a program that counts.

Widgets: `toggle`, `checkbox`, `select`, `radio`, `text`, `number`, `slider`,
`path`. Kinds: `bool`, `int`, `float`, `number`, `string`.

`number` is for a program that does not distinguish `1` from `1.0`, written to a
grammar that does not either. C#'s `System.Text.Json` writes a float of `1.0f` as
`1`, and C++'s `operator<<` does the same, so such a setting is a whole number at
its own default and a decimal the moment it moves: `float` would leave it
unavailable at the default and `int` would refuse every value between. Whether the
substitution is safe is a fact about the program rather than the grammar —
libultraship's `Config::GetFloat` ignores a value that is not a JSON float, so
there the strictness is exactly right — so `Validate` allows `number` only against
`json`, `ini` and `spaceSeparated`, the formats whose numbers are untyped, and a Godot or Lua schema still has to say
which of the two a setting is.

A `pointer` is dotted, which reaches the great majority of settings. Where a key
itself contains a dot — `modOptions["some.mod"].difficulty` — the dotted form
cannot express it and would address the wrong thing, which is why `configfile.Path`
is a slice and `ParsePath` is only the convenience for the common case.

`Validate` reports every fault in a schema at once — a slider without bounds, an
option of the wrong kind, two fields editing one value, text on a number — so an
author fixing an entry sees the whole list. `ValidateAgainstVersions` additionally
checks that `sinceVersion` and `untilVersion` name releases that exist, which
depends on the spec the schema is paired with.

`Resolve` reads a schema against an open file and returns the state of every field:
the current value, whether the setting is present, whether it is editable, and if
not, why.

**A field that is not editable is reported, not recommended.** Nobody can act on a
row that says "this release stores this differently" — it is a fact about the
schema, not a choice — so a host should leave such a field out of what it draws.
They are reported because that is the only signal a schema has drifted away from
its program, and the host is the thing that can log it. The same goes for authoring:
**a schema should not name a setting it cannot edit.** A value that is opaque by
nature, a controller binding, a per-device block — leave it out, or declare it
`readOnly` if it is worth seeing.
`WriteField` applies an edit through the field's own rules — a select's options, a
slider's bounds, a toggle's two values — so a host cannot skip them by reaching for
the `Doc` directly.

## Settings a program has not written yet

A config holds only what its program has bothered to write, which for a program
whose defaults grew between releases is a fraction of what it reads. A field can
declare a `default` — **the program's own** fallback value, not one invented here —
and then:

- `Resolve` reports the setting as editable, with `Unset` marking that the value
  shown is what the program would use rather than something it recorded.
- `WriteField` adds the key when it is saved.

Recording a default changes nothing about how the program behaves, since it is the
value already in force. What it buys is a file that holds every setting.

`Create` adds a **leaf into a container that already exists, and never the container
itself.** A program reads its config into a fixed set of sections, and one it does
not recognise can break that read outright, while an unknown key inside a section it
does recognise is loaded, written back and ignored. `CanCreate` answers the same
question in advance, so a host offers a setting as editable only where writing it
would work. A field with no default is never created: the schema does not claim to
know what the program does, so nothing is guessed.

## Completing a config from a reference copy

The rule above has a limit worth naming: a setting whose *section* the program has
never written cannot be added at all, because adding the section would be inventing
structure. For a program that writes its whole config every time this never comes
up. For one that writes a section only once something in it is touched, it is most
of the file.

A **reference copy** resolves it. It is a complete copy of the config — every setting
at the value the program itself uses — authored from the program's own source and
shipped beside the schema, which names it:

```json
{
  "path": "install/settings.cfg",
  "format": "godot",
  "reference": [
    { "file": "settings.cfg.1.0.2.example", "untilVersion": "1.0.2" },
    { "file": "settings.cfg.1.1 RC4.example", "sinceVersion": "1.1 RC4" }
  ],
  "sections": [ … ]
}
```

A schema is named `<something>.schema.json`, and a directory of them is read with
`LoadDir`, which reads only those. That gives the folder a contract rather than a
convention: a reference copy sits beside the schemas without being mistaken for one,
which matters most where a program's config is itself JSON — `shipofharkinian.json`
alongside `shipofharkinian.schema.json` says plainly which is which.

One object where one copy will do, an array where a program's config changes shape
between releases. It is declared rather than found by a naming convention, so a schema
can say it deliberately has none, one copy can cover a range of releases, and the
relationship is a statement instead of a coincidence.

Given one:

- `ResolveWith` reports a setting whose section is missing as editable and `Unset`,
  showing the value the reference holds.
- `Complete` writes every setting the file does not hold yet, adding the sections it
  needs by **copying them from the reference rather than inventing them**. That is
  the whole of the justification, and it is why `CreateContainer` exists on `Doc` and
  why nothing but this calls it.

What gets written is bounded by the **schema**, not by the reference: a setting the
reference holds that no field names is not written, and neither is one whose
`sinceVersion`/`untilVersion` put it outside the installed release — so a reference
taken from a newer build cannot drag that build's settings into an older one. Where
the reference and the schema disagree about a setting's kind, neither value is
written: one of them is wrong, and writing either would be writing a guess.

A reference copy is also what lets a host start a config file the program has not
created, by writing the reference's own bytes. This package still does not compose
one: `Doc` has no way to produce a file, and the bytes in that case are the
reference's.

**Why a file and not a tree inside the schema.** The copy's authority is that it *is*
a copy — which is what makes adding a section copying rather than inventing, and what
lets somebody `diff` it against a config captured from a real run to see whether it is
faithful or merely plausible. A tree inside a schema cannot be diffed against a real
file, and turning one into a file would need a serialiser per grammar. That is the
line this package has held since its first release, for the reason "Why not template
the file" gives.

## What this package will not do

- **Draw anything.** Widgets are names.
- **Resolve paths.** `File.Path` is spelled as the program's own declaration spells
  it. Turning that into somewhere on disk is the host's job, and the host is the
  one that knows about profiles, storage units and per-platform folders.
- **Author a file.** It adds settings inside a config file the program created; it
  does not create the file. It adds a section or table only by copying one from a
  reference copy of that same config, never by composing one.
- **Decide when to write.** A program that is running owns its config and may
  rewrite it at any moment. Refusing to edit a config while its program runs, and
  re-reading afterwards, is the host's responsibility.

## Credits

- Claude by Anthropic for coding assistance
- The entire Open Source ecosystem and the community behind it