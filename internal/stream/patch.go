package stream

import (
	"reflect"

	"github.com/meros/fyr/internal/core"
)

// MergePatchDiff returns the JSON Merge Patch (RFC 7396) that turns old into
// neu (SPEC 0.5 V0.38): changed and new keys with their value, removed keys
// as null, objects field by field, arrays whole. Both are decoded JSON.
func MergePatchDiff(old, neu map[string]any) map[string]any {
	p := map[string]any{}
	for k, nv := range neu {
		ov, had := old[k]
		if had && reflect.DeepEqual(ov, nv) {
			continue
		}
		om, oIsMap := ov.(map[string]any)
		nm, nIsMap := nv.(map[string]any)
		if had && oIsMap && nIsMap {
			p[k] = MergePatchDiff(om, nm)
			continue
		}
		p[k] = nv
	}
	for k := range old {
		if _, ok := neu[k]; !ok {
			p[k] = nil
		}
	}
	return p
}

// ApplyMergePatch applies a merge patch to a copy of target.
func ApplyMergePatch(target, patch map[string]any) map[string]any {
	out, _ := core.MergePatch(target, patch).(map[string]any)
	return out
}
