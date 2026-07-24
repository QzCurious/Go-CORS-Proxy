package liveconfig

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// DomainListEntry is a validated, normalized routing input produced by Live
// Configuration. Consumers can inspect its private value but cannot construct
// a valid entry independently.
type DomainListEntry struct {
	value domainListEntry
	valid bool
}

type domainListEntry struct {
	scheme   string
	hostname string
	port     string
	wildcard bool
}

func (e DomainListEntry) Scheme() string {
	return e.semanticValue().scheme
}

func (e DomainListEntry) Hostname() string {
	return e.semanticValue().hostname
}

func (e DomainListEntry) Port() string {
	return e.semanticValue().port
}

func (e DomainListEntry) IsWildcard() bool {
	return e.semanticValue().wildcard
}

func (e DomainListEntry) semanticValue() domainListEntry {
	if !e.valid {
		panic("invalid zero-value Domain List Entry")
	}
	return e.value
}

type domainListLineError struct {
	line int
	text string
	err  error
}

func (e domainListLineError) Error() string {
	return fmt.Sprintf("line %d: %s: %v", e.line, e.text, e.err)
}

func parseDomainList(data []byte) ([]DomainListEntry, error) {
	entries, errs := parseDomainListText(string(data))
	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid Domain List:\n%s", formatDomainListErrors(errs))
	}
	return entries, nil
}

func parseDomainListText(contents string) ([]DomainListEntry, []domainListLineError) {
	var entries []DomainListEntry
	var errs []domainListLineError
	seen := map[domainListEntry]struct{}{}

	for idx, line := range strings.Split(contents, "\n") {
		lineNo := idx + 1
		text := stripDomainListComment(line)
		if text == "" {
			continue
		}
		value, err := parseDomainListEntry(text)
		if err != nil {
			errs = append(errs, domainListLineError{line: lineNo, text: text, err: err})
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		entries = append(entries, DomainListEntry{value: value, valid: true})
	}
	return entries, errs
}

func parseDomainListEntry(text string) (domainListEntry, error) {
	text = strings.TrimSpace(text)
	if strings.Contains(text, "://") {
		return parseDomainListOrigin(text)
	}
	return parseDomainListHostname(text)
}

func parseDomainListOrigin(text string) (domainListEntry, error) {
	u, err := url.Parse(text)
	if err != nil {
		return domainListEntry{}, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return domainListEntry{}, fmt.Errorf("origin scheme must be http or https")
	}
	if u.Hostname() == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return domainListEntry{}, fmt.Errorf("entry must be an origin without path, query, or fragment")
	}
	hostname := strings.ToLower(u.Hostname())
	if strings.Contains(hostname, "*") {
		return domainListEntry{}, fmt.Errorf("wildcards require hostname shorthand")
	}
	port := u.Port()
	if port == "" {
		port = defaultDomainListPort(u.Scheme)
	}
	return domainListEntry{scheme: u.Scheme, hostname: hostname, port: port}, nil
}

func parseDomainListHostname(text string) (domainListEntry, error) {
	if strings.Contains(text, "/") || strings.Contains(text, ":") {
		return domainListEntry{}, fmt.Errorf("host shorthand must not include scheme, port, path, or IPv6")
	}
	hostname := strings.TrimSuffix(strings.ToLower(text), ".")
	if hostname == "" {
		return domainListEntry{}, fmt.Errorf("host is required")
	}
	if strings.Contains(hostname, "#") || strings.ContainsAny(hostname, " \t") {
		return domainListEntry{}, fmt.Errorf("host contains invalid characters")
	}
	wildcard := strings.HasPrefix(hostname, "*.")
	if strings.Contains(strings.TrimPrefix(hostname, "*."), "*") {
		return domainListEntry{}, fmt.Errorf("wildcard must be a single leading label")
	}
	if wildcard && strings.Count(hostname, ".") < 2 {
		return domainListEntry{}, fmt.Errorf("wildcard must include a concrete parent domain")
	}
	if !wildcard && net.ParseIP(hostname) != nil && strings.Contains(hostname, ":") {
		return domainListEntry{}, fmt.Errorf("IPv6 entries require full origin syntax")
	}
	return domainListEntry{hostname: hostname, wildcard: wildcard}, nil
}

func sameDomainListEntries(left, right []DomainListEntry) bool {
	if len(left) != len(right) {
		return false
	}
	entries := make(map[domainListEntry]struct{}, len(left))
	for _, entry := range left {
		entries[entry.semanticValue()] = struct{}{}
	}
	for _, entry := range right {
		if _, ok := entries[entry.semanticValue()]; !ok {
			return false
		}
	}
	return true
}

func stripDomainListComment(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") {
		return ""
	}
	for idx := 1; idx < len(line); idx++ {
		if line[idx] == '#' && (line[idx-1] == ' ' || line[idx-1] == '\t') {
			line = line[:idx]
			break
		}
	}
	return strings.TrimSpace(line)
}

func defaultDomainListPort(scheme string) string {
	switch scheme {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func formatDomainListErrors(errs []domainListLineError) string {
	lines := make([]string, 0, len(errs))
	for _, err := range errs {
		lines = append(lines, err.Error())
	}
	return strings.Join(lines, "\n")
}
