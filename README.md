# Bollworm
![a picture of our handrawn logo](./assets/logo.png)

**Bollworm is for people who have an API spec and have been asked for "the API document, in Word".**

It reads the three ways an API is usually written down: an OpenAPI 3.0, 3.1
or 3.2 spec for an HTTP API, an OpenRPC 1.x spec for a JSON-RPC API, and the
gRPC services in a `.proto` file or a FileDescriptorSet. From any of them it
writes a `.docx` that reads like a person sat down in Word and wrote the
reference manual: a title page, a table of contents, an introduction, one
chapter per group of calls with a section per call, and a chapter of data
models. Headings, tables and lists use Word's own built-in styles, so the
navigation pane, the contents and the style gallery behave the way they do in
a document someone typed.

It is one static binary, made to run in CI as happily as on a laptop. The
`.docx` is written with Go's standard library; the dependencies are a YAML
parser, a CommonMark parser, the command-line library, Google's protobuf
runtime and its `google.api` annotations, and bufbuild's protocompile for
`.proto` files, all MIT, BSD or Apache licensed.

## Bollworm is not for you if you

- **want a documentation website.** Redoc, Scalar, Swagger UI and the
  OpenRPC playground do that, and they do it live. Bollworm writes a file you
  can attach to an email.
- **have a Swagger 2.0 document.** It is refused with a message; convert it
  with `swagger2openapi` first.
- **want to restyle the output from the command line.** There is no theme,
  font or colour flag. Open the file in Word and change the styles; that is
  what styles are for.
- **need PDF.** Word and LibreOffice both export the `.docx` to PDF.
- **have a running server and no spec.** Bollworm never connects to the API
  it documents: it does not call `rpc.discover` on a JSON-RPC server or use
  gRPC server reflection. Save what the server describes to a file first.
  For gRPC, `grpcurl -plaintext -protoset-out api.binpb localhost:50051
  describe` writes a set bollworm reads, usually without the comments.

## How it works

Every format is read into one model, and one renderer lays that model out.
An HTTP endpoint, a JSON-RPC method and a gRPC rpc are all a call; a tag and
a service are both a chapter; a JSON Schema and a protobuf message are both
a data model. So the three manuals have the same shape, and a feature such as
deprecation, a "See also" link or the warning for something the manual
cannot show works the same way in each.

| In the manual | OpenAPI | OpenRPC | Protobuf |
| --- | --- | --- | --- |
| Title page | `info` | `info` | the package, and its version (`v1`, `v2beta1`) |
| Introduction | `info.description`, `servers` | `info.description`, `servers` | the package comment, and the files described |
| Authentication chapter | `securitySchemes`, `security` | (OpenRPC has none) | (protobuf has none) |
| One chapter per | tag | tag | service |
| One section per | endpoint, and webhook | method | rpc |
| Data models chapter | `components.schemas` | `components.schemas` | messages and enums |
| Descriptions | CommonMark | CommonMark | leading and trailing comments |

Bollworm tells the formats apart by the file's extension, or, for any other
name and for stdin, by its content: the `openapi` or `openrpc` field, a
`syntax` or `edition` line, or the bytes of a descriptor set.

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

Two public APIs whose specs need no account: the Arch User Repository's
HTTP API, described in OpenAPI, and Ethereum's JSON-RPC API, described in
OpenRPC.

1. Fetch the specs:

   ```sh
   curl -sSfL https://aur.archlinux.org/rpc/openapi.json -o aur.json
   curl -sSfL https://raw.githubusercontent.com/ethereum/execution-apis/assembled-spec/openrpc.json -o ethereum.json
   ```

2. Write the manuals:

   ```sh
   bollworm render aur.json -o aur.docx --link-base https://aur.archlinux.org/
   bollworm render ethereum.json -o ethereum.docx
   ```

   Nothing is printed on success. The AUR's description links to
   `/rpc/olddoc.html`; `--link-base` turns that into a link to the AUR, and
   without it bollworm prints the path as text and warns.

3. Open them in Word. Word asks whether to update the fields in the
   document; say yes, and the contents get their page numbers. The Ethereum
   manual has a chapter of 78 methods, each with its params in order, its
   result, and the example calls the spec gives.

A gRPC API works from its `.proto` file, with `--proto-path` (or `-I`)
naming the directory its imports are relative to, the way `protoc` takes it.
`testdata/proto` has a fixture that imports `google/api/annotations.proto`:

```sh
bollworm render testdata/proto/library/v1/library.proto --proto-path testdata/proto -o library.docx
```

Or from a FileDescriptorSet that buf or protoc has already built:

```sh
buf build -o api.binpb
bollworm render api.binpb -o api.docx
```

A spec split across files works the same way in OpenAPI and OpenRPC:
`$ref`s to other files are followed. A spec whose `$ref`s point at URLs
needs one more flag, because bollworm only goes to the network when told to.
`testdata/strava.json` is Strava's API in OpenAPI 3.0 form, with every model
in a file on developers.strava.com:

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
    [--proto-path <dir>]... [--include-imports] [--link-base <url>] [--toc-depth 1|2|3] [--strict]
```

The spec is an OpenAPI or OpenRPC document (YAML or JSON), a `.proto` file,
or a binary FileDescriptorSet (`.binpb`, `.pb`, `.desc`, `.protoset`).

| Flag | What it does |
| --- | --- |
| `-o`, `--output` | The file to write, or `-` for stdout. Required. Writing to stdout is refused when stdout is a terminal. |
| `--force` | Replace the output file if it exists. Without it an existing file is never touched. The replacement is written to a temporary file and renamed into place, so a failure leaves the old file as it was. |
| `--paper` | `a4` (the default) or `letter`. |
| `--toc-depth` | How many heading levels the contents list: `1` for chapters, `2` (the default) for each call and model as well, `3` for their parts (params, responses, callbacks) too. |
| `--strict` | Fail, writing nothing, when there are warnings. Without it, warnings go to stderr and the document is still written. |
| `--link-base` | An absolute http or https URL that relative links in descriptions are resolved against, such as the site the spec's own docs live on. Without it, a relative link is printed after its text, with a warning. The server URL is never guessed as the base: an API's host is often not where its documentation lives. |
| `--allow-remote-refs` | OpenAPI and OpenRPC: fetch `$ref`s that point at https URLs. Without it they are refused with the line they are on. Files are always read. |
| `-I`, `--proto-path` | Protobuf: a directory to look for `.proto` imports in. Repeatable, searched in order. Without it, imports are looked up in the directory of the file being rendered (the working directory for stdin). `google/protobuf/*.proto` are built in; anything else, `google/api/annotations.proto` included, must be on a proto path. Refused for input that is not a `.proto` file. |
| `--include-imports` | Protobuf: also describe the services in files the given file imports, or that other files in a FileDescriptorSet import. Files under `google/protobuf/` and `google/api/` are never described. Without it, only the files nothing else imports get chapters. |

Bollworm will not write over its own input, with or without `--force`.

| Exit code | Meaning |
| --- | --- |
| 0 | The document was written. |
| 1 | The spec could not be read or rendered, the file could not be written, or `--strict` was given and there were warnings. Every problem found is on stderr, one per line, with the line in the spec when there is one (`file.proto:12:3:` for a `.proto`). |
| 2 | The command line was wrong. |

Bollworm warns, on stderr as `bollworm: warning: ...`, when the spec says
something the manual cannot show as written: two operations with one
`operationId`, a link to an operation that does not exist, a discriminator
mapping that resolves to nothing, a parameter with more than one media type,
an `$id` that is not a usable location, a relative link with no
`--link-base` to resolve it, or a protobuf option whose definition is
missing. In CI, pass `--strict` to make those fail the build.

## Continuous integration

A GitHub Actions job that writes the manual from your spec on every push to
`main` and keeps the `.docx` as a downloadable build artifact. Put it in
`.github/workflows/api-docs.yml` and point the `render` line at your own
spec, whichever format it is in (`openrpc.json` here):

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
          go-version: "1.27.3"
      - run: go install github.com/Smiduweorc/bollworm/cmd/bollworm@latest
      - run: bollworm render openrpc.json -o api.docx --strict
      - uses: actions/upload-artifact@v7
        with:
          name: api-docx
          path: api.docx
          if-no-files-found: error
```

The file is attached to the workflow run under **Artifacts** and kept for the
retention period of the repository. Add `--allow-remote-refs` to the `render`
line if the spec refers to `$ref`s on the network, `--link-base` if its
descriptions use relative links, and `--proto-path` for a `.proto` file.
Exit code 1 fails the job with every reason on stderr, so a broken spec turns
the run red; `--strict` does the same for a spec the manual cannot show as
written.

## What ends up in the document

### In every manual

- **The title page** has the title and version, then the summary, contact,
  licence and terms of service when the spec gives them.
- **The introduction** has the description, a table of servers with their
  variables, and a "See also" line for `externalDocs`, each when the spec
  has it.
- **One chapter per group of calls**, in the order the groups are declared
  (or first used), then the calls in no group. A call in several groups is
  described in the chapter of its first one, and the chapters of the others
  list it with a link there.
- **Each call** has its name and description, a deprecation notice when it is
  deprecated, and a "See also" line for its `externalDocs`.
- **The data models chapter** has a section per model. A reference to a model
  anywhere in the manual links to its section.
- **Descriptions** are CommonMark (with GitHub's tables and strikethrough),
  turned into Word paragraphs, lists, tables and links.
- **Examples** are printed as JSON in the order they were written.

### Calls

**An OpenAPI endpoint** gets its method and path, the authentication it
needs, its parameters (with their `style` and `explode` when given), its
request body (with its multipart or form `encoding`), and a table of
responses with their headers and bodies, then each response's examples and
`links`, each link pointing at its target's section. Its `callbacks` follow,
laid out like the endpoint. A non-JSON example (XML, CSV, plain text) is
printed as it is. `webhooks` get a chapter of their own.

**An OpenRPC method** gets the name a client calls, a table of its params in
order with whether they go by name, by position or either, its result, and a
table of its errors with their `data`. A method with no `result` is described
as a notification. A method's own `servers` replace the document's for it.
Each example pairing is printed as the `params` a client sends and the
`result` it gets back, shaped as an object or an array to match the param
structure; an example given only as an `externalValue` is a link to it.

**A gRPC rpc** gets its gRPC path, its request and response messages, the
streaming kind (none, server, client or bidirectional), and a table of its
HTTP mappings when it has a `google.api.http` option. Connect and Twirp
services are protobuf services, so their `.proto` files render the same
way; the path in each section is the gRPC one, which Connect also serves,
and which Twirp serves under `/twirp` by default.

### Data models

**OpenAPI and OpenRPC** models are JSON Schema, and both formats get the same
treatment. Each model has a table of fields. Nested anonymous objects are
spelled out as `address.city`. Keywords beside a `$ref` (`readOnly`,
`example`, `nullable` and the rest) apply on top of the model.
`additionalProperties: false` and `unevaluatedProperties: false` are printed
as "no other properties", the `false` schema as "nothing", and every entry of
`examples` is shown. A `discriminator` is a table of its values and the
models they select, and `xml` names, attributes, wrapping and namespaces are
spelled out.

**Protobuf** messages have a table of fields with their numbers and types:
`repeated`, `map<K, V>`, `oneof` membership and `required` (proto2) are
spelled out. Enums have a table of values and their numbers. Nested types are
models of their own, named `Book.Format`. The well-known types
(`google.protobuf.Timestamp`, `Duration`, `Any` and the rest) are shown by
name and not expanded. `google.api.field_behavior` marks a field required
(`REQUIRED`) or is spelled out in its description (`OPTIONAL`,
`OUTPUT_ONLY`, `INPUT_ONLY`, `IMMUTABLE`, `IDENTIFIER`, `UNORDERED_LIST`,
`NON_EMPTY_DEFAULT`). Custom options, such as `buf.validate` rules, and a
method's `idempotency_level` are listed under what they are set on, in
protobuf text format: `(buf.validate.field): {string: {min_len: 1}}`.

### `$ref`s in OpenAPI and OpenRPC

`$ref`s are followed, recursive and mutually recursive schemas included. In
OpenRPC that covers the methods themselves and everything under
`components`: content descriptors, errors, example pairings, examples and
tags. A `$ref` may use a JSON pointer,
an `$anchor` name, or a schema's `$id`; a `$dynamicRef` is followed like a
`$ref`. They are followed:

- inside the spec, always;
- to another file, always, relative to the file the `$ref` is in (or to the
  working directory for a spec on stdin);
- to an `https://` URL, only with `--allow-remote-refs`. Plain `http://` is
  never fetched, and a document fetched from the network may only refer to
  other `https://` URLs, never to a file on your machine.

A schema found in another file becomes a data model of its own, named by the
`$ref` (`athlete.json#/DetailedAthlete` is `DetailedAthlete`). Two different
schemas with one name are told apart by file: `Fault (errors.json)`.

## Where it sits

Bollworm reads the same documents that [dung beetle](https://github.com/Smiduweorc/dung-beetle-template)
turns into a typed client: one describes an API for code, the other for
people. Neither does the other's job.

## Known quirks

- **Word asks to update fields on open.** That is how the contents get page
  numbers, and Word always asks before doing it. Until the fields are
  updated, the contents list every heading as a link without a page number.
  LibreOffice does not update on open and has no way to be asked to: the
  contents show without page numbers until you use Tools, Update, Update All.
- **Aptos.** The document uses Word 365's default fonts, Aptos and Aptos
  Display. Where they are not installed (older Office, most Linux desktops)
  the viewer substitutes another font. LibreOffice picks a sans-serif one
  (bollworm declares Aptos as sans-serif), and the table columns are wide
  enough that "Required" does not wrap in it.
- **Checked against the schema and LibreOffice.** Every part of the output
  validates against the ECMA-376 schema (`make validate`, also in CI, over
  OpenAPI, OpenRPC and protobuf fixtures). It has been opened in LibreOffice
  26.8 (the Flatpak build) and converted to PDF: headings, lists, tables,
  links, code blocks and the footer's page numbers come out as intended.
- **Two calls with the same title** are told apart in their headings, so the
  contents never list one line twice: an endpoint gets its method, "Lookup
  (GET)", a JSON-RPC method its name, "Updates the forkchoice state
  (engine_forkchoiceUpdatedV2)", and an rpc its service, "ListBooks
  (Library)". A summary's closing full stop is dropped from its heading, as
  headings go without one; an ellipsis or an abbreviation such as "etc."
  keeps it.
- **Relative links** in descriptions are resolved against `--link-base`.
  Without it they cannot be followed from a document, so they are printed
  after the link text, "here (/rpc/olddoc.html)", and each one is warned
  about. A link to `#fragment` points into a web page the manual is not, so
  it is always printed that way, without a warning.
- **Long examples are cut at 16 KiB**, and anonymous nested objects are
  spelled out four levels deep, then shown as `object`.
- **A `$dynamicRef` is resolved as written.** The manual describes each
  schema on its own, so a `$dynamicRef` goes to the `$dynamicAnchor` in its
  own document, not to one a schema extending it might supply.
- **Fetching is one document at a time, and bounded.** Strava's spec pulls in
  22 documents and takes about 7 seconds. Each fetch gets 15 seconds and
  16 MiB, a fetch may be redirected 5 times (to https only), and a spec may
  pull in 256 documents at most. A fetch that fails in a way that may pass
  (a dropped connection, 429, or a 5xx other than 501) is tried 3 times,
  waiting about 1 second and then about 2 (each jittered by up to half
  either way), or what the server's `Retry-After` asks for, in seconds or as
  a date, up to 10 seconds. A fetch that runs out of its 15 seconds is not
  tried again. A document that cannot be fetched is reported once, however
  many `$ref`s point into it. These are fixed: they are there to stop a
  broken server and ride out a blip on a CI runner, not to be tuned.
- **Which protobuf files are described.** By default, the ones no other file
  in the set imports, such as the file given to bollworm or to `protoc`;
  their imports supply types but no chapters. `--include-imports` describes
  every other file too, except those of `google.api`. The files of
  `google.protobuf` are never described. A set built without its imports
  (`buf build --exclude-imports`) still renders, and a type from a missing
  file is shown by its full name.
- **A FileDescriptorSet without source info has no comments.** `buf build`
  keeps them; `protoc` needs `--include_source_info`.
- **Protobuf comments that are detached** (separated from the declaration by
  a blank line, such as a licence header) are left out, as `protoc` leaves
  them out of the descriptor's leading comment.
- **Protobuf options whose definition is not in the set** cannot be read, so
  they are left out with a warning. Compiling from `.proto` files always
  includes them; a FileDescriptorSet built with `--exclude-imports` may not.

## Development

```sh
make setup      # Go modules, then npm ci: lefthook, commitlint, git-cliff
make preflight  # lint and test
```

`make lint` also needs golangci-lint (the release pinned in CI, v2.15.0) and
shellcheck on the `PATH`; on Arch, `pacman -S golangci-lint shellcheck`.

| Task | What it does |
| --- | --- |
| `make setup` | Downloads Go modules and installs the git hooks and release tooling. Safe to run twice. |
| `make fmt` | `gofmt -w`, or only `FILES=...`. |
| `make lint` | gofmt check, `go vet`, golangci-lint (rules in `.golangci.yml`, tests included), `go mod tidy -diff`, and shellcheck plus a syntax check on every shell script. Changes nothing. |
| `make test` | The suite with the race detector. |
| `make fuzz` | Fuzzes the parser and renderer, 30 seconds by default (`FUZZTIME=5m`). |
| `make validate` | Renders the offline OpenAPI and OpenRPC specs and the `.proto` fixtures in `testdata/` on both paper sizes and validates each Word part against the ECMA-376 schema. Needs `curl` and `xmllint`. |
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
| `cmd/bollworm/` | The command: flags, exit codes, telling the input kinds apart, reading and writing files, fetching referenced documents. |
| `internal/spec/` | The model the renderer walks, and the OpenAPI and OpenRPC readers: versions, `$ref` across documents, merging, ordering, warnings. |
| `internal/proto/` | Compiles `.proto` files and reads FileDescriptorSets into the same model: services, rpcs, messages, enums, comments, options. |
| `internal/render/` | Lays the model out as a manual, including CommonMark descriptions and examples. |
| `internal/docx/` | Writes WordprocessingML: paragraphs, tables, lists, the contents field, styles. |
| `testdata/` | The AUR, Swagger Petstore and Strava specs and the OpenRPC petstore example (Apache 2.0) as published, and a fixture for everything they do not cover in each format. |
| `testdata/proto/` | `.proto` fixtures for every streaming kind, oneofs, maps, nested types, well-known types and `google.api.http`, a Twirp-style service, and copies of googleapis' `google/api/annotations.proto` and `http.proto` (Apache 2.0) for them to import. |
| `scripts/` | `release.sh` and `validate-ooxml.sh`. |
| `PKGBUILD`, `.SRCINFO` | The AUR package. |
| `.goreleaser.yaml`, `install.sh` | Release binaries and the installer for them. |
| `assets/` | The logo. |

## Licence

MIT. See [LICENSE](./LICENSE).

## Lore

A `.docx` file is a zip of XML documents, an archive that hides its workings
inside a single file. A bollworm bores into a cotton boll or a fruit and
does all its work on the inside, out of sight. Hence the name.

Bollworm was [inspired by go-openapi-converter](https://github.com/weisurya/go-openapi-converter).
We made our own because that one uses an older version of Go, did not seem
to parse some newer schema versions correctly, does not support `$ref`, and
depends on `unioffice`, whose AGPL licence may not suit everyone. This is by
no means throwing dirt at it, just us making our own thing.
