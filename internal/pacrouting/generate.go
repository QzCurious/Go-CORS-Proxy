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
	routes := deriveRoutes(opts.DomainListEntries, opts.CATrusted)
	type pacTemplateData struct {
		Proxy  string
		Routes string
	}
	data := pacTemplateData{
		Proxy:  pacJSONLiteral("PROXY " + opts.ProxyListen),
		Routes: pacJSONLiteral(routes),
	}
	var body strings.Builder
	if err := pacTemplate.Execute(&body, data); err != nil {
		panic(err)
	}
	return body.String()
}

type pacLiteral interface {
	string | []route
}

func pacJSONLiteral[T pacLiteral](value T) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
