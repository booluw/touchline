package explanation

import (
	"encoding/json"
	"testing"
)

// sample returns the representative explanation used across the wire tests.
func sample() *Explanation {
	return New("board_confidence", -14).
		Add("Wage bill 18% above structure", -12).
		Add("Failed to qualify for Europe", -2)
}

// TestWireFormat pins the JSON contract. The key order and the fact that every
// field is always emitted are part of the stable wire shape; a change here is a
// versioned contract change, not an ordinary edit.
func TestWireFormat(t *testing.T) {
	got, err := json.Marshal(sample())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"subject":"board_confidence","score":-14,"factors":[{"label":"Wage bill 18% above structure","delta":-12},{"label":"Failed to qualify for Europe","delta":-2}]}`
	if string(got) != want {
		t.Fatalf("wire mismatch:\n got %s\nwant %s", got, want)
	}
}

func TestRoundTrip(t *testing.T) {
	in := sample()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out Explanation
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if out.Subject != in.Subject || out.Score != in.Score || len(out.Factors) != len(in.Factors) {
		t.Fatalf("round-trip mismatch: got %+v want %+v", out, in)
	}
	for i := range in.Factors {
		if out.Factors[i] != in.Factors[i] {
			t.Fatalf("factor %d mismatch: got %+v want %+v", i, out.Factors[i], in.Factors[i])
		}
	}
}

// TestEmbeddedInEventPayload proves the Explanation type is serializable inside
// an event payload struct (technical plan section 8: world.events.payload can
// carry the explanation alongside domain data).
func TestEmbeddedInEventPayload(t *testing.T) {
	type payload struct {
		Home, Away int
		Reason     Explanation `json:"reason"`
	}
	in := &payload{Home: 3, Away: 1, Reason: *sample()}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out payload
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Reason.Subject != in.Reason.Subject || out.Reason.Score != in.Reason.Score ||
		len(out.Reason.Factors) != len(in.Reason.Factors) {
		t.Fatalf("embedded explanation mismatch: got %+v want %+v", out.Reason, in.Reason)
	}
}

// TestEmbeddedInAPIResponse proves the Explanation type is serializable in a
// state-changing API response body, so clients receive the reason directly and
// never have to derive it.
func TestEmbeddedInAPIResponse(t *testing.T) {
	type response struct {
		WorldID     string       `json:"world_id"`
		Changed     bool         `json:"changed"`
		Explanation *Explanation `json:"explanation"`
	}
	in := &response{WorldID: "w-1", Changed: true, Explanation: sample()}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out response
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Explanation == nil || out.Explanation.Subject != in.Explanation.Subject {
		t.Fatalf("response explanation mismatch: got %+v want %+v", out.Explanation, in.Explanation)
	}
}

func TestAddPreservesOrder(t *testing.T) {
	e := New("transfer_desire", 0)
	e.Add("Playing time", -18)
	e.Add("Broken promise", -12)
	e.Add("Club ambition", -8)
	if len(e.Factors) != 3 {
		t.Fatalf("want 3 factors, got %d", len(e.Factors))
	}
	if e.Factors[0].Label != "Playing time" || e.Factors[2].Label != "Club ambition" {
		t.Fatalf("order not preserved: %+v", e.Factors)
	}
}

func TestEmptyExplanation(t *testing.T) {
	e := New("news_prompt", 0)
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Stable shape: factors always emitted, even when empty.
	want := `{"subject":"news_prompt","score":0,"factors":[]}`
	if string(data) != want {
		t.Fatalf("empty wire mismatch: got %s want %s", data, want)
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("empty explanation should validate: %v", err)
	}
}

func TestValidate(t *testing.T) {
	exact := New("board_confidence", -14).Add("Wage bill", -12).Add("Europe", -2)
	if err := exact.Validate(); err != nil {
		t.Fatalf("matching sum should validate: %v", err)
	}

	broken := New("board_confidence", -14).Add("Wage bill", -12).Add("Europe", -1)
	if err := broken.Validate(); err == nil {
		t.Fatal("mismatched sum should fail validation")
	}

	// Narrative-only explanations (PRD section 54 AI-bid case) carry no score
	// and still validate.
	prose := New("ai_bid", 0).Add("Their starting striker is injured for 4 months", 0)
	if err := prose.Validate(); err != nil {
		t.Fatalf("prose explanation should validate: %v", err)
	}
}

func TestRender(t *testing.T) {
	e := New("transfer_desire", -44).
		Add("Playing time", -18).
		Add("Broken promise", -12).
		Add("Club ambition", -8).
		Add("Relationship with manager", -6)

	want := []string{
		"Playing time: -18",
		"Broken promise: -12",
		"Club ambition: -8",
		"Relationship with manager: -6",
	}
	got := e.Render()
	if len(got) != len(want) {
		t.Fatalf("render length: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("render line %d: got %q want %q", i, got[i], want[i])
		}
	}
}
