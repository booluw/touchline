package matchsim

// emitFn is the signature of the engine's event sink.
type emitFn func(minute int, typ, clubID, desc, detail string)

// liveInputs indexes manager inputs for fast minute/club lookup during
// simulation (spec §2.7). Only the current EngineVersion ("1.2-approved")
// consumes substitution inputs numerically; tactic_change inputs are consumed
// by the orchestration layer's recording gate and replayed into the feed with
// no numeric effect in this block (tactical modulation is upstream).
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