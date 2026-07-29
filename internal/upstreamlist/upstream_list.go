package upstreamlist

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

// UpstreamList is the decoded, normalized Upstream List.
type UpstreamList struct {
	HostSelectors   []HostSelector
	OriginSelectors []OriginSelector
	Warnings        []Warning
}

// SameEntries reports whether two Upstream Lists contain the same normalized
// selector sets. Source ordering and warnings do not affect entry identity.
func SameEntries(left, right UpstreamList) bool {
	return sameHostSelectors(left.HostSelectors, right.HostSelectors) &&
		sameOriginSelectors(left.OriginSelectors, right.OriginSelectors)
}

func sameHostSelectors(left, right []HostSelector) bool {
	if len(left) != len(right) {
		return false
	}
	selectors := make(map[HostSelector]struct{}, len(left))
	for _, selector := range left {
		selectors[selector] = struct{}{}
	}
	for _, selector := range right {
		if _, ok := selectors[selector]; !ok {
			return false
		}
	}
	return true
}

func sameOriginSelectors(left, right []OriginSelector) bool {
	if len(left) != len(right) {
		return false
	}
	selectors := make(map[OriginSelector]struct{}, len(left))
	for _, selector := range left {
		selectors[selector] = struct{}{}
	}
	for _, selector := range right {
		if _, ok := selectors[selector]; !ok {
			return false
		}
	}
	return true
}
