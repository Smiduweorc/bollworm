# Creative ways to use bollworm, based on what I hear from people in corporate

Bollworm reads a file or stdin, writes a file or stdout, and only goes to the
network when you pass `--allow-remote-refs`, so it fits between other tools in
a pipeline. These are workflows that come up when someone has to hand an API
manual to a partner, an auditor, a customer or a regulator. The other tools'
flags may have changed since this was written; check them against the
versions you install.

## 1. Sanitized manual for an external partner

Your master spec has admin routes, experimental features and employee-only
endpoints, and the partner should see only the public ones. Bollworm renders
everything it is given, so redact first and render the result:

```sh
# Redocly's filter-out decorator (configured in redocly.yaml) plus pruning
redocly bundle master-spec.yaml --remove-unused-components -o public-spec.yaml
bollworm render public-spec.yaml -o partner-manual.docx
```

`openapi-filter` does the same job.

- Filtering paths is not enough. Schemas used only by internal endpoints stay
  in `components` and appear in the data models chapter unless you prune them.
- Descriptions, examples and tag text are copied as written. Read the output
  once for internal system names before sending it.

## 2. Lint and test before the manual is built

Some teams will not hand out documentation for an API that breaks their own
style guide. Bollworm is not a linter, so put
[Spectral](https://github.com/stoplightio/spectral) in front of it.
[Schemathesis](https://github.com/schemathesis/schemathesis) can test a
running environment against the spec as a second step, if you have a deployed
service.

```yaml
jobs:
  docs:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Lint
        run: npx @stoplight/spectral-cli lint openapi.yaml --fail-severity=error
      - name: Install bollworm
        run: curl -sSfL https://raw.githubusercontent.com/Smiduweorc/bollworm/master/install.sh | sh
      - name: Render
        run: bollworm render openapi.yaml -o api-reference.docx --force
      - uses: actions/upload-artifact@v4
        with:
          name: api-reference
          path: api-reference.docx
```

Attach the `.docx` to a release instead if the manual should be versioned
with the tag.

## 3. A gRPC manual and a REST manual, shipped together

A backend with `.proto` files for internal services and an OpenAPI spec for
the public gateway needs two runs, one spec each:

```sh
bollworm render ./proto/v1/billing.proto -I ./proto -o internal-grpc.docx
bollworm render gateway-spec.json -o public-rest.docx
```

With a running gRPC server and no `.proto` files, `grpcurl` can write a
descriptor set from reflection:

```sh
grpcurl -plaintext -protoset-out api.binpb localhost:50051 describe
bollworm render api.binpb -o api.docx
```

To combine the two into one file, use Word (Insert, Object, Text from File),
where the styles survive and you can fix the contents page. `pandoc a.docx
b.docx` rebuilds the document from its own model, which flattens bollworm's
Word styles and leaves two title pages and two tables of contents.

## 4. A signed PDF for regulated environments

In finance and healthcare a manual often has to leave the building as a
flat, checksummed, signed file. Bollworm leaves PDF to Word or LibreOffice:

```sh
work=$(mktemp -d)
bollworm render internal-spec.yaml -o "$work/draft.docx"
libreoffice --headless --convert-to pdf "$work/draft.docx" --outdir ./distribution
sha256sum distribution/draft.pdf > distribution/draft.pdf.sha256
rm -rf "$work"
```

- LibreOffice does not update fields on open, so check that the PDF's
  contents page has page numbers. If not, open the `.docx` in Word, let it
  update the fields, and export from there.
- OpenSSL can make a detached signature or a timestamp request. A signature
  embedded in the PDF that Acrobat recognises needs a PDF signer such as
  pyHanko.
- Deleting a file does not securely erase it. If the intermediate `.docx` is
  sensitive, render in a tmpfs or on an ephemeral CI runner.
