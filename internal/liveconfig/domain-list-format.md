# Domain List format

The Domain List is a UTF-8, line-oriented text format owned by the Live
Configuration package. Each non-empty line contains one host shorthand or HTTP
origin.

```text
# Exact host, independent of scheme and port
api.example.test

# The wildcard is a single leading label
*.qa.example.test

# Exact origins
https://localhost
http://localhost:3000
http://[::1]:3000
```

## Lines and comments

- LF and CRLF line endings are accepted.
- Leading and trailing whitespace is ignored.
- Blank lines are ignored.
- `#` begins a comment at the start of a trimmed line or when preceded by a
  space or tab. A `#` elsewhere makes the entry invalid.

## Host shorthand

- A host shorthand contains no scheme, port, path, or IPv6 address.
- A trailing dot is ignored and letters are normalized to lowercase.
- A wildcard, when present, is exactly one leading `*.` label and must have a
  concrete parent domain.

Host shorthand is a routing match independent of scheme and port.

## Origin

- An origin uses the `http` or `https` scheme and contains a host with an
  optional port.
- Paths, queries, fragments, user information, and wildcards are invalid.
- IPv6 is accepted only in bracketed origin form.
- An omitted port normalizes to `80` for HTTP and `443` for HTTPS.
- Host letters are normalized to lowercase.

## Decoding

Decoding ignores invalid lines, reports the complete untruncated set as Domain
List Warnings in source line order, and returns every valid entry. A document
with no valid entries decodes as an Empty Domain List with any collected
warnings. Invalid UTF-8 makes the entire source unusable.

Each warning contains the source line number, the active entry text after
whitespace and comment removal, and the validation diagnostic.

Entries that normalize to the same semantic value are deduplicated, preserving
the position of the first occurrence.
