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

func parseHostSelector(selectorText string) (HostSelector, error) {
	u, err := url.Parse("//" + selectorText)
	if err != nil {
		return HostSelector{}, errors.New("host selector syntax is invalid")
	}

	hostname := u.Hostname()
	// IPv6
	if strings.Contains(hostname, ":") {
		hostname = "[" + hostname + "]"
	}
	if selectorText != hostname {
		return HostSelector{}, errors.New("host selector must not include a scheme, port, path, query, or fragment")
	}

	hostname = u.Hostname()

	// Decode the only supported wildcard form before validating the hostname.
	wildcard := false
	if strings.HasPrefix(hostname, "*.") {
		wildcard = true
		hostname = strings.TrimPrefix(hostname, "*.")
	}
	if hostname == "" || strings.Contains(hostname, "*") {
		return HostSelector{}, errors.New("wildcard must be * followed by a hostname")
	}
	if _, err := netip.ParseAddr(hostname); err == nil {
		if wildcard {
			return HostSelector{}, errors.New("host selector hostname is invalid")
		}
	} else if hostnameValidator.Var(hostname, "hostname_rfc1123") != nil {
		return HostSelector{}, errors.New("host selector hostname is invalid")
	}
	return HostSelector{
		Hostname: strings.ToLower(hostname),
		Wildcard: wildcard,
	}, nil
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
	hostname := u.Hostname()

	if _, err := netip.ParseAddr(hostname); err != nil &&
		hostnameValidator.Var(hostname, "hostname_rfc1123") != nil {
		return OriginSelector{}, errors.New("origin selector hostname is invalid")
	}

	portNumber := uint64(80)
	if scheme == "https" {
		portNumber = 443
	}
	if port := u.Port(); port != "" {
		portNumber, err = strconv.ParseUint(port, 10, 16)
		if err != nil || portNumber == 0 {
			return OriginSelector{}, errors.New("origin selector port must be between 1 and 65535")
		}
	}

	return OriginSelector{
		Scheme:   scheme,
		Hostname: strings.ToLower(hostname),
		Port:     uint16(portNumber),
	}, nil
}

var hostnameValidator = validator.New()
