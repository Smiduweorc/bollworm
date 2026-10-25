# Known quirks

## Word and LibreOffice

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
- **A `--reference-doc` saved by Word in another language** names its styles
  in that language: German Word stores "heading 1" as `berschrift1`.
  Bollworm matches styles by those stored names, so it treats such styles as
  missing, warns about each, and uses its own.

## Content

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

## OpenAPI and OpenRPC `$ref`s

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

## Protobuf

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
