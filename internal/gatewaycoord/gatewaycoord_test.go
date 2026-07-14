package gatewaycoord

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestVerifyReportsMissingWithoutCache(t *testing.T) {
	coord := New(t.TempDir())

	verification := coord.Verify()

	if verification.Status != Missing {
		t.Fatalf("status = %s, want %s", verification.Status, Missing)
	}
}

func TestVerifyCollapsesMalformedCacheIntoStale(t *testing.T) {
	coord := New(t.TempDir())
	if err := os.MkdirAll(coord.RuntimeDirPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(coord.StateFilePath(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	verification := coord.Verify()

	if verification.Status != Stale {
		t.Fatalf("status = %s, want %s", verification.Status, Stale)
	}
}

func TestVerifyReportsStaleWhenRouterIsInactive(t *testing.T) {
	coord := NewWithVerifier(t.TempDir(), func(GatewayStateCache) bool { return false })
	if err := coord.Write(GatewayStateCache{HTTPRouterListen: "127.0.0.1:1", Token: "token"}); err != nil {
		t.Fatal(err)
	}

	verification := coord.Verify()

	if verification.Status != Stale {
		t.Fatalf("status = %s, want %s", verification.Status, Stale)
	}
}

func TestVerifyDoesNotReadLegacyControlListenSchema(t *testing.T) {
	coord := NewWithVerifier(t.TempDir(), func(cache GatewayStateCache) bool {
		if cache.HTTPRouterListen != "" {
			t.Fatalf("legacy controlListen was read as httpRouterListen: %#v", cache)
		}
		return cache.HTTPRouterListen != "" && cache.Token != ""
	})
	if err := os.MkdirAll(coord.RuntimeDirPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(coord.StateFilePath(), []byte(`{"controlListen":"127.0.0.1:1","token":"token"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	verification := coord.Verify()

	if verification.Status != Stale {
		t.Fatalf("status = %s, want %s", verification.Status, Stale)
	}
}

func TestVerifyReportsActiveWhenRouterResponds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/health" {
			http.NotFound(w, req)
			return
		}
		if got := req.Header.Get("X-Seamless-CORS-Token"); got != "token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	coord := New(t.TempDir())
	if err := coord.Write(GatewayStateCache{
		HTTPRouterListen: strings.TrimPrefix(server.URL, "http://"),
		Token:            "token",
	}); err != nil {
		t.Fatal(err)
	}

	verification := coord.Verify()

	if verification.Status != Active {
		t.Fatalf("status = %s, want %s", verification.Status, Active)
	}
}

func TestWriteUsesExclusiveCacheFile(t *testing.T) {
	coord := New(t.TempDir())
	cache := GatewayStateCache{HTTPRouterListen: "127.0.0.1:1", Token: "token"}
	if err := coord.Write(cache); err != nil {
		t.Fatal(err)
	}

	err := coord.Write(cache)

	if !os.IsExist(err) {
		t.Fatalf("second write error = %v, want exists", err)
	}
}

func TestCacheShapeContainsOnlyRouterIdentity(t *testing.T) {
	coord := New(t.TempDir())
	cache := GatewayStateCache{HTTPRouterListen: "127.0.0.1:1", Token: "token"}
	if err := coord.Write(cache); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(coord.StateFilePath())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["httpRouterListen"] != cache.HTTPRouterListen || got["token"] != cache.Token {
		t.Fatalf("cache json = %s", data)
	}
}

func TestRemoveOwnedDoesNotRemoveAnotherOwnerCache(t *testing.T) {
	coord := New(t.TempDir())
	other := GatewayStateCache{HTTPRouterListen: "127.0.0.1:2", Token: "other-token"}
	if err := coord.Write(other); err != nil {
		t.Fatal(err)
	}

	err := coord.RemoveOwned(GatewayStateCache{HTTPRouterListen: "127.0.0.1:1", Token: "token"})

	if err != nil {
		t.Fatal(err)
	}
	if !coord.Owns(other) {
		t.Fatal("removed another owner's cache")
	}
}

func TestClaimReplacesStaleCache(t *testing.T) {
	coord := NewWithVerifier(t.TempDir(), func(GatewayStateCache) bool { return false })
	stale := GatewayStateCache{HTTPRouterListen: "127.0.0.1:1", Token: "stale-token"}
	if err := coord.Write(stale); err != nil {
		t.Fatal(err)
	}
	current := GatewayStateCache{HTTPRouterListen: "127.0.0.1:2", Token: "current-token"}

	if err := coord.Claim(current); err != nil {
		t.Fatal(err)
	}

	if !coord.Owns(current) {
		t.Fatal("claim did not replace stale cache with current owner")
	}
}
