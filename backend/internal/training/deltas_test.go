package training

import "testing"

func TestMoraleSwingNeedsBaseline(t *testing.T) {
	delta, write := moraleSwing(0, false, 0.8)
	if write || delta != 0 {
		t.Fatalf("no-baseline week must skip: delta=%d write=%v", delta, write)
	}
}

func TestMoraleSwingIsSignedInteger(t *testing.T) {
	delta, write := moraleSwing(50, true, 0.60)
	if !write || delta != 10 {
		t.Fatalf("expected +10, got delta=%d write=%v", delta, write)
	}
	delta, write = moraleSwing(60, true, 0.55)
	if !write || delta != -5 {
		t.Fatalf("expected -5, got delta=%d write=%v", delta, write)
	}
	delta, _ = moraleSwing(50, true, 0.5)
	if delta != 0 {
		t.Fatalf("zero swing must record a 0 delta: got %d", delta)
	}
}

func TestMoraleSwingClampsBand(t *testing.T) {
	delta, _ := moraleSwing(95, true, 1.3)
	if delta != 5 {
		t.Fatalf("out-of-range high morale clamped wrong: %d", delta)
	}
	delta, _ = moraleSwing(5, true, -0.3)
	if delta != -5 {
		t.Fatalf("out-of-range low morale clamped wrong: %d", delta)
	}
}

func TestMoraleDeltaKeyConstant(t *testing.T) {
	if MoraleDeltaKey != "morale" {
		t.Fatalf("unexpected pseudo-key: %q", MoraleDeltaKey)
	}
}
