package matchsim

// emitFn is the signature of the engine's event sink.
type emitFn func(minute int, typ, clubID, desc, detail string)

// liveInputs indexes manager inputs for fast minute/club lookup during
// simulation (spec §2.7). Substitutions replace the random sub draw at the
// windows; tactic_change inputs switch the side's style from their minute
// (v1.5) — both are consumed inside the engine with no RNG.
type liveInputs map[int]map[string]LiveInput

// indexLiveInputs builds the lookup index, discarding out-of-domain minutes.
func indexLiveInputs(inputs []LiveInput) liveInputs {
	idx := make(liveInputs, 90)
	for _, in := range inputs {
		if in.Minute < 1 || in.Minute > 90 {
			continue
		}
		if idx[in.Minute] == nil {
			idx[in.Minute] = make(map[string]LiveInput)
		}
		idx[in.Minute][in.ClubID] = in
	}
	return idx
}

// substitutionAt returns the manager substitution for a minute/club, or nil.
func (idx liveInputs) substitutionAt(minute int, clubID string) *LiveInput {
	if byMinute := idx[minute]; byMinute != nil {
		if in, ok := byMinute[clubID]; ok && in.Kind == "substitution" {
			return &in
		}
	}
	return nil
}

// tacticAt returns the manager tactic_change for a minute/club, or nil (v1.5:
// these now carry a numeric effect, consumed by the engine at the input's
// minute).
func (idx liveInputs) tacticAt(minute int, clubID string) *LiveInput {
	if byMinute := idx[minute]; byMinute != nil {
		if in, ok := byMinute[clubID]; ok && in.Kind == "tactic_change" {
			return &in
		}
	}
	return nil
}

// TacticStyle extracts the target style key from a tactic_change input's
// Detail map ("style"), returning "" when absent/malformed so the caller's
// DefaultStyle fallback applies.
func (in LiveInput) TacticStyle() string {
	if in.Detail == nil {
		return ""
	}
	s, _ := in.Detail["style"].(string)
	return s
}
