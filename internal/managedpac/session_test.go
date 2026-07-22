package managedpac

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeAdapter struct {
	pacStates    []ServiceSnapshot
	installedURL string
	installed    []string
	installOut   []string
	refreshes    []string
	installErr   error
	refreshErr   error
	currentErr   error
	currentCalls int
	applyStarted chan struct{}
	applyRelease chan struct{}
}

func (f *fakeAdapter) Apply(_ context.Context, url string, services []string) (ApplyResult, error) {
	if f.applyStarted != nil {
		close(f.applyStarted)
		<-f.applyRelease
	}
	f.installedURL = url
	f.installed = append([]string(nil), services...)
	f.refreshes = append(f.refreshes, url)
	for idx := range f.pacStates {
		for _, service := range services {
			if f.pacStates[idx].ServiceName == service {
				f.pacStates[idx].PACURL = url
				f.pacStates[idx].Enabled = true
			}
		}
	}
	if len(f.refreshes) > 1 && f.refreshErr != nil {
		return ApplyResult{}, f.refreshErr
	}
	if f.installOut != nil {
		return ApplyResult{AppliedServices: append([]string(nil), f.installOut...)}, f.installErr
	}
	return ApplyResult{AppliedServices: append([]string(nil), f.installed...)}, f.installErr
}

func (f *fakeAdapter) Snapshot(context.Context) ([]ServiceSnapshot, error) {
	f.currentCalls++
	if f.currentErr != nil {
		return nil, f.currentErr
	}
	return append([]ServiceSnapshot(nil), f.pacStates...), nil
}

func (f *fakeAdapter) ClearIfUnchanged(_ context.Context, expected []ServiceSnapshot) error {
	for idx, snapshot := range f.pacStates {
		for _, want := range expected {
			if snapshot == want {
				f.pacStates[idx].Enabled = false
			}
		}
	}
	return nil
}

func TestAssessPropagatesCurrentPACStateError(t *testing.T) {
	wantErr := errors.New("inspection denied")

	_, err := Assess(context.Background(), &fakeAdapter{currentErr: wantErr})

	if !errors.Is(err, wantErr) {
		t.Fatalf("assessment error = %v", err)
	}
}

func TestAssessSelectsServicesAndReportsReplacement(t *testing.T) {
	assessment, err := Assess(context.Background(), &fakeAdapter{
		pacStates: []ServiceSnapshot{
			{ServiceName: "USB", PACURL: "", Enabled: false},
			{ServiceName: "Wi-Fi", PACURL: "http://corp.example/proxy.pac", Enabled: true},
			{ServiceName: "Ethernet", PACURL: "http://127.0.0.1:49152/seamless-cors.pac?v=1", Enabled: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if !assessment.ReplacementRequired {
		t.Fatal("foreign PAC state should require replacement")
	}
	wantServices := "Ethernet,USB,Wi-Fi"
	if got := strings.Join(assessment.ServiceSet, ","); got != wantServices {
		t.Fatalf("service set = %s, want %s", got, wantServices)
	}
	if assessment.Services[0].ServiceName != "Ethernet" || assessment.Services[0].Ownership != OwnershipOwned {
		t.Fatalf("states not sorted/classified: %#v", assessment.Services)
	}
}

func TestStartInstallsInitialURLAndKeepsSelectedServices(t *testing.T) {
	adapter := &fakeAdapter{}

	session, result, err := Start(context.Background(), adapter, []string{"Wi-Fi", "Ethernet"}, "http://127.0.0.1:1/seamless-cors.pac?v=1")
	if err != nil {
		t.Fatal(err)
	}

	if session.CurrentURL() != "http://127.0.0.1:1/seamless-cors.pac?v=1" {
		t.Fatalf("current URL = %q", session.CurrentURL())
	}
	if got := strings.Join(session.Services(), ","); got != "Ethernet,Wi-Fi" {
		t.Fatalf("services = %s", got)
	}
	if got := strings.Join(result.InstalledServices, ","); got != "Ethernet,Wi-Fi" {
		t.Fatalf("installed services = %s", got)
	}
	if adapter.currentCalls != 0 {
		t.Fatalf("start rediscovered PAC state %d times", adapter.currentCalls)
	}
}

func TestPreparedSessionClosedBeforeInstallPerformsNoPlatformWrite(t *testing.T) {
	adapter := &fakeAdapter{}
	session, err := Prepare(adapter, []string{"Wi-Fi"}, "http://127.0.0.1/seamless-cors.pac?v=1")
	if err != nil {
		t.Fatal(err)
	}
	session.Close()

	if _, err := session.Install(context.Background()); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("install error = %v, want %v", err, ErrSessionClosed)
	}
	if adapter.installedURL != "" {
		t.Fatalf("platform write URL = %q, want none", adapter.installedURL)
	}
}

func TestStartRejectsEmptyServiceSet(t *testing.T) {
	_, _, err := Start(context.Background(), &fakeAdapter{}, nil, "http://127.0.0.1:1/seamless-cors.pac?v=1")

	if err == nil || !strings.Contains(err.Error(), "managed PAC service set is empty") {
		t.Fatalf("start error = %v", err)
	}
}

func TestStartRejectsZeroInstalledServices(t *testing.T) {
	adapter := &fakeAdapter{installOut: []string{}}

	_, _, err := Start(context.Background(), adapter, []string{"Wi-Fi"}, "http://127.0.0.1:1/seamless-cors.pac?v=1")

	if err == nil || !strings.Contains(err.Error(), "managed PAC install updated no services") {
		t.Fatalf("start error = %v", err)
	}
}

func TestRefreshCommitsCurrentURLOnlyAfterSuccess(t *testing.T) {
	adapter := &fakeAdapter{refreshErr: errors.New("refresh denied")}
	session, _, err := Start(context.Background(), adapter, []string{"Wi-Fi"}, "http://127.0.0.1:1/seamless-cors.pac?v=1")
	if err != nil {
		t.Fatal(err)
	}

	err = session.Refresh(context.Background(), "http://127.0.0.1:1/seamless-cors.pac?v=2")
	if err == nil {
		t.Fatal("expected refresh error")
	}
	var refreshErr RefreshError
	if !errors.As(err, &refreshErr) {
		t.Fatalf("error type = %T", err)
	}
	if session.CurrentURL() != "http://127.0.0.1:1/seamless-cors.pac?v=1" {
		t.Fatalf("current URL changed after failed refresh: %q", session.CurrentURL())
	}
	if session.AttemptedURL() != "http://127.0.0.1:1/seamless-cors.pac?v=2" {
		t.Fatalf("attempted URL = %q", session.AttemptedURL())
	}

	adapter.refreshErr = nil
	if err := session.Refresh(context.Background(), "http://127.0.0.1:1/seamless-cors.pac?v=3"); err != nil {
		t.Fatal(err)
	}
	if session.CurrentURL() != "http://127.0.0.1:1/seamless-cors.pac?v=3" || session.AttemptedURL() != "" {
		t.Fatalf("refresh state current=%q attempted=%q", session.CurrentURL(), session.AttemptedURL())
	}
}

func TestRefreshDoesNotOverwriteForeignSelectedPACState(t *testing.T) {
	currentURL := "http://127.0.0.1:49152/seamless-cors.pac?v=1"
	nextURL := "http://127.0.0.1:49152/seamless-cors.pac?v=2"
	adapter := &fakeAdapter{pacStates: []ServiceSnapshot{{ServiceName: "Wi-Fi"}}}
	session, _, err := Start(context.Background(), adapter, []string{"Wi-Fi"}, currentURL)
	if err != nil {
		t.Fatal(err)
	}
	adapter.pacStates[0] = ServiceSnapshot{
		ServiceName: "Wi-Fi", PACURL: "http://corp.example/proxy.pac", Enabled: true,
	}
	writesBefore := len(adapter.refreshes)

	err = session.Refresh(context.Background(), nextURL)

	if !errors.Is(err, ErrManagedPACLeaseLost) {
		t.Fatalf("refresh error = %v, want managed PAC lease lost", err)
	}
	if len(adapter.refreshes) != writesBefore {
		t.Fatalf("refresh overwrote foreign PAC state: writes before=%d after=%d", writesBefore, len(adapter.refreshes))
	}
	if session.CurrentURL() != currentURL {
		t.Fatalf("current URL = %q after rejected refresh", session.CurrentURL())
	}
}

func TestRequireLeaseAllowsMissingSelectedService(t *testing.T) {
	url := "http://127.0.0.1:49152/seamless-cors.pac?v=1"
	adapter := &fakeAdapter{
		pacStates: []ServiceSnapshot{{ServiceName: "Wi-Fi", PACURL: url, Enabled: true}},
	}
	session, _, err := Start(context.Background(), adapter, []string{"Wi-Fi", "Ethernet"}, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.RequireLease(context.Background()); err != nil {
		t.Fatalf("missing selected service should not lose the managed PAC lease: %v", err)
	}
}

func TestRequireLeaseRejectsVisibleChangedSelectedService(t *testing.T) {
	url := "http://127.0.0.1:49152/seamless-cors.pac?v=1"
	adapter := &fakeAdapter{
		pacStates: []ServiceSnapshot{
			{ServiceName: "Wi-Fi", PACURL: url, Enabled: true},
			{ServiceName: "Ethernet", PACURL: "http://corp.example/proxy.pac", Enabled: true},
		},
	}
	session, _, err := Start(context.Background(), adapter, []string{"Wi-Fi", "Ethernet"}, url)
	if err != nil {
		t.Fatal(err)
	}
	adapter.pacStates[1].PACURL = "http://corp.example/proxy.pac"

	if !errors.Is(session.RequireLease(context.Background()), ErrManagedPACLeaseLost) {
		t.Fatal("visible selected service with replaced PAC should lose the managed PAC lease")
	}
}

func TestRequireLeaseReattachesVisibleSelectedServiceWithOwnedMarker(t *testing.T) {
	currentURL := "http://127.0.0.1:49152/seamless-cors.pac?v=2"
	adapter := &fakeAdapter{
		pacStates: []ServiceSnapshot{
			{ServiceName: "Wi-Fi", PACURL: currentURL, Enabled: true},
			{ServiceName: "Ethernet", PACURL: "http://localhost:48000/seamless-cors.pac?v=1", Enabled: false},
			{ServiceName: "New VPN", PACURL: "http://corp.example/proxy.pac", Enabled: true},
		},
	}
	session, _, err := Start(context.Background(), adapter, []string{"Wi-Fi", "Ethernet"}, currentURL)
	if err != nil {
		t.Fatal(err)
	}
	adapter.pacStates[1].PACURL = "http://localhost:48000/seamless-cors.pac?v=1"
	adapter.pacStates[1].Enabled = false
	adapter.installed = nil

	if err := session.RequireLease(context.Background()); err != nil {
		t.Fatalf("owned selected service should reattach: %v", err)
	}
	if got := strings.Join(adapter.installed, ","); got != "Ethernet" {
		t.Fatalf("reattached services = %q, want Ethernet", got)
	}
	if adapter.pacStates[1].PACURL != currentURL || !adapter.pacStates[1].Enabled {
		t.Fatalf("reattached state = %+v", adapter.pacStates[1])
	}
	if adapter.pacStates[2].PACURL != "http://corp.example/proxy.pac" {
		t.Fatalf("new unselected service was modified: %+v", adapter.pacStates[2])
	}
}

func TestRequireLeaseWrapsInspectionFailure(t *testing.T) {
	wantErr := errors.New("inspection denied")
	session, _, err := Start(context.Background(), &fakeAdapter{}, []string{"Wi-Fi"}, "http://127.0.0.1:49152/seamless-cors.pac?v=1")
	if err != nil {
		t.Fatal(err)
	}
	session.settings = &fakeAdapter{currentErr: wantErr}

	err = session.RequireLease(context.Background())

	if !errors.Is(err, wantErr) {
		t.Fatalf("lease error = %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "managed PAC lease inspection failed") {
		t.Fatalf("lease error missing context: %v", err)
	}
}

func TestCloseWaitsForCurrentMutationAndRejectsLaterWrites(t *testing.T) {
	adapter := &fakeAdapter{}
	session, _, err := Start(context.Background(), adapter, []string{"Wi-Fi"}, "http://127.0.0.1:49152/seamless-cors.pac?v=1")
	if err != nil {
		t.Fatal(err)
	}
	adapter.applyStarted = make(chan struct{})
	adapter.applyRelease = make(chan struct{})
	applyStarted := adapter.applyStarted
	refreshDone := make(chan error, 1)
	go func() {
		refreshDone <- session.Refresh(context.Background(), "http://127.0.0.1:49152/seamless-cors.pac?v=2")
	}()
	<-applyStarted

	closeDone := make(chan struct{})
	go func() {
		session.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
		t.Fatal("close returned before the current PAC mutation settled")
	default:
	}
	close(adapter.applyRelease)
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
	<-closeDone
	writesBefore := len(adapter.refreshes)

	if err := session.Refresh(context.Background(), "http://127.0.0.1:49152/seamless-cors.pac?v=3"); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("refresh after close = %v", err)
	}
	if len(adapter.refreshes) != writesBefore {
		t.Fatalf("refresh wrote after close: before=%d after=%d", writesBefore, len(adapter.refreshes))
	}
}
