package gateway

import (
	"fmt"

	"seamless-cors/internal/managedpac"
	"seamless-cors/internal/platform"
)

func cleanManagedPAC(adapter managedpac.FootprintAdapter) *CleanupFailureDetail {
	if err := managedpac.ClearFootprint(adapter); err != nil {
		return &CleanupFailureDetail{Subject: CleanupSubjectManagedPAC, Diagnostic: err.Error()}
	}
	return nil
}

func cleanGatewayFootprint(adapter managedpac.FootprintAdapter, coord *coordinator, ownedCache *stateCache) []CleanupFailureDetail {
	var failures []CleanupFailureDetail
	if failure := cleanManagedPAC(adapter); failure != nil {
		failures = append(failures, *failure)
	}
	if ownedCache != nil && len(failures) > 0 {
		return failures
	}
	var err error
	if ownedCache == nil {
		err = coord.Remove()
	} else {
		err = coord.RemoveOwned(*ownedCache)
	}
	if err != nil {
		failures = append(failures, CleanupFailureDetail{
			Subject:    CleanupSubjectGatewayStateCache,
			Diagnostic: fmt.Errorf("gateway state cache cleanup failed: %w", err).Error(),
		})
	}
	return failures
}

func inspectGatewayFootprint(adapter interface {
	CurrentPACState() ([]platform.PACServiceState, error)
}, coord *coordinator, stale bool, runtimeActive bool, ownerCache stateCache) CleanupStatusDetail {
	cacheState := CleanupStatusNone
	ownerCacheActive := ownerCache.HTTPRouterListen != "" && ownerCache.Token != "" && coord.Owns(ownerCache)
	if stale || (coord.Exists() && !ownerCacheActive) {
		cacheState = CleanupStatusNeeded
	}

	pac := CleanupSubjectStatusDetail{Subject: CleanupSubjectManagedPAC, State: CleanupStatusNone}
	if !runtimeActive {
		inspection, err := managedpac.InspectFootprint(adapter)
		if err != nil {
			pac.State = CleanupStatusUnknown
			pac.Diagnostic = err.Error()
		} else if inspection.Needed() {
			pac.State = CleanupStatusNeeded
		}
	}

	subjects := []CleanupSubjectStatusDetail{
		{Subject: CleanupSubjectGatewayStateCache, State: cacheState},
		pac,
	}
	return CleanupStatusDetail{State: aggregateCleanupStatus(subjects), Subjects: subjects}
}

func aggregateCleanupStatus(subjects []CleanupSubjectStatusDetail) CleanupStatusState {
	state := CleanupStatusNone
	for _, subject := range subjects {
		if subject.State == CleanupStatusNeeded {
			return CleanupStatusNeeded
		}
		if subject.State == CleanupStatusUnknown {
			state = CleanupStatusUnknown
		}
	}
	return state
}
