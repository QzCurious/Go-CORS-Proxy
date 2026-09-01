package gateway

import (
	"context"
	"fmt"

	"github.com/QzCurious/seamless-cors/internal/managedpac"
)

func cleanManagedPAC(ctx context.Context, pac managedpac.Footprint) ([]ManagedPACObservationIssue, *CleanupFailure) {
	result, err := pac.Cleanup(ctx)
	issues := managedPACObservationIssueDetails(result.ObservationIssues)
	if err != nil {
		return issues, &CleanupFailure{Subject: CleanupSubjectManagedPAC, Diagnostic: err.Error()}
	}
	return issues, nil
}

func closeManagedPAC(control managedpac.Control) ([]ManagedPACObservationIssue, *CleanupFailure) {
	result, err := control.Close()
	issues := managedPACObservationIssueDetails(result.ObservationIssues)
	if err != nil {
		return issues, &CleanupFailure{Subject: CleanupSubjectManagedPAC, Diagnostic: err.Error()}
	}
	return issues, nil
}

func cleanGatewayFootprint(ctx context.Context, pac managedpac.Footprint, coord *coordinator, ownedCache *stateCache) ([]ManagedPACObservationIssue, []CleanupFailure) {
	var failures []CleanupFailure
	issues, failure := cleanManagedPAC(ctx, pac)
	if failure != nil {
		failures = append(failures, *failure)
	}
	return issues, append(failures, cleanGatewayStateCache(coord, ownedCache)...)
}

func cleanGatewayStateCache(coord *coordinator, ownedCache *stateCache) []CleanupFailure {
	var err error
	if ownedCache == nil {
		err = coord.Remove()
	} else {
		err = coord.RemoveOwned(*ownedCache)
	}
	if err == nil {
		return nil
	}
	return []CleanupFailure{{
		Subject:    CleanupSubjectGatewayStateCache,
		Diagnostic: fmt.Errorf("gateway state cache cleanup failed: %w", err).Error(),
	}}
}

func inspectGatewayFootprint(ctx context.Context, pacModule managedpac.Footprint, coord *coordinator, stale bool, runtimeActive bool, ownerCache stateCache) CleanupStatusDetail {
	cacheState := CleanupStatusNone
	ownerCacheActive := ownerCache.HTTPRouterListen != "" && ownerCache.Token != "" && coord.Owns(ownerCache)
	if stale || (coord.Exists() && !ownerCacheActive) {
		cacheState = CleanupStatusNeeded
	}

	pac := CleanupSubjectStatusDetail{Subject: CleanupSubjectManagedPAC, State: CleanupStatusNone}
	if !runtimeActive {
		report, err := pacModule.InspectFootprint(ctx)
		if err != nil {
			pac.State = CleanupStatusUnknown
			pac.Diagnostic = err.Error()
		} else if report.State == managedpac.FootprintCleanupNeeded {
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
