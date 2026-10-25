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

It is one static binary, made to run in CI as happily as on a laptop.

## Bollworm is not for you if you

- **want a documentation website.** Redoc, Scalar, Swagger UI and the
  OpenRPC playground do that, and they do it live. Bollworm writes a file you
  can attach to an email.
- **have a Swagger 2.0 document.** It is refused with a message; convert it
  with `swagger2openapi` first.
- **want to restyle the output from the command line.** There is no theme,
  font or colour flag. Open the file in Word and change the styles, or pass
  `--reference-doc`; that is what styles are for.
- **need PDF.** Word and LibreOffice both export the `.docx` to PDF.
- **have a running server and no spec.** Bollworm never connects to the API
  it documents: it does not call `rpc.discover` on a JSON-RPC server or use
  gRPC server reflection. Save what the server describes to a file first.
  For gRPC, `grpcurl -plaintext -protoset-out api.binpb localhost:50051
  describe` writes a set bollworm reads, usually without the comments.

## Install

```sh
# Arch Linux (AUR), with completions for bash, zsh and fish
yay -S bollworm      # or: paru -S bollworm

# Go toolchain
go install github.com/Smiduweorc/bollworm/cmd/bollworm@latest

# Released binary (Linux and macOS, amd64 and arm64), checksum verified
curl -sSfL https://raw.githubusercontent.com/Smiduweorc/bollworm/master/install.sh | sh
```

Windows binaries, `.deb` and `.rpm` packages are attached to each
[GitHub release](https://github.com/Smiduweorc/bollworm/releases). The
[`PKGBUILD`](./PKGBUILD) in this repository is the one published to the AUR;
`makepkg -si` in the repository root builds it too.

## Quick start

Two public APIs whose specs need no account: the Arch User Repository's
HTTP API, described in OpenAPI, and Ethereum's JSON-RPC API, described in
OpenRPC.

```sh
curl -sSfL https://aur.archlinux.org/rpc/openapi.json -o aur.json
curl -sSfL https://raw.githubusercontent.com/ethereum/execution-apis/assembled-spec/openrpc.json -o ethereum.json

bollworm render aur.json -o aur.docx --link-base https://aur.archlinux.org/
bollworm render ethereum.json -o ethereum.docx
```

Nothing is printed on success. Open the files in Word; when it asks whether
to update the fields in the document, say yes, and the contents get their
page numbers.

Other inputs:

```sh
# gRPC from a .proto file; -I names the import root, as with protoc
bollworm render testdata/proto/library/v1/library.proto -I testdata/proto -o library.docx

# gRPC from a FileDescriptorSet that buf or protoc has built
buf build -o api.binpb && bollworm render api.binpb -o api.docx

# A spec whose $refs point at https URLs (files are always followed)
bollworm render testdata/strava.json -o strava.docx --allow-remote-refs

# stdin to stdout
curl -sSfL https://aur.archlinux.org/rpc/openapi.json | bollworm render - -o - > aur.docx
```

## Usage

```
bollworm render <spec> -o <file.docx> [--force] [--paper a4|letter] [--allow-remote-refs]
    [--proto-path <dir>]... [--include-imports] [--link-base <url>] [--toc-depth 1|2|3] [--reference-doc <file.docx>] [--strict]
```

The spec is an OpenAPI or OpenRPC document (YAML or JSON), a `.proto` file,
or a binary FileDescriptorSet (`.binpb`, `.pb`, `.desc`, `.protoset`).

| Flag | What it does |
| --- | --- |
| `-o`, `--output` | The file to write, or `-` for stdout. Required. Writing to stdout is refused when stdout is a terminal. |
| `--force` | Replace the output file if it exists. Without it an existing file is never touched. The replacement is written to a temporary file and renamed into place, so a failure leaves the old file as it was. |
| `--paper` | `a4` (the default) or `letter`. |
| `--toc-depth` | How many heading levels the contents list: `1` for chapters, `2` (the default) for each call and model as well, `3` for their parts (params, responses, callbacks) too. |
| `--reference-doc` | Take the styles (fonts, colours, spacing) from a .docx of your own, such as a company template. Bollworm uses Word's built-in style names (`Heading1`, `Title`, `TOC1`, ...) plus its own `Code`, `CodeChar`, `Endpoint`, `Label` and `TableText`; any style the file lacks keeps bollworm's look, with a warning for each. |
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

## Documentation

- [What ends up in the document](./docs/output.md): how each format maps
  onto the manual, calls, data models, `$ref`s and warnings.
- [Continuous integration](./docs/ci.md): a GitHub Actions job that builds
  the manual on every push.
- [Creative workflows](./docs/creative-workflows.md): partner manuals,
  linting first, gRPC plus REST, signed PDFs.
- [Known quirks](./docs/quirks.md): field updates, fonts, links, fetching
  limits and protobuf edge cases.
- [Development](./docs/development.md): make targets, releasing, layout.

## Where it sits

Bollworm reads the same documents that [dung beetle](https://github.com/Smiduweorc/dung-beetle-template)
turns into a typed client: one describes an API for code, the other for
people. Neither does the other's job.

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
