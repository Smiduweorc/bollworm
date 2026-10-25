# Continuous integration

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

For linting the spec first, or turning the manual into a signed PDF, see
[creative workflows](./creative-workflows.md).
