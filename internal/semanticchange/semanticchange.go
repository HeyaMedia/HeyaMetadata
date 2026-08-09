// Package semanticchange compares public canonical projections while ignoring
// operational freshness/provenance churn that must not invalidate consumers.
package semanticchange

import (
	"encoding/json"
	"reflect"
)

var volatileKeys = map[string]struct{}{
	"projection_version":    {},
	"freshness":             {},
	"observation_id":        {},
	"source_observation_id": {},
	"last_observation_id":   {},
	"observed_at":           {},
}

// Equal reports whether two JSON projections carry the same user-visible
// metadata. Freshness and observation lineage are deliberately ignored: they
// are persisted operationally, but refreshing them alone must not wake every
// downstream media server.
func Equal(left, right []byte) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	var l, r any
	if json.Unmarshal(left, &l) != nil || json.Unmarshal(right, &r) != nil {
		return false
	}
	return reflect.DeepEqual(strip(l), strip(r))
}

func strip(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if _, volatile := volatileKeys[key]; volatile {
				continue
			}
			out[key] = strip(child)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, child := range typed {
			out[index] = strip(child)
		}
		return out
	default:
		return value
	}
}
