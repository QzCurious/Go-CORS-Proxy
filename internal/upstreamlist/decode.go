package upstreamlist

import (
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-playground/validator/v10"
)

type parsedUpstreamList struct {
	HostSelectors   []HostSelector
	OriginSelectors []OriginSelector
	Warnings        []Warning
}

type InvalidEncodingError struct{}

const invalidSelectorDiagnostic = "invalid selector: expected a hostname or HTTP(S) origin"

func (*InvalidEncodingError) Error() string {
	return "invalid Upstream List: content must be UTF-8"
}

// decode parses the Upstream List format described in
// [upstream-list-format.md](upstream-list-format.md). It does not apply
// source-level deduplication. Invalid lines become warnings; source-level
// decoding failures remain errors.
func decode(data []byte) (parsedUpstreamList, error) {
	if !utf8.Valid(data) {
		return parsedUpstreamList{}, &InvalidEncodingError{}
	}

	var (
		hostSelectors   []HostSelector
		originSelectors []OriginSelector
		warnings        []Warning
	)

	// Decode raw selectors.
	for idx, line := range strings.Split(string(data), "\n") {
		lineNo := idx + 1
		selectorText := stripComment(line)
		if selectorText == "" {
			continue
		}

		if selector, err := parseHostSelector(selectorText); err == nil {
			hostSelectors = append(hostSelectors, selector)
			continue
		}
		if selector, err := parseOriginSelector(selectorText); err == nil {
			originSelectors = append(originSelectors, selector)
			continue
		}
		warnings = append(warnings, Warning{
			Line:       lineNo,
			Text:       selectorText,
			Diagnostic: invalidSelectorDiagnostic,
		})
	}

	return parsedUpstreamList{
		HostSelectors:   hostSelectors,
		OriginSelectors: originSelectors,
		Warnings:        warnings,
	}, nil
}

func stripComment(line string) string {
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

func parseOriginSelector(selectorText string) (OriginSelector, error) {
	u, err := url.ParseRequestURI(selectorText)
	if err != nil {
		return OriginSelector{}, errors.New("origin selector syntax is invalid")
	}
	if u.RequestURI() != "/" {
		return OriginSelector{}, errors.New("origin selector must contain only an HTTP(S) origin")
	}
	if strings.HasSuffix(u.Host, ":") {
		return OriginSelector{}, errors.New("origin selector port must not be empty")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return OriginSelector{}, errors.New("origin selector scheme must be http or https")
	}
	hostname, err := normalizedHostname(u)
	if err != nil {
		return OriginSelector{}, err
	}

	if !isValidHostname(hostname, false) {
		return OriginSelector{}, errors.New("origin selector hostname is invalid")
	}

	port := u.Port()
	if port != "" {
		portNumber, err := strconv.ParseUint(port, 10, 16)
		if err != nil || portNumber == 0 {
			return OriginSelector{}, errors.New("origin selector port must be between 1 and 65535")
		}
		port = strconv.FormatUint(portNumber, 10)
	}

	return OriginSelector{
		Scheme:   scheme,
		Hostname: hostname,
		Port:     port,
	}, nil
}

func parseHostSelector(selectorText string) (HostSelector, error) {
	u, err := url.Parse("//" + selectorText)
	if err != nil {
		return HostSelector{}, errors.New("host selector syntax is invalid")
	}

	parsedHostnameText := u.Hostname()
	if strings.Contains(parsedHostnameText, ":") {
		parsedHostnameText = "[" + parsedHostnameText + "]"
	}
	if selectorText != parsedHostnameText {
		return HostSelector{}, errors.New("host selector must contain only a hostname")
	}

	hostname, err := normalizedHostname(u)
	if err != nil {
		return HostSelector{}, err
	}

	// Decode the only supported wildcard form before validating the hostname.
	wildcard := false
	if strings.HasPrefix(hostname, "*.") {
		wildcard = true
		hostname = strings.TrimPrefix(hostname, "*.")
	}
	if hostname == "" || strings.Contains(hostname, "*") {
		return HostSelector{}, errors.New("wildcard must be * followed by a hostname")
	}
	if !isValidHostname(hostname, wildcard) {
		return HostSelector{}, errors.New("host selector hostname is invalid")
	}
	return HostSelector{
		Hostname: hostname,
		Wildcard: wildcard,
	}, nil
}

func normalizedHostname(u *url.URL) (string, error) {
	hostname := strings.ToLower(u.Hostname())
	if hostname == "" {
		return "", errors.New("hostname is required")
	}
	for _, char := range hostname {
		if char > 127 {
			return "", errors.New("hostname must use ASCII or punycode")
		}
	}
	return hostname, nil
}

var hostnameValidator = validator.New()

func isValidHostname(hostname string, wildcard bool) bool {
	if address, err := netip.ParseAddr(hostname); err == nil {
		return !wildcard && address.Zone() == ""
	}
	return hostnameValidator.Var(hostname, "hostname_rfc1123") == nil
}
