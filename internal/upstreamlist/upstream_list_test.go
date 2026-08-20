package upstreamlist_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/upstreamlist"
)

func TestDefaultContentsFormAValidProjection(t *testing.T) {
	projection, err := upstreamlist.Project([]byte(upstreamlist.DefaultContents))
	if err != nil {
		t.Fatalf("Project(DefaultContents): %v", err)
	}
	if !reflect.DeepEqual(projection, upstreamlist.Projection{}) {
		t.Fatalf("DefaultContents projection = %#v, want empty projection", projection)
	}
}

func TestProjectProducesNormalizedDeduplicatedProjection(t *testing.T) {
	projection, err := upstreamlist.Project([]byte("API.EXAMPLE.TEST\napi.example.test\nhttps://secure.example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.HostSelectors) != 1 ||
		projection.HostSelectors[0].Hostname != "api.example.test" ||
		len(projection.OriginSelectors) != 1 {
		t.Fatalf("projection = %#v", projection)
	}
}

func TestProjectKeepsExactAndWildcardHostSelectorsDistinct(t *testing.T) {
	projection, err := upstreamlist.Project([]byte("example.test\n*.example.test\nexample.test\n*.example.test\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []upstreamlist.HostSelector{
		{Hostname: "example.test"},
		{Hostname: "example.test", Wildcard: true},
	}
	if !reflect.DeepEqual(projection.HostSelectors, want) {
		t.Fatalf("Host Selectors = %#v, want %#v", projection.HostSelectors, want)
	}
}

func TestProjectReturnsConcreteWholeDocumentError(t *testing.T) {
	projection, err := upstreamlist.Project([]byte{0xff})
	var encodingErr *upstreamlist.InvalidEncodingError
	if !errors.As(err, &encodingErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if !reflect.DeepEqual(projection, upstreamlist.Projection{}) {
		t.Fatalf("projection on error = %#v", projection)
	}
}

func TestProjectionZeroValueIsCanonicalEmpty(t *testing.T) {
	projection, err := upstreamlist.Project(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(projection, upstreamlist.Projection{}) {
		t.Fatalf("empty projection = %#v", projection)
	}
}
