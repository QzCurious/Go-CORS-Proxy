package pacrouting

import (
	_ "embed"
	"encoding/json"
	"strings"
	"text/template"

	"seamless-cors/internal/liveconfig"
)

type Options struct {
	ProxyListen       string
	CATrusted         bool
	DomainListEntries []liveconfig.DomainListEntry
}

//go:embed proxy.pac.tmpl
var pacTemplateSource string

var pacTemplate = template.Must(template.New("proxy.pac.tmpl").Parse(pacTemplateSource))

func Generate(opts Options) string {
	buckets := deriveRouteBuckets(opts.DomainListEntries, opts.CATrusted)
	type pacTemplateData struct {
		Proxy           string
		ExactHosts      string
		WildcardParents string
		Origins         string
	}
	data := pacTemplateData{
		Proxy:           pacJSONLiteral("PROXY " + opts.ProxyListen),
		ExactHosts:      pacJSONLiteral(buckets.exactHosts),
		WildcardParents: pacJSONLiteral(buckets.wildcardParents),
		Origins:         pacJSONLiteral(buckets.origins),
	}
	var body strings.Builder
	if err := pacTemplate.Execute(&body, data); err != nil {
		panic(err)
	}
	return body.String()
}

type pacLiteral interface {
	string | []hostRoute | []originRoute
}

func pacJSONLiteral[T pacLiteral](value T) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
