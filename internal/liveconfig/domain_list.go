package liveconfig

type HostMatch uint8

const (
	HostExact HostMatch = iota
	HostSingleLevel
	HostRecursive
)

// DomainListEntry is normalized routing data. Empty Scheme leaves the scheme
// unconstrained. Empty Port selects any port without a scheme and the default
// port with one. Hostname never includes wildcard syntax.
type DomainListEntry struct {
	Scheme    string
	Hostname  string
	Port      string
	HostMatch HostMatch
}

func sameDomainListEntries(left, right []DomainListEntry) bool {
	if len(left) != len(right) {
		return false
	}
	entries := make(map[DomainListEntry]struct{}, len(left))
	for _, entry := range left {
		entries[entry] = struct{}{}
	}
	for _, entry := range right {
		if _, ok := entries[entry]; !ok {
			return false
		}
	}
	return true
}

func sameDomainListWarnings(left, right []DomainListWarning) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}
