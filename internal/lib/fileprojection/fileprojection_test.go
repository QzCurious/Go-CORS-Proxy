package fileprojection_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/lib/fileprojection"
)

const projectionTestTimeout = 4 * time.Second

var testOptions = fileprojection.Options{
	Debounce: 20 * time.Millisecond,
}

func TestProjectionPromotesOnlyConsumerCapability(t *testing.T) {
	projectionType := reflect.TypeOf((*fileprojection.Projection[string])(nil))
	if _, ok := projectionType.MethodByName("Updates"); !ok {
		t.Fatal("Projection does not promote Updates")
	}
	if _, ok := projectionType.MethodByName("Publish"); ok {
		t.Fatal("Projection exposes Publish")
	}
}

func TestOpenProjectsInitialOrdinaryFileWithoutPublishingIt(t *testing.T) {
	path := writeFile(t, "INITIAL\n")
	projection, err := openStrings(path)
	if err != nil {
		t.Fatal(err)
	}
	defer projection.Close()

	current := projection.Current()
	if current.Err != nil || current.Value != "initial" {
		t.Fatalf("current = %#v", current)
	}
	assertNoResult(t, projection.Updates(), 150*time.Millisecond)
}

func TestOpenRejectsInvalidInitialProjection(t *testing.T) {
	path := writeFile(t, "invalid")
	_, err := fileprojection.Open(path, func(data []byte) (string, error) {
		return "", errors.New("not accepted")
	}, func(left, right string) bool { return left == right }, testOptions)
	assertProjectionError(t, err, fileprojection.ErrorProject)
}

func TestOpenRejectsSymlinkedSource(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "source.txt")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}

	_, err := openStrings(path)
	assertProjectionError(t, err, fileprojection.ErrorRead)
}

func TestChangedProjectionPublishesAndAdvancesCurrentFirst(t *testing.T) {
	path := writeFile(t, "first")
	projection := mustOpenStrings(t, path)
	defer projection.Close()

	rewriteFile(t, path, "second")
	result := waitResult(t, projection.Updates())
	if result.Err != nil || result.Value != "second" {
		t.Fatalf("result = %#v", result)
	}
	if current := projection.Current(); current != result {
		t.Fatalf("current = %#v, published = %#v", current, result)
	}
}

func TestEqualProjectionSuppressesRepresentationOnlyChange(t *testing.T) {
	path := writeFile(t, "VALUE")
	projection := mustOpenStrings(t, path)
	defer projection.Close()

	rewriteFile(t, path, "value\n")
	assertNoResult(t, projection.Updates(), 250*time.Millisecond)
	if current := projection.Current(); current.Err != nil || current.Value != "value" {
		t.Fatalf("current = %#v", current)
	}
}

func TestProjectionFailurePublishesAfterDebounce(t *testing.T) {
	path := writeFile(t, "valid")
	projection := mustOpenStrings(t, path)
	defer projection.Close()

	rewriteFile(t, path, "invalid")
	assertNoResult(t, projection.Updates(), 10*time.Millisecond)
	result := waitResult(t, projection.Updates())
	assertProjectionError(t, result.Err, fileprojection.ErrorProject)
	if result.Value != "" {
		t.Fatalf("failed value = %q", result.Value)
	}
}

func TestRepeatedProjectionFailuresAreBothPublished(t *testing.T) {
	path := writeFile(t, "valid")
	projection := mustOpenStrings(t, path)
	defer projection.Close()

	rewriteFile(t, path, "invalid")
	first := waitResult(t, projection.Updates())
	assertProjectionError(t, first.Err, fileprojection.ErrorProject)

	rewriteFile(t, path, "invalid")
	second := waitResult(t, projection.Updates())
	assertProjectionError(t, second.Err, fileprojection.ErrorProject)
}

func TestTrailingDebounceAbsorbsTransientProjectionFailure(t *testing.T) {
	path := writeFile(t, "first")
	projection := mustOpenStrings(t, path)
	defer projection.Close()

	rewriteFile(t, path, "invalid")
	time.Sleep(10 * time.Millisecond)
	rewriteFile(t, path, "second")
	result := waitResult(t, projection.Updates())
	if result.Err != nil || result.Value != "second" {
		t.Fatalf("result = %#v", result)
	}
	assertNoResult(t, projection.Updates(), 150*time.Millisecond)
}

func TestRecoveryPublishesEvenWhenValueEqualsLastSuccess(t *testing.T) {
	path := writeFile(t, "value")
	projection := mustOpenStrings(t, path)
	defer projection.Close()

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	failed := waitResult(t, projection.Updates())
	assertProjectionError(t, failed.Err, fileprojection.ErrorRead)

	rewriteFile(t, path, "VALUE\n")
	recovered := waitResult(t, projection.Updates())
	if recovered.Err != nil || recovered.Value != "value" {
		t.Fatalf("recovered = %#v", recovered)
	}
}

func TestAtomicReplacementPublishes(t *testing.T) {
	path := writeFile(t, "first")
	projection := mustOpenStrings(t, path)
	defer projection.Close()

	temporary := filepath.Join(filepath.Dir(path), "replacement.tmp")
	if err := os.WriteFile(temporary, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, path); err != nil {
		t.Fatal(err)
	}
	result := waitResult(t, projection.Updates())
	if result.Err != nil || result.Value != "second" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCloseIsIdempotentAndClosesUpdates(t *testing.T) {
	projection := mustOpenStrings(t, writeFile(t, "value"))
	if err := projection.Close(); err != nil {
		t.Fatal(err)
	}
	if err := projection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-projection.Updates():
		if ok {
			t.Fatal("updates remained open")
		}
	case <-time.After(projectionTestTimeout):
		t.Fatal("updates did not close")
	}
}

func openStrings(path string) (*fileprojection.Projection[string], error) {
	return fileprojection.Open(path, func(data []byte) (string, error) {
		value := strings.TrimSpace(strings.ToLower(string(data)))
		if value == "invalid" {
			return "", errors.New("invalid value")
		}
		return value, nil
	}, func(left, right string) bool {
		return left == right
	}, testOptions)
}

func mustOpenStrings(t *testing.T, path string) *fileprojection.Projection[string] {
	t.Helper()
	projection, err := openStrings(path)
	if err != nil {
		t.Fatal(err)
	}
	return projection
}

func assertProjectionError(t *testing.T, err error, kind fileprojection.ErrorKind) {
	t.Helper()
	var projectionErr *fileprojection.Error
	if !errors.As(err, &projectionErr) || projectionErr.Kind != kind {
		t.Fatalf("error = %v, want fileprojection kind %v", err, kind)
	}
}

func waitResult[T any](t *testing.T, updates <-chan fileprojection.Result[T]) fileprojection.Result[T] {
	t.Helper()
	select {
	case result, ok := <-updates:
		if !ok {
			t.Fatal("updates closed unexpectedly")
		}
		return result
	case <-time.After(projectionTestTimeout):
		t.Fatal("timed out waiting for projection result")
		return fileprojection.Result[T]{}
	}
}

func assertNoResult[T any](t *testing.T, updates <-chan fileprojection.Result[T], duration time.Duration) {
	t.Helper()
	select {
	case result, ok := <-updates:
		if !ok {
			t.Fatal("updates closed unexpectedly")
		}
		t.Fatalf("unexpected result: %#v", result)
	case <-time.After(duration):
	}
}

func writeFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.txt")
	rewriteFile(t, path, contents)
	return path
}

func rewriteFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
