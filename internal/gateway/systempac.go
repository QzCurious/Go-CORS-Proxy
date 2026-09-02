package gateway

import "github.com/QzCurious/seamless-cors/internal/systempac"

func openSystemPAC() systempac.Module { return systempac.New() }

func systemPACReport(state systempac.State, err error) SystemPACReport {
	report := SystemPACReport{
		Generation:            state.Generation,
		Services:              make([]SystemPACServiceState, 0, len(state.Services)),
		RoutesCurrentEndpoint: state.RoutesCurrentEndpoint,
	}
	for _, service := range state.Services {
		report.Services = append(report.Services, SystemPACServiceState{
			Name: service.Name, URL: service.URL, Enabled: service.Enabled, Ownership: SystemPACOwnership(service.Ownership),
		})
	}
	walkErrors(err, func(item error) {
		issue := SystemPACIssue{Cause: item.Error()}
		switch typed := item.(type) {
		case systempac.DiscoveryError:
			issue.Kind = SystemPACIssueDiscovery
		case systempac.ObservationError:
			issue.Kind, issue.ServiceName = SystemPACIssueObservation, typed.ServiceName
		case systempac.MutationError:
			issue.Kind, issue.ServiceName = SystemPACIssueMutation, typed.ServiceName
		case systempac.VerificationError:
			issue.Kind, issue.ServiceName = SystemPACIssueVerification, typed.ServiceName
		case systempac.ResidueError:
			issue.Kind = SystemPACIssueResidue
		default:
			return
		}
		report.Issues = append(report.Issues, issue)
	})
	return report
}

func cleanupSystemPACReport(services []systempac.ServiceState, err error) SystemPACReport {
	return systemPACReport(systempac.State{Services: services}, err)
}

func walkErrors(err error, visit func(error)) {
	if err == nil {
		return
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			walkErrors(child, visit)
		}
		return
	}
	visit(err)
}
