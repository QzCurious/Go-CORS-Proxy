package gateway

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
)

func useTestGatewayEnvironment(t *testing.T) (string, *coordinator) {
	t.Helper()
	home := t.TempDir()
	values := map[string]string{
		"HOME":            home,
		"XDG_CONFIG_HOME": filepath.Join(home, "config"),
		"XDG_STATE_HOME":  filepath.Join(home, "state"),
		"XDG_RUNTIME_DIR": filepath.Join(home, "runtime"),
	}
	type priorValue struct {
		value string
		set   bool
	}
	prior := make(map[string]priorValue, len(values))
	for name, value := range values {
		old, set := os.LookupEnv(name)
		prior[name] = priorValue{value: old, set: set}
		if err := os.Setenv(name, value); err != nil {
			t.Fatal(err)
		}
	}
	xdg.Reload()
	t.Cleanup(func() {
		for name, old := range prior {
			if old.set {
				_ = os.Setenv(name, old.value)
			} else {
				_ = os.Unsetenv(name)
			}
		}
		xdg.Reload()
	})
	coord, err := defaultCoordinator()
	if err != nil {
		t.Fatal(err)
	}
	return home, coord
}
