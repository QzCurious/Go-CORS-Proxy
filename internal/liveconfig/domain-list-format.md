# Domain List format

The Domain List is a UTF-8, line-oriented text format owned by the Live
Configuration package. Each active line contains one HTTP(S) domain selector.
A selector may constrain the scheme, port, and number of subdomain labels.

```text
# Exact host, independent of scheme and port
api.example.test

# Exact host on either HTTP or HTTPS port 3000
localhost:3000

# Exactly one leading subdomain label
*.qa.example.test

# One or more leading subdomain labels, constrained to HTTPS
https://**.dev.example.test

# Exact HTTP(S) origins
https://localhost
http://localhost:3000
http://[::1]:3000

# Scheme-less bracketed IPv6
[::1]:3000
```

## Lines and comments

- LF and CRLF line endings are accepted.
- Leading and trailing whitespace is ignored.
- Blank lines are ignored.
- `#` begins a comment at the start of a trimmed line or when preceded by a
  space or tab.
- A `#` without preceding whitespace remains part of the URL. An empty
  fragment is accepted; a non-empty fragment makes the line invalid.

## URL parsing

- Parsing follows Go's `net/url` behavior.
- Bare host authorities such as `example.com`, `example.com:8080`, and
  `[::1]:8080` are interpreted as authorities before parsing.
- RFC network-path references such as `//example.com:8080` are accepted.
- The parsed URL must contain a hostname.
- The scheme may be omitted or be `http` or `https`. Any other scheme makes
  the line invalid.
- User information is accepted but is not retained.
- The path must be empty or `/`.
- A non-empty query or fragment makes the line invalid. Empty `?` and `#`
  delimiters are accepted.
- Bracketed IPv6 is accepted with or without a scheme or port.
- No DNS lookup, IDNA conversion, port-range validation, or scheme-specific
  validation beyond the HTTP(S) filter is performed.

## Host matching

- A hostname without a wildcard matches exactly that hostname.
- `*.` before the hostname matches exactly one leading subdomain label.
- `**.` before the hostname matches one or more leading subdomain labels.
- Wildcards are accepted with or without a scheme and port.
- `*` and `**` without a parent hostname are invalid.
- Any other use of `*` is invalid.
- A wildcard does not match its parent hostname. List the parent separately
  when it should also match.
- A wildcard parent may be a single-label hostname such as `localhost`.

## Normalization and routing meaning

Each valid line becomes a Domain List Entry containing only its scheme,
hostname, explicit port, and host match mode.

- Scheme and hostname letters are normalized to lowercase.
- One trailing hostname dot is removed.
- User information and an accepted root path or empty suffix delimiters are
  discarded.
- An explicitly written port is retained exactly, including leading zeroes.
  An omitted port remains empty.
- A scheme-less entry matches both HTTP and HTTPS. Its omitted port matches
  every port; its explicit port matches only that port.
- An HTTP(S) entry with an omitted port matches that scheme's default port.
  Its explicit port matches only that port.
- HTTPS selectors remain valid when HTTPS interception is unavailable, but
  PAC Routing leaves their HTTPS routes inactive until the local CA is
  trusted.

## Decoding

Decoding filters out invalid lines, reports every filtered active line as a
Domain List Warning in source line order, and returns every valid entry. Each
warning reports the first failed parse or filter condition. A document with no
valid entries decodes as an Empty Domain List with any collected warnings.
Invalid UTF-8 makes the entire source unusable.

Each warning contains the source line number, the active entry text after
whitespace and comment removal, and the validation diagnostic.

Entries that normalize to the same semantic value are deduplicated, preserving
the position of the first occurrence. Scheme, explicit port, and host match
mode are part of that semantic value. Source line information is not retained
for valid entries.
