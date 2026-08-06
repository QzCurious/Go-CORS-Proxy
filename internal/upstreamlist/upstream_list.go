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

// Clone returns an independent Upstream List value.
func (u UpstreamList) Clone() UpstreamList {
	return UpstreamList{
		HostSelectors:   append([]HostSelector(nil), u.HostSelectors...),
		OriginSelectors: append([]OriginSelector(nil), u.OriginSelectors...),
		Warnings:        append([]Warning(nil), u.Warnings...),
	}
}

// HTTPSIntent reports whether the list contains at least one HTTPS Origin
// Selector.
func (u UpstreamList) HTTPSIntent() bool {
	for _, selector := range u.OriginSelectors {
		if selector.Scheme == "https" {
			return true
		}
	}
	return false
}
