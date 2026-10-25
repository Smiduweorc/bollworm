# Bollworm
![a picture of our handrawn logo](./assets/logo.png)

Bollworm is a CI tool that turns your openAPI schema into a simple docx. This project is called bollworm because docx files are just a zipped xml file, which is an archive of docments and hides abstractions, kinda like how a bollworm will bore holes inside a fruit and will do its work inside.

**Bollworm is for people who have an OpenAPI spec and have been asked for "the API document, in Word".**

It reads an OpenAPI 3.0, 3.1 or 3.2 spec or an OpenRPC 1.x spec, in YAML or
JSON, or the gRPC services in a `.proto` file or a FileDescriptorSet, and
writes a `.docx` that reads like a person sat down in Word and wrote the
reference manual: a title page, a table of contents, an introduction, an
authentication chapter, one chapter per tag (or service) with a section per
endpoint (or method, or rpc), and a chapter of data models. Headings, tables and lists use Word's own built-in
styles, so the navigation pane, the contents and the style gallery behave the
way they do in a document someone typed. It is one static binary. The `.docx`
is written with Go's standard library; the dependencies are a YAML parser, a
CommonMark parser, the command-line library, Google's protobuf runtime and its
`google.api` annotations, and bufbuild's protocompile for `.proto` files, all
MIT, BSD or Apache licensed.

## Bollworm is not for you if you

- **want a documentation website.** Redoc, Scalar and Swagger UI do that, and
  they do it live. Bollworm writes a file you can attach to an email.
- **have a Swagger 2.0 document.** It is refused with a message; convert it
  with `swagger2openapi` first.
- **want to restyle the output from the command line.** There is no theme,
  font or colour flag. Open the file in Word and change the styles; that is
  what styles are for.
- **need PDF.** Word, LibreOffice and Google Docs all export the `.docx` to PDF.
- **have a running gRPC server and no `.proto` files.** Bollworm does not use
  server reflection and never connects to a server. `grpcurl -plaintext
  -protoset-out api.binpb localhost:50051 describe` writes what the server's
  reflection describes to a file bollworm reads, usually without the
  comments.

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

A gRPC API works from its `.proto` file, with `--proto-path` (or `-I`) naming
the directory its imports are relative to, the way `protoc` takes it.
`testdata/proto` has a fixture that imports `google/api/annotations.proto`:

```sh
bollworm render testdata/proto/library/v1/library.proto --proto-path testdata/proto -o library.docx
```

Or from a FileDescriptorSet that buf or protoc has already built:

```sh
buf build -o api.binpb
bollworm render api.binpb -o api.docx
```

A JSON-RPC API described with OpenRPC renders like an OpenAPI spec; its
`openrpc` field tells them apart. `testdata/openrpc-petstore.json` is the
OpenRPC project's own expanded petstore example:

```sh
bollworm render testdata/openrpc-petstore.json -o petstore.docx
```

## Usage

```
bollworm render <spec> -o <file.docx> [--force] [--paper a4|letter] [--allow-remote-refs] [--proto-path <dir>]...
```

The spec is an OpenAPI or OpenRPC document (YAML or JSON), a `.proto` file,
or a binary FileDescriptorSet (`.binpb`, `.pb`, `.desc`, `.protoset`). The
extension decides; a file with another extension, or stdin, is recognised by
its content.

| Flag | What it does |
| --- | --- |
| `-o`, `--output` | The file to write, or `-` for stdout. Required. Writing to stdout is refused when stdout is a terminal. |
| `--force` | Replace the output file if it exists. Without it an existing file is never touched. The replacement is written to a temporary file and renamed into place, so a failure leaves the old file as it was. |
| `--paper` | `a4` (the default) or `letter`. |
| `--allow-remote-refs` | Fetch `$ref`s that point at https URLs. Without it they are refused with the line they are on. Files are always read. |
| `-I`, `--proto-path` | A directory to look for `.proto` imports in. Repeatable, searched in order. Without it, imports are looked up in the directory of the file being rendered (the working directory for stdin). `google/protobuf/*.proto` are built in; anything else, `google/api/annotations.proto` included, must be on a proto path. Refused for input that is not a `.proto` file. |

Bollworm will not write over its own input, with or without `--force`.

| Exit code | Meaning |
| --- | --- |
| 0 | The document was written. |
| 1 | The spec could not be read or rendered, or the file could not be written. The reason is on stderr, with the line in the spec when there is one (`file.proto:12:3:` for a `.proto`). |
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

For protobuf:

| Protobuf | Document |
| --- | --- |
| `package` and its comment | Title page (the package name, and its version when it ends in one, such as `v1` or `v2beta1`), and the introduction with a table of the files described. |
| `service` | One chapter per service, in the order they are declared. |
| `rpc` | One section per method: its gRPC path, request and response messages linked to their models, the streaming kind (none, server, client or bidirectional), deprecation, and a table of its HTTP mappings when it has a `google.api.http` option. |
| `message`, `enum` | Data models chapter. Each message has a table of fields with their numbers and types: `repeated`, `map<K, V>`, `oneof` membership and `required` (proto2) are spelled out. Each enum has a table of values and their numbers. Nested types are models of their own, named `Book.Format`. |
| Leading and trailing comments | Descriptions. |

The well-known types (`google.protobuf.Timestamp`, `Duration`, `Any` and the
rest) are shown by name and not expanded. Connect and Twirp services are
protobuf services, so their `.proto` files render the same way; the path in
each section is the gRPC one, which Connect also serves, and which Twirp
serves under `/twirp` by default.

For OpenRPC:

| OpenRPC | Document |
| --- | --- |
| `info`, `servers` | Title page and introduction, as for OpenAPI. |
| `methods` | One chapter per first tag, in the order the tags are first used, then untagged methods. Each method gets its name, description, deprecation, a table of params in order, whether they go by name, by position or either, the result, a table of error codes, and its examples as the `params` a client sends and the `result` it gets back. A method with no `result` is described as a notification. |
| `components.schemas` | Data models chapter, as for OpenAPI. |

`$ref`s to `components` (content descriptors, errors, examples, tags) and to
other files are followed the same way as in OpenAPI.

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
  Two rpcs with one name in different services get the service:
  "ListBooks (Library)".
- **A FileDescriptorSet without source info has no comments.** `buf build`
  keeps them; `protoc` needs `--include_source_info`.
- **Which protobuf files are described.** The ones no other file in the set
  imports, such as the file given to bollworm or to `protoc`. Their imports
  supply types but no chapters, so a service in a file that another file in
  the set imports is left out; render that file on its own. A set built
  without its imports (`buf build --exclude-imports`) still renders, and a
  type from a missing file is shown by its full name.
- **Protobuf comments that are detached** (separated from the declaration by
  a blank line, such as a licence header) are left out, as `protoc` leaves
  them out of the descriptor's leading comment.
- **Not printed from OpenRPC:** a method's own `servers`, `links` and
  `externalDocs`, and an error's `data`. An example param given only as
  an `externalValue` is printed as `null`, and such a result is left out.
- **Protobuf options are not printed** apart from `deprecated` and
  `google.api.http`: `buf.validate` rules, `google.api.field_behavior` and
  custom options do not reach the document.

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
| `internal/spec/` | The model the renderer walks, and the OpenAPI and OpenRPC readers: versions, `$ref` across documents, merging, ordering. |
| `internal/proto/` | Compiles `.proto` files and reads FileDescriptorSets into the same model: services, rpcs, messages, enums, comments, `google.api.http`. |
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

### Inspiration
[Inspired by this go-openapi-converter.](https://github.com/weisurya/go-openapi-converter) The reason I did use it are that it uses an older version of go, didn't seem to correctly parse some newer schema versions, does not support $ref, and also license (original project uses `unioffice`, which has AGPL and may not be suitable for all users). This is by no means me throwing dirt at them, just me making my own thing.
