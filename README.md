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
| `Set` with a kind the file does not hold there | `ErrKindMismatch` — a program's `[1024, 960]` never becomes a string |
| Schema names a setting the file has lost, or whose type changed | `Resolve` marks it uneditable and says why |

That last row is the one that matters in practice. A schema written against one
release, read against a later one, reports *"the file holds bool here, but the
schema expects int"* instead of corrupting the setting.

## Formats

| `Format` | Grammar |
|---|---|
| `godot` | Godot `ConfigFile`: `[section]` headers over `key=value` lines of Godot variant literals |
| `luaTable` | A Lua data file — `return { key = value, … }` — in the restricted grammar a deterministic writer emits |

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
`touchControls.enabled` is an editable bool.

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
`path`. Kinds: `bool`, `int`, `float`, `string`.

A `pointer` is dotted, which reaches the great majority of settings. Where a key
itself contains a dot — `modOptions["some.mod"].difficulty` — the dotted form
cannot express it and would address the wrong thing, which is why `configfile.Path`
is a slice and `ParsePath` is only the convenience for the common case.

`Validate` reports every fault in a schema at once — a slider without bounds, an
option of the wrong kind, two fields editing one value, text on a number — so an
author fixing an entry sees the whole list. `ValidateAgainstVersions` additionally
checks that `sinceVersion` and `untilVersion` name releases that exist, which
depends on the spec the schema is paired with.

`Resolve` reads a schema against an open file and returns what to draw: the current
value, whether the setting is present, whether it is editable, and if not, why.
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

## What this package will not do

- **Draw anything.** Widgets are names.
- **Resolve paths.** `File.Path` is spelled as the program's own declaration spells
  it. Turning that into somewhere on disk is the host's job, and the host is the
  one that knows about profiles, storage units and per-platform folders.
- **Author a file.** It adds settings inside a config file the program created; it
  does not create the file, or a section or table within one.
- **Decide when to write.** A program that is running owns its config and may
  rewrite it at any moment. Refusing to edit a config while its program runs, and
  re-reading afterwards, is the host's responsibility.
