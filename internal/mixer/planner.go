// Package mixer plans provider work from known identifiers and desired scopes.
// It does not fetch or merge data; newly discovered IDs can be fed back into a
// subsequent plan without teaching the planner about a specific provider.
package mixer

import "github.com/HeyaMedia/HeyaMetadata/internal/providers"

type Step struct {
	Collector  providers.Collector
	Identifier providers.Identifier
	Scopes     []providers.Scope
}

type Plan struct {
	Steps   []Step
	Missing []providers.Scope
}

type Planner struct{ collectors []providers.Collector }

func New(collectors ...providers.Collector) *Planner { return &Planner{collectors: collectors} }

func (p *Planner) Build(identifiers []providers.Identifier, desired []providers.Scope) Plan {
	return p.BuildAvailable(identifiers, desired, nil)
}

// BuildAvailable replans after earlier collectors discover identifiers. A
// completed provider is skipped, allowing supplemental collectors to contribute
// overlapping scopes instead of being hidden by the first source.
func (p *Planner) BuildAvailable(identifiers []providers.Identifier, desired []providers.Scope, completed map[string]bool) Plan {
	remaining := make(map[providers.Scope]bool, len(desired))
	for _, scope := range desired {
		remaining[scope] = true
	}
	result := Plan{}
	for _, collector := range p.collectors {
		capability := collector.Capability()
		if completed[capability.Provider] {
			continue
		}
		identifier, ok := acceptedIdentifier(capability, identifiers)
		if !ok {
			continue
		}
		var supplied []providers.Scope
		for _, scope := range capability.Provides {
			if remaining[scope] {
				supplied = append(supplied, scope)
				delete(remaining, scope)
			}
		}
		if len(supplied) > 0 {
			result.Steps = append(result.Steps, Step{Collector: collector, Identifier: identifier, Scopes: supplied})
		}
	}
	for _, scope := range desired {
		if remaining[scope] {
			result.Missing = append(result.Missing, scope)
		}
	}
	return result
}

func acceptedIdentifier(capability providers.Capability, available []providers.Identifier) (providers.Identifier, bool) {
	for _, accepted := range capability.AcceptedIdentifiers {
		for _, candidate := range available {
			if accepted.Provider == candidate.Provider && accepted.Namespace == candidate.Namespace {
				return candidate, true
			}
		}
	}
	return providers.Identifier{}, false
}
