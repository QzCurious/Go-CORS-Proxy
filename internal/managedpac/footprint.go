package managedpac

import (
	"net"
	"net/url"
	"path"
	"strings"
)

const FootprintFileName = "seamless-cors.pac"

func isOwnedURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return path.Base(u.EscapedPath()) == FootprintFileName
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback() && path.Base(u.EscapedPath()) == FootprintFileName
}
