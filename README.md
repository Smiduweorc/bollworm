# Bollworm
![a picture of our handrawn logo](./assets/logo.png)

Bollworm is a CI tool that turns your openAPI schema into a simple docx. This project is called bollworm because docx files are just a zipped xaml file, which is an archive of docments and hides abstractions, kinda like how a bollworm will bore holes inside a fruit and will do its work inside.

**Bollworm is for people who have an OpenAPI spec and have been asked for "the API document, in Word".**

It reads an OpenAPI 3.0, 3.1 or 3.2 spec, in YAML or JSON, and writes a
`.docx` that reads like a person sat down in Word and wrote the reference
manual: a title page, a table of contents, an introduction, an authentication
chapter, one chapter per tag with a section per endpoint, and a chapter of
data models. Headings, tables and lists use Word's own built-in styles, so the
navigation pane, the contents and the style gallery behave the way they do in
a document someone typed. It is one static binary. The `.docx` is written with
Go's standard library; the only dependencies are a YAML parser, a CommonMark
parser and the command-line library, all MIT, BSD or Apache licensed.

## Bollworm is not for you if you

- **want a documentation website.** Redoc, Scalar and Swagger UI do that, and
  they do it live. Bollworm writes a file you can attach to an email.
- **have a Swagger 2.0 document.** It is refused with a message; convert it
  with `swagger2openapi` first.
- **want to restyle the output from the command line.** There is no theme,
  font or colour flag. Open the file in Word and change the styles; that is
  what styles are for.
- **need PDF.** Word, LibreOffice and Google Docs all export the `.docx` to PDF.

## Install

### Arch Linux (AUR)

```sh
yay -S bollworm      # or: paru -S bollworm
```

Or by hand:

```sh
git clone https://aur.archlinux.org/bollworm.git
cd bollworm
makepkg -si
```

The package installs `/usr/bin/bollworm` and completions for bash, zsh and
fish. The [`PKGBUILD`](./PKGBUILD) in this repository is the one that is
published, and `makepkg -si` in the repository root builds it too.

### Other platforms

```sh
# Go toolchain
go install github.com/Smiduweorc/bollworm/cmd/bollworm@latest

# Released binary (Linux and macOS, amd64 and arm64), checksum verified
curl -sSfL https://raw.githubusercontent.com/Smiduweorc/bollworm/master/install.sh | sh
```

Windows binaries, `.deb` and `.rpm` packages are attached to each
[GitHub release](https://github.com/Smiduweorc/bollworm/releases).

## Quick start

A worked example against a real API: the Arch User Repository's `/rpc`
endpoints, whose spec is public.

1. Fetch the spec:

   ```sh
   curl -sSfL https://aur.archlinux.org/rpc/openapi.json -o aur.json
   ```

2. Write the manual:

   ```sh
   bollworm render aur.json -o aur.docx
   ```

   Nothing is printed on success.

3. Open `aur.docx` in Word. Word asks whether to update the fields in the
   document; say yes, and the contents get their page numbers.

A spec split across files works the same way: `$ref`s to other files are
followed. A spec whose `$ref`s point at URLs needs one more flag, because
bollworm only goes to the network when told to. `testdata/strava.json` is
Strava's API in OpenAPI 3.0 form, with every model in a file on
developers.strava.com:

```sh
bollworm render testdata/strava.json -o strava.docx --allow-remote-refs
```

The spec can come from stdin and the document can go to stdout:

```sh
curl -sSfL https://aur.archlinux.org/rpc/openapi.json | bollworm render - -o - > aur.docx
```

## Usage

```
bollworm render <spec> -o <file.docx> [--force] [--paper a4|letter] [--allow-remote-refs]
```

| Flag | What it does |
| --- | --- |
| `-o`, `--output` | The file to write, or `-` for stdout. Required. Writing to stdout is refused when stdout is a terminal. |
| `--force` | Replace the output file if it exists. Without it an existing file is never touched. The replacement is written to a temporary file and renamed into place, so a failure leaves the old file as it was. |
| `--paper` | `a4` (the default) or `letter`. |
| `--allow-remote-refs` | Fetch `$ref`s that point at https URLs. Without it they are refused with the line they are on. Files are always read. |

Bollworm will not write over its own input, with or without `--force`.

| Exit code | Meaning |
| --- | --- |
| 0 | The document was written. |
| 1 | The spec could not be read or rendered, or the file could not be written. The reason is on stderr, with the line in the spec when there is one. |
| 2 | The command line was wrong. |

## Continuous integration

A GitHub Actions job that writes the manual from your spec on every push to
`main` and keeps the `.docx` as a downloadable build artifact. Put it in
`.github/workflows/api-docs.yml` and point the `render` line at your own spec
(`openapi.yaml` here):

```yaml
name: API document

on:
  push:
    branches: [main]
  pull_request:
  workflow_dispatch:

permissions:
  contents: read

jobs:
  api-docx:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version: "1.26.9"
      - run: go install github.com/Smiduweorc/bollworm/cmd/bollworm@latest
      - run: bollworm render openapi.yaml -o api.docx
      - uses: actions/upload-artifact@v7
        with:
          name: api-docx
          path: api.docx
          if-no-files-found: error
```

The file is attached to the workflow run under **Artifacts** and kept for the
retention period of the repository. Add `--allow-remote-refs` to the `render`
line if the spec refers to `$ref`s on the network. Exit code 1 fails the job
with the reason on stderr, so a broken spec turns the run red.

## What ends up in the document

| Spec | Document |
| --- | --- |
| `info` | Title page: title, version, summary, contact, licence, terms. |
| `info.description`, `servers` | Introduction chapter, with a table of servers and their variables. |
| `components.securitySchemes`, `security` | Authentication chapter, one section per scheme, OAuth flows and scopes in tables. Each endpoint says what it needs. |
| `tags`, `paths` | One chapter per tag, in the order the tags are declared, then untagged endpoints. Each endpoint gets its method and path, description, parameters, request body, responses, headers and examples. |
| `webhooks` | A Webhooks chapter, laid out like the endpoints. |
| `components.schemas` | Data models chapter, one section per schema with a table of fields. Nested anonymous objects are spelled out as `address.city`; a reference to another model links to its section. |

Descriptions are CommonMark (with GitHub's tables and strikethrough), turned
into Word paragraphs, lists, tables and links.

`$ref`s are followed, recursive and mutually recursive schemas included:

- inside the spec, always;
- to another file, always, relative to the file the `$ref` is in (or to the
  working directory for a spec on stdin);
- to an `https://` URL, only with `--allow-remote-refs`. Plain `http://` is
  never fetched, and a document fetched from the network may only refer to
  other `https://` URLs, never to a file on your machine.

A schema found in another file becomes a data model of its own, named by the
`$ref` (`athlete.json#/DetailedAthlete` is `DetailedAthlete`). Two different
schemas with one name are told apart by file: `Fault (errors.json)`. Examples are
printed as JSON in the order they were written; a non-JSON example (XML, CSV,
plain text) is printed as it is.

## Where it sits

Bollworm reads the same documents that [dung beetle](https://github.com/Smiduweorc/dung-beetle-template)
turns into a typed client: one describes an API for code, the other for
people. Neither does the other's job.

## Known quirks

- **Word asks to update fields on open.** That is how the contents get page
  numbers, and Word always asks before doing it. Until the fields are
  updated, the contents list every heading as a link without a page number.
  LibreOffice does not update on open: use Tools, Update, Update All.
- **Aptos.** The document uses Word 365's default fonts, Aptos and Aptos
  Display. Where they are not installed (older Office, most Linux desktops)
  the viewer substitutes another font.
- **Checked against the schema, not against every viewer.** Every part of the
  output validates against the ECMA-376 schema (`make validate`, also in CI).
  It has not been opened in Google Docs or LibreOffice by us. Another
  converter's numbered lists are reported to break in Google Docs; bollworm
  gives every list its own numbering instance with an explicit start, but
  whether Google Docs keeps that is unverified.
- **Fetching is one document at a time, and bounded.** Strava's spec pulls in
  22 documents and takes about 7 seconds. Each fetch gets 15 seconds and
  16 MiB, a fetch may be redirected 5 times (to https only), and a spec may
  pull in 256 documents at most. These are fixed: they are there to stop a
  broken server, not to be tuned. There are no retries.
- **Not printed:** callbacks, links, `discriminator`, `xml`, `externalDocs`,
  per-property `encoding`, and parameter `style`/`explode`.
- **Relative links** in descriptions cannot be followed from a document, so
  they are printed after the link text: "here (/rpc/olddoc.html)".
- **Long examples are cut at 16 KiB**, and anonymous nested objects are
  spelled out four levels deep, then shown as `object`.
- **Two endpoints with the same summary** get the method added to their
  titles, "Lookup (GET)" and "Lookup (POST)", so the contents tell them apart.

## Development

```sh
make setup      # Go modules, then npm ci: lefthook, commitlint, git-cliff
make preflight  # lint and test
```

| Task | What it does |
| --- | --- |
| `make setup` | Downloads Go modules and installs the git hooks and release tooling. Safe to run twice. |
| `make fmt` | `gofmt -w`, or only `FILES=...`. |
| `make lint` | gofmt check, `go vet`, `go mod tidy -diff`. Changes nothing. |
| `make test` | The suite with the race detector. |
| `make fuzz` | Fuzzes the parser and renderer, 30 seconds by default (`FUZZTIME=5m`). |
| `make validate` | Renders the offline specs in `testdata/` on both paper sizes and validates each Word part against the ECMA-376 schema. Needs `curl` and `xmllint`. |
| `make build` | Static, zero-cgo binary. |
| `make preflight` | `lint` then `test`, what `release` runs first. |
| `make changelog` | Regenerates `CHANGELOG.md` with git-cliff. |
| `make release V=patch` | Cuts a release (below). |
| `make snapshot` | A local GoReleaser build; publishes nothing. |
| `make clean` | Removes build output. |

Commits follow Conventional Commits (`fix(render): ...`), checked by
commitlint in the `commit-msg` hook. `CHANGELOG.md` is generated, never edited.

### Releasing

`make release V=v0.1.0` (or `V=patch|minor|major`) refuses a dirty tree, the
wrong branch and an existing tag, runs `preflight`, regenerates the
changelog, commits `chore(release): prepare for v0.1.0` and writes an
annotated tag. It pushes nothing unless `PUSH=1`. Pushing the tag runs
GoReleaser, which publishes the binaries and packages to the GitHub release.

Then the AUR: bump `pkgver` in the `PKGBUILD`, run `updpkgsums` and
`makepkg --printsrcinfo > .SRCINFO`, commit both here, and copy them to the
AUR clone.

### Layout

| Path | What it is |
| --- | --- |
| `cmd/bollworm/` | The command: flags, exit codes, reading and writing files, fetching referenced documents. |
| `internal/spec/` | Reads an OpenAPI document into a model: versions, `$ref` across documents, merging, ordering. |
| `internal/render/` | Lays the model out as a manual, including CommonMark descriptions and examples. |
| `internal/docx/` | Writes WordprocessingML: paragraphs, tables, lists, the contents field, styles. |
| `testdata/` | The AUR, Swagger Petstore and Strava specs as published, and a fixture for everything they do not cover. |
| `scripts/` | `release.sh` and `validate-ooxml.sh`. |
| `PKGBUILD`, `.SRCINFO` | The AUR package. |
| `.goreleaser.yaml`, `install.sh` | Release binaries and the installer for them. |
| `assets/` | The logo. |

## Licence

MIT. See [LICENSE](./LICENSE).

### Inspiration
[Inspired by this go-openapi-converter.](https://github.com/weisurya/go-openapi-converter) The reason I did use it are that it uses an older version of go, didn't seem to correctly parse some newer schema versions, does not support $ref, and also license (original project uses `unioffice`, which has AGPL and may not be suitable for all users). This is by no means me throwing dirt at them, just me making my own thing.
