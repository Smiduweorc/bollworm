# Development

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

## Dependencies

The `.docx` is written with Go's standard library; the dependencies are a
YAML parser, a CommonMark parser, the command-line library, Google's protobuf
runtime and its `google.api` annotations, and bufbuild's protocompile for
`.proto` files, all MIT, BSD or Apache licensed.

## Releasing

`make release V=v0.1.0` (or `V=patch|minor|major`) refuses a dirty tree, the
wrong branch and an existing tag, runs `preflight`, regenerates the
changelog, commits `chore(release): prepare for v0.1.0` and writes an
annotated tag. It pushes nothing unless `PUSH=1`. Pushing the tag runs
GoReleaser, which publishes the binaries and packages to the GitHub release.

Then the AUR: bump `pkgver` in the `PKGBUILD`, run `updpkgsums` and
`makepkg --printsrcinfo > .SRCINFO`, commit both here, and copy them to the
AUR clone.

## Layout

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
| `docs/` | Longer documentation linked from the README. |
