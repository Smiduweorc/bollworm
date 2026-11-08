# Known quirks

## Word and LibreOffice

- Word asks to update fields when it opens the file. That is how the contents
  get page numbers; until then they list every heading as a link without one.
  LibreOffice does not update on open and cannot be asked to: use Tools,
  Update, Update All.
- The document uses Word 365's default fonts, Aptos and Aptos Display. Where
  they are not installed (older Office, most Linux desktops) the viewer
  substitutes another. LibreOffice picks a sans-serif one, since bollworm
  declares Aptos as sans-serif, and the table columns are wide enough that
  "Required" does not wrap in it.
- Every part of the output validates against the ECMA-376 schema
  (`make validate`, also run in CI over OpenAPI, OpenRPC and protobuf
  fixtures). It has been opened in LibreOffice 26.8 (the Flatpak build) and
  converted to PDF with headings, lists, tables, links, code blocks and the
  footer's page numbers intact.
- A `--reference-doc` saved by Word in another language names its styles in
  that language: German Word stores "heading 1" as `berschrift1`. Bollworm
  matches styles by those stored names, so it treats them as missing, warns
  about each, and uses its own.

## Content

- Two calls with the same title get a suffix in their headings so the
  contents never list one line twice: an endpoint its method, "Lookup
  (GET)", a JSON-RPC method its name, "Updates the forkchoice state
  (engine_forkchoiceUpdatedV2)", and an rpc its service, "ListBooks
  (Library)". A summary's closing full stop is dropped from its heading; an
  ellipsis or an abbreviation such as "etc." keeps it.
- Relative links in descriptions are resolved against `--link-base`. Without
  it they are printed after the link text, "here (/rpc/olddoc.html)", with a
  warning each. A `#fragment` link points into a web page, so it is always
  printed that way, without a warning.
- Long examples are cut at 16 KiB. Anonymous nested objects are spelled out
  four levels deep, then shown as `object`.

## OpenAPI and OpenRPC `$ref`s

- A `$dynamicRef` is resolved as written. Each schema is described on its
  own, so a `$dynamicRef` goes to the `$dynamicAnchor` in its own document,
  not to one a schema extending it might supply.
- Remote documents are fetched one at a time. Strava's spec pulls in 22
  documents and takes about 7 seconds. The limits are fixed: 15 seconds and
  16 MiB per fetch, 5 redirects (https only), 256 documents per spec. A
  dropped connection, 429, or a 5xx other than 501 is tried 3 times, waiting
  about 1 second and then 2 (jittered by up to half), or what `Retry-After`
  asks for, up to 10 seconds. A fetch that times out is not retried. A
  document that cannot be fetched is reported once, however many `$ref`s
  point into it.

## Protobuf

- By default only the files no other file in the set imports get chapters,
  such as the file given to bollworm or to `protoc`; their imports supply
  types. `--include-imports` describes the others too, except `google.api`.
  `google.protobuf` files are never described. A set built without its
  imports (`buf build --exclude-imports`) still renders, and a type from a
  missing file is shown by its full name.
- A FileDescriptorSet without source info has no comments. `buf build` keeps
  them; `protoc` needs `--include_source_info`.
- Detached comments (separated from the declaration by a blank line, such as
  a licence header) are left out, as `protoc` leaves them out of the
  descriptor's leading comment.
- Options whose definition is not in the set cannot be read, so they are left
  out with a warning. Compiling from `.proto` files always includes them; a
  FileDescriptorSet built with `--exclude-imports` may not.
