package provider

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ---------------------------------------------------------------------------
// parseJSONArray (shared helper — exercised via flow path)
// ---------------------------------------------------------------------------

// parseJSONArray must decode a valid JSON array and return []any.
func TestParseJSONArrayValid(t *testing.T) {
	input := `[{"type":"model","id":"m1"}]`
	steps, err := parseJSONArray(input)
	if err != nil {
		t.Fatalf("parseJSONArray: unexpected error: %v", err)
	}
	if len(steps) != 1 {
		t.Errorf("expected 1 step, got %d", len(steps))
	}
}

// parseJSONArray must return an error for non-JSON input.
func TestParseJSONArrayInvalid(t *testing.T) {
	_, err := parseJSONArray("not-json")
	if err == nil {
		t.Fatal("parseJSONArray: expected error for invalid JSON, got nil")
	}
}

// parseJSONArray must return an empty slice for an empty string.
func TestParseJSONArrayEmpty(t *testing.T) {
	steps, err := parseJSONArray("")
	if err != nil {
		t.Fatalf("parseJSONArray: unexpected error on empty input: %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("expected 0 steps for empty input, got %d", len(steps))
	}
}

// parseJSONArray must reject a JSON object (not an array).
func TestParseJSONArrayRejectsObject(t *testing.T) {
	_, err := parseJSONArray(`{"type":"model"}`)
	if err == nil {
		t.Fatal("parseJSONArray: expected error for JSON object (not array), got nil")
	}
}

// ---------------------------------------------------------------------------
// flowCreateBody marshalling
// ---------------------------------------------------------------------------

// The create body must marshal to the gateway's field names and transmit
// steps as a real JSON array (not a quoted string).
func TestFlowCreateBodyMarshalsFull(t *testing.T) {
	steps := []any{
		map[string]any{
			"type":       "model",
			"id":         "m1",
			"strategy":   map[string]any{"type": "failover"},
			"candidates": []any{map[string]any{"model_id": "uuid-1", "weight": 1, "priority": 1}},
			"next":       nil,
		},
	}
	body := flowCreateBody{
		Name:        "my-flow",
		Description: "desc",
		Steps:       steps,
		Entry:       "m1",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// steps must be a real array in the output JSON, not a string.
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	stepsField, ok := decoded["steps"]
	if !ok {
		t.Fatal("steps field missing from marshalled body")
	}
	if _, isArr := stepsField.([]any); !isArr {
		t.Errorf("steps must be a JSON array, got %T", stepsField)
	}
	if decoded["name"] != "my-flow" {
		t.Errorf("name: got %v", decoded["name"])
	}
	if decoded["entry"] != "m1" {
		t.Errorf("entry: got %v", decoded["entry"])
	}
}

// With empty steps, the steps field must be a JSON array (not null/absent).
func TestFlowCreateBodyEmptySteps(t *testing.T) {
	body := flowCreateBody{
		Name:  "minimal",
		Steps: []any{},
		Entry: "start",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Verify the exact output shape.
	got := string(raw)
	want := `{"name":"minimal","description":"","steps":[],"entry":"start"}`
	if got != want {
		t.Errorf("create body mismatch\n got: %s\nwant: %s", got, want)
	}
}

// ---------------------------------------------------------------------------
// flowResource.apply round-trip
// ---------------------------------------------------------------------------

// apply() must round-trip id, name, entry, steps from the API response.
func TestFlowApplyRoundTrips(t *testing.T) {
	r := &flowResource{}
	m := &flowResourceModel{}
	rawSteps := json.RawMessage(`[{"id":"m1","type":"model"}]`)
	a := &flowAPI{
		ID:    "flow_abc",
		Name:  "test-flow",
		Entry: "m1",
		Steps: rawSteps,
	}

	if err := r.apply(m, a); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if m.ID.ValueString() != "flow_abc" {
		t.Errorf("id: got %q want %q", m.ID.ValueString(), "flow_abc")
	}
	if m.Name.ValueString() != "test-flow" {
		t.Errorf("name: got %q want %q", m.Name.ValueString(), "test-flow")
	}
	if m.Entry.ValueString() != "m1" {
		t.Errorf("entry: got %q want %q", m.Entry.ValueString(), "m1")
	}
	// steps must be compact JSON
	wantSteps := `[{"id":"m1","type":"model"}]`
	if m.Steps.ValueString() != wantSteps {
		t.Errorf("steps: got %q want %q", m.Steps.ValueString(), wantSteps)
	}
}

// apply() must set steps to "[]" when the API returns an empty array.
func TestFlowApplyEmptySteps(t *testing.T) {
	r := &flowResource{}
	m := &flowResourceModel{}
	a := &flowAPI{
		ID:    "flow_empty",
		Name:  "empty",
		Entry: "s1",
		Steps: json.RawMessage(`[]`),
	}

	if err := r.apply(m, a); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if m.Steps.ValueString() != "[]" {
		t.Errorf("steps should be '[]' for empty, got %q", m.Steps.ValueString())
	}
}

// apply() must reflect a non-empty description and leave it unchanged when absent.
func TestFlowApplyDescription(t *testing.T) {
	r := &flowResource{}

	// With description present in response.
	mWith := &flowResourceModel{}
	if err := r.apply(mWith, &flowAPI{ID: "x", Name: "y", Entry: "e", Description: "some desc", Steps: json.RawMessage(`[]`)}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if mWith.Description.ValueString() != "some desc" {
		t.Errorf("description must round-trip, got %q", mWith.Description.ValueString())
	}

	// With empty description in response — field must not be overwritten.
	mEmpty := &flowResourceModel{Description: types.StringNull()}
	if err := r.apply(mEmpty, &flowAPI{ID: "x", Name: "y", Entry: "e", Description: "", Steps: json.RawMessage(`[]`)}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// When the server returns "" the model's description is left as-is (null).
	if !mEmpty.Description.IsNull() {
		t.Errorf("description must stay null when server returns empty string, got %q", mEmpty.Description.ValueString())
	}
}

// apply() normalises pretty-printed steps JSON to compact form.
func TestFlowApplyCompactsSteps(t *testing.T) {
	r := &flowResource{}
	m := &flowResourceModel{}
	rawSteps := json.RawMessage(`[
  {"type": "model", "id": "m1"}
]`)
	a := &flowAPI{ID: "x", Name: "y", Entry: "m1", Steps: rawSteps}

	if err := r.apply(m, a); err != nil {
		t.Fatalf("apply: %v", err)
	}
	want := `[{"id":"m1","type":"model"}]`
	if m.Steps.ValueString() != want {
		t.Errorf("steps compact mismatch\n got: %s\nwant: %s", m.Steps.ValueString(), want)
	}
}

// apply() must set steps to "[]" when the API returns JSON null (absent field).
func TestFlowApplyNullSteps(t *testing.T) {
	r := &flowResource{}
	m := &flowResourceModel{}
	a := &flowAPI{
		ID:    "flow_null",
		Name:  "null-steps",
		Entry: "s1",
		Steps: json.RawMessage(`null`),
	}
	if err := r.apply(m, a); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if m.Steps.ValueString() != "[]" {
		t.Errorf("steps should be '[]' for null, got %q", m.Steps.ValueString())
	}
}

// apply() must set steps to "[]" when the API returns a nil RawMessage (absent/zero field).
func TestFlowApplyNilSteps(t *testing.T) {
	r := &flowResource{}
	m := &flowResourceModel{}
	a := &flowAPI{
		ID:    "flow_nil",
		Name:  "nil-steps",
		Entry: "s1",
		Steps: nil,
	}
	if err := r.apply(m, a); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if m.Steps.ValueString() != "[]" {
		t.Errorf("steps should be '[]' for nil RawMessage, got %q", m.Steps.ValueString())
	}
}

// entry attribute is required — verify it round-trips correctly.
func TestFlowApplyEntryRequired(t *testing.T) {
	r := &flowResource{}
	m := &flowResourceModel{}
	a := &flowAPI{
		ID:    "flow_1",
		Name:  "flow",
		Entry: "step-1",
		Steps: json.RawMessage(`[]`),
	}
	if err := r.apply(m, a); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if m.Entry.ValueString() != "step-1" {
		t.Errorf("entry: got %q want %q", m.Entry.ValueString(), "step-1")
	}
	if m.Entry.IsNull() {
		t.Error("entry must not be null after apply")
	}
}
