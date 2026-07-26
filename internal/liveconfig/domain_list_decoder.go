package liveconfig

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"unicode/utf8"
)

// DomainListWarning describes one ignored invalid Domain List line.
type DomainListWarning struct {
	Line       int
	Text       string
	Diagnostic string
}

type domainListDecodeResult struct {
	entries  []DomainListEntry
	warnings []DomainListWarning
}

// decodeDomainList owns the Domain List text format described in
// domain-list-format.md. Invalid lines become warnings; source-level decoding
// failures remain errors.
func decodeDomainList(data []byte) (domainListDecodeResult, error) {
	if !utf8.Valid(data) {
		return domainListDecodeResult{}, fmt.Errorf("invalid Domain List: content must be UTF-8")
	}
	return decodeDomainListText(string(data)), nil
}

func decodeDomainListText(contents string) domainListDecodeResult {
	var entries []DomainListEntry
	var warnings []DomainListWarning
	seen := map[DomainListEntry]struct{}{}

	for idx, line := range strings.Split(contents, "\n") {
		lineNo := idx + 1
		text := stripDomainListComment(line)
		if text == "" {
			continue
		}
		value, err := decodeDomainListEntry(text)
		if err != nil {
			warnings = append(warnings, DomainListWarning{
				Line:       lineNo,
				Text:       text,
				Diagnostic: err.Error(),
			})
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		entries = append(entries, value)
	}
	return domainListDecodeResult{entries: entries, warnings: warnings}
}

func decodeDomainListEntry(text string) (DomainListEntry, error) {
	text = strings.TrimSpace(text)
	if strings.Contains(text, "://") {
		return decodeDomainListOrigin(text)
	}
	return decodeDomainListHostname(text)
}

func decodeDomainListOrigin(text string) (DomainListEntry, error) {
	u, err := url.Parse(text)
	if err != nil {
		return DomainListEntry{}, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return DomainListEntry{}, fmt.Errorf("origin scheme must be http or https")
	}
	if u.User != nil {
		return DomainListEntry{}, fmt.Errorf("origin must not include user information")
	}
	if u.Hostname() == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return DomainListEntry{}, fmt.Errorf("entry must be an origin without path, query, or fragment")
	}
	hostname := strings.ToLower(u.Hostname())
	if strings.Contains(hostname, "*") {
		return DomainListEntry{}, fmt.Errorf("wildcards require hostname shorthand")
	}
	port := u.Port()
	if port == "" {
		port = defaultDomainListPort(u.Scheme)
	}
	return DomainListEntry{
		Scheme:   u.Scheme,
		Hostname: hostname,
		Port:     port,
	}, nil
}

func decodeDomainListHostname(text string) (DomainListEntry, error) {
	if strings.Contains(text, "/") || strings.Contains(text, ":") {
		return DomainListEntry{}, fmt.Errorf("host shorthand must not include scheme, port, path, or IPv6")
	}
	hostname := strings.TrimSuffix(strings.ToLower(text), ".")
	if hostname == "" {
		return DomainListEntry{}, fmt.Errorf("host is required")
	}
	if strings.Contains(hostname, "#") || strings.ContainsAny(hostname, " \t") {
		return DomainListEntry{}, fmt.Errorf("host contains invalid characters")
	}
	wildcard := strings.HasPrefix(hostname, "*.")
	if strings.Contains(strings.TrimPrefix(hostname, "*."), "*") {
		return DomainListEntry{}, fmt.Errorf("wildcard must be a single leading label")
	}
	if wildcard && strings.Count(hostname, ".") < 2 {
		return DomainListEntry{}, fmt.Errorf("wildcard must include a concrete parent domain")
	}
	if !wildcard && net.ParseIP(hostname) != nil && strings.Contains(hostname, ":") {
		return DomainListEntry{}, fmt.Errorf("IPv6 entries require full origin syntax")
	}
	return DomainListEntry{
		Hostname: hostname,
	}, nil
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
