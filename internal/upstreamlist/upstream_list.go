package upstreamlist

// DefaultContents is the exact initial contents disclosed and written by
// Gateway-owned Upstream List Creation.
const DefaultContents = "# One upstream host or origin per line.\n# api.dev.example.com\n"

type HostnameMatch uint8

const (
	HostnameExact HostnameMatch = iota
	HostnameSingleLevel
	HostnameRecursive
)

// HostSelector selects an HTTP or HTTPS hostname on any port.
// Hostname never includes wildcard syntax.
type HostSelector struct {
	Hostname      string
	HostnameMatch HostnameMatch
}

// OriginSelector selects one HTTP(S) origin. An empty Port means the source
// omitted the port; a non-empty Port is a normalized explicit port.
type OriginSelector struct {
	Scheme   string
	Hostname string
	Port     string
}

// Warning describes one ignored invalid Upstream List line.
type Warning struct {
	Line       int
	Text       string
	Diagnostic string
}

// Projection is the decoded, normalized Upstream List. Its zero value is the
// canonical Empty Upstream List.
type Projection struct {
	HostSelectors   []HostSelector
	OriginSelectors []OriginSelector
	Warnings        []Warning
}

// Project forms an Upstream List Projection from complete observed contents.
// A non-nil error means the returned Projection is unusable.
func Project(contents []byte) (Projection, error) {
	parsed, err := decode(contents)
	if err != nil {
		return Projection{}, err
	}
	return deduplicate(parsed), nil
}

// Equal reports Upstream List Projection identity. Selector order is not
// significant; warning order and contents are significant.
func Equal(left, right Projection) bool {
	if !sameHostSelectors(left.HostSelectors, right.HostSelectors) ||
		!sameOriginSelectors(left.OriginSelectors, right.OriginSelectors) ||
		len(left.Warnings) != len(right.Warnings) {
		return false
	}
	for i := range left.Warnings {
		if left.Warnings[i] != right.Warnings[i] {
			return false
		}
	}
	return true
}

func deduplicate(parsed parsedUpstreamList) Projection {
	var hosts []HostSelector
	seenHosts := make(map[HostSelector]struct{}, len(parsed.HostSelectors))
	for _, selector := range parsed.HostSelectors {
		if _, ok := seenHosts[selector]; !ok {
			seenHosts[selector] = struct{}{}
			hosts = append(hosts, selector)
		}
	}
	var origins []OriginSelector
	seenOrigins := make(map[OriginSelector]struct{}, len(parsed.OriginSelectors))
	for _, selector := range parsed.OriginSelectors {
		if _, ok := seenOrigins[selector]; !ok {
			seenOrigins[selector] = struct{}{}
			origins = append(origins, selector)
		}
	}
	return Projection{HostSelectors: hosts, OriginSelectors: origins, Warnings: parsed.Warnings}
}

func sameHostSelectors(left, right []HostSelector) bool {
	if len(left) != len(right) {
		return false
	}
	set := make(map[HostSelector]struct{}, len(left))
	for _, v := range left {
		set[v] = struct{}{}
	}
	for _, v := range right {
		if _, ok := set[v]; !ok {
			return false
		}
	}
	return true
}

func sameOriginSelectors(left, right []OriginSelector) bool {
	if len(left) != len(right) {
		return false
	}
	set := make(map[OriginSelector]struct{}, len(left))
	for _, v := range left {
		set[v] = struct{}{}
	}
	for _, v := range right {
		if _, ok := set[v]; !ok {
			return false
		}
	}
	return true
}

// HTTPSIntent reports whether the projection contains at least one HTTPS Origin
// Selector.
func (u Projection) HTTPSIntent() bool {
	for _, selector := range u.OriginSelectors {
		if selector.Scheme == "https" {
			return true
		}
	}
	return false
}
