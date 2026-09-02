package gateway

import (
	"context"
	"fmt"

	"github.com/QzCurious/seamless-cors/internal/systempac"
)

func cleanGatewayFootprint(ctx context.Context, pac systempac.Module, coord *coordinator, ownedCache *stateCache) (SystemPACReport, []CleanupFailure) {
	var failures []CleanupFailure
	services, err := pac.Cleanup(ctx)
	if err != nil {
		failures = append(failures, CleanupFailure{Subject: CleanupSubjectSystemPAC, Diagnostic: err.Error()})
	}
	return cleanupSystemPACReport(services, err), append(failures, cleanGatewayStateCache(coord, ownedCache)...)
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
	return []CleanupFailure{{Subject: CleanupSubjectGatewayStateCache, Diagnostic: fmt.Errorf("gateway state cache cleanup failed: %w", err).Error()}}
}

func inspectGatewayFootprint(coord *coordinator, stale bool, ownerCache stateCache, pac SystemPACReport) CleanupStatusDetail {
	cacheState := CleanupStatusNone
	ownerCacheActive := ownerCache.HTTPRouterListen != "" && ownerCache.Token != "" && coord.Owns(ownerCache)
	if stale || (coord.Exists() && !ownerCacheActive) {
		cacheState = CleanupStatusNeeded
	}
	pacState := CleanupStatusNone
	if len(pac.Issues) > 0 {
		pacState = CleanupStatusUnknown
	}
	for _, service := range pac.Services {
		if service.Enabled && service.Ownership == SystemPACOwnershipOwned {
			pacState = CleanupStatusNeeded
			break
		}
	}
	subjects := []CleanupSubjectStatusDetail{
		{Subject: CleanupSubjectGatewayStateCache, State: cacheState},
		{Subject: CleanupSubjectSystemPAC, State: pacState},
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
