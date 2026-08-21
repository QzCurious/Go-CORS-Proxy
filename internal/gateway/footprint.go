package gateway

import (
	"context"
	"fmt"
)

func cleanManagedPACActiveState(ctx context.Context, pac managedPACModule) *CleanupFailureDetail {
	if err := pac.CleanupActiveState(ctx); err != nil {
		return &CleanupFailureDetail{Subject: CleanupSubjectManagedPAC, Diagnostic: err.Error()}
	}
	return nil
}

func uninstallManagedPAC(ctx context.Context, pac managedPACModule) *CleanupFailureDetail {
	if err := pac.Uninstall(ctx); err != nil {
		return &CleanupFailureDetail{Subject: CleanupSubjectManagedPAC, Diagnostic: err.Error()}
	}
	return nil
}

func cleanGatewayFootprint(ctx context.Context, pac managedPACModule, coord *coordinator, ownedCache *stateCache) []CleanupFailureDetail {
	var failures []CleanupFailureDetail
	if failure := cleanManagedPACActiveState(ctx, pac); failure != nil {
		failures = append(failures, *failure)
	}
	return append(failures, cleanGatewayStateCache(coord, ownedCache)...)
}

func cleanGatewayStateCache(coord *coordinator, ownedCache *stateCache) []CleanupFailureDetail {
	var err error
	if ownedCache == nil {
		err = coord.Remove()
	} else {
		err = coord.RemoveOwned(*ownedCache)
	}
	if err == nil {
		return nil
	}
	return []CleanupFailureDetail{{
		Subject:    CleanupSubjectGatewayStateCache,
		Diagnostic: fmt.Errorf("gateway state cache cleanup failed: %w", err).Error(),
	}}
}

func inspectGatewayFootprint(ctx context.Context, pacModule managedPACModule, coord *coordinator, stale bool, runtimeActive bool, ownerCache stateCache) CleanupStatusDetail {
	cacheState := CleanupStatusNone
	ownerCacheActive := ownerCache.HTTPRouterListen != "" && ownerCache.Token != "" && coord.Owns(ownerCache)
	if stale || (coord.Exists() && !ownerCacheActive) {
		cacheState = CleanupStatusNeeded
	}

	pac := CleanupSubjectStatusDetail{Subject: CleanupSubjectManagedPAC, State: CleanupStatusNone}
	if !runtimeActive {
		snapshot, err := pacModule.Inspect(ctx)
		if err != nil {
			pac.State = CleanupStatusUnknown
			pac.Diagnostic = err.Error()
		} else if snapshot.HasActiveOwnedState() {
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
