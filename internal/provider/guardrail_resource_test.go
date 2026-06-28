package provider

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// parseRulesJSON must decode a valid JSON array and return []any.
func TestParseRulesJSONValid(t *testing.T) {
	input := `[{"type":"prompt_injection","action":"block"}]`
	rules, err := parseRulesJSON(input)
	if err != nil {
		t.Fatalf("parseRulesJSON: unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rules))
	}
}

// parseRulesJSON must return an error for non-JSON input.
func TestParseRulesJSONInvalid(t *testing.T) {
	_, err := parseRulesJSON("not-json")
	if err == nil {
		t.Fatal("parseRulesJSON: expected error for invalid JSON, got nil")
	}
}

// parseRulesJSON must return an empty slice for an empty string.
func TestParseRulesJSONEmpty(t *testing.T) {
	rules, err := parseRulesJSON("")
	if err != nil {
		t.Fatalf("parseRulesJSON: unexpected error on empty input: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules for empty input, got %d", len(rules))
	}
}

// parseRulesJSON must reject a JSON object (not an array).
func TestParseRulesJSONRejectsObject(t *testing.T) {
	_, err := parseRulesJSON(`{"type":"prompt_injection"}`)
	if err == nil {
		t.Fatal("parseRulesJSON: expected error for JSON object (not array), got nil")
	}
}

// The create body must marshal to the gateway's camelCase shape and transmit
// rules as a real JSON array (not a quoted string).
func TestGuardrailCreateBodyMarshalsFull(t *testing.T) {
	desc := "blocks injection"
	rules := []any{map[string]any{"type": "prompt_injection", "action": "block"}}
	body := guardrailCreateBody{
		Name:        "no-injection",
		Description: &desc,
		Enabled:     true,
		Rules:       rules,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"name":"no-injection","description":"blocks injection","enabled":true,"rules":[{"action":"block","type":"prompt_injection"}]}`
	if got != want {
		t.Errorf("create body mismatch\n got: %s\nwant: %s", got, want)
	}
}

// With only the required fields set, description must be omitted.
func TestGuardrailCreateBodyOmitsOptionalDescription(t *testing.T) {
	body := guardrailCreateBody{
		Name:    "minimal",
		Enabled: true,
		Rules:   []any{},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"name":"minimal","enabled":true,"rules":[]}`
	if got != want {
		t.Errorf("create body mismatch\n got: %s\nwant: %s", got, want)
	}
}

// apply() must round-trip id, name, enabled, and rules from the API response.
func TestGuardrailApplyRoundTrips(t *testing.T) {
	r := &guardrailResource{}
	m := &guardrailResourceModel{}
	rawRules := json.RawMessage(`[{"type":"prompt_injection","action":"block"}]`)
	a := &guardrailAPI{
		ID:      "policy_abc",
		Name:    "no-injection",
		Enabled: true,
		Rules:   rawRules,
	}

	if err := r.apply(m, a); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if m.ID.ValueString() != "policy_abc" {
		t.Errorf("id: got %q want %q", m.ID.ValueString(), "policy_abc")
	}
	if m.Name.ValueString() != "no-injection" {
		t.Errorf("name: got %q want %q", m.Name.ValueString(), "no-injection")
	}
	if !m.Enabled.ValueBool() {
		t.Error("enabled must be true")
	}
	if m.Rules.ValueString() != `[{"action":"block","type":"prompt_injection"}]` {
		t.Errorf("rules: got %q", m.Rules.ValueString())
	}
}

// apply() must set rules to "[]" when the API returns an empty array.
func TestGuardrailApplyEmptyRules(t *testing.T) {
	r := &guardrailResource{}
	m := &guardrailResourceModel{}
	a := &guardrailAPI{
		ID:      "policy_empty",
		Name:    "empty",
		Enabled: false,
		Rules:   json.RawMessage(`[]`),
	}

	if err := r.apply(m, a); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if m.Rules.ValueString() != "[]" {
		t.Errorf("rules should be '[]' for empty, got %q", m.Rules.ValueString())
	}
}

// apply() must set rules to "[]" when the API returns JSON null (absent field).
func TestGuardrailApplyNullRules(t *testing.T) {
	r := &guardrailResource{}
	m := &guardrailResourceModel{}
	a := &guardrailAPI{
		ID:      "policy_null",
		Name:    "null-rules",
		Enabled: true,
		Rules:   json.RawMessage(`null`),
	}
	if err := r.apply(m, a); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if m.Rules.ValueString() != "[]" {
		t.Errorf("rules should be '[]' for null, got %q", m.Rules.ValueString())
	}
}

// apply() must set rules to "[]" when the API returns a nil RawMessage (absent/zero field).
func TestGuardrailApplyNilRules(t *testing.T) {
	r := &guardrailResource{}
	m := &guardrailResourceModel{}
	a := &guardrailAPI{
		ID:      "policy_nil",
		Name:    "nil-rules",
		Enabled: true,
		Rules:   nil,
	}
	if err := r.apply(m, a); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if m.Rules.ValueString() != "[]" {
		t.Errorf("rules should be '[]' for nil RawMessage, got %q", m.Rules.ValueString())
	}
}

// apply() must reflect a non-empty description and leave it null when absent.
func TestGuardrailApplyDescription(t *testing.T) {
	r := &guardrailResource{}

	// With description present in response.
	desc := "some desc"
	mWith := &guardrailResourceModel{}
	if err := r.apply(mWith, &guardrailAPI{ID: "x", Name: "y", Description: &desc, Rules: json.RawMessage(`[]`)}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if mWith.Description.ValueString() != "some desc" {
		t.Errorf("description must round-trip, got %q", mWith.Description.ValueString())
	}

	// With description absent (nil) in response — config had null, must stay null.
	mNil := &guardrailResourceModel{Description: types.StringNull()}
	if err := r.apply(mNil, &guardrailAPI{ID: "x", Name: "y", Description: nil, Rules: json.RawMessage(`[]`)}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !mNil.Description.IsNull() {
		t.Errorf("description must stay null when unset and server returns nil, got %q", mNil.Description.ValueString())
	}
}

// defBool (shared helper) is used for enabled; verify it defaults to true
// for null/unknown — same contract as the model resource.
func TestGuardrailDefBoolEnabled(t *testing.T) {
	if got := defBool(types.BoolNull(), true); got != true {
		t.Errorf("null => default true, got %v", got)
	}
	if got := defBool(types.BoolValue(false), true); got != false {
		t.Errorf("explicit false must win over default true, got %v", got)
	}
}

// compactJSON normalises pretty-printed JSON to a compact single-line string
// for stable state storage.
func TestCompactJSON(t *testing.T) {
	raw := json.RawMessage(`[
  {"type": "prompt_injection", "action": "block"}
]`)
	got, err := compactJSON(raw)
	if err != nil {
		t.Fatalf("compactJSON: %v", err)
	}
	want := `[{"action":"block","type":"prompt_injection"}]`
	if got != want {
		t.Errorf("compactJSON mismatch\n got: %s\nwant: %s", got, want)
	}
}

// compactJSON must return "[]" for nil, empty, and literal JSON null inputs.
func TestCompactJSONNullAndNil(t *testing.T) {
	cases := []struct {
		name string
		raw  json.RawMessage
	}{
		{"nil", nil},
		{"empty", json.RawMessage{}},
		{"json_null", json.RawMessage(`null`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := compactJSON(tc.raw)
			if err != nil {
				t.Fatalf("compactJSON(%s): unexpected error: %v", tc.name, err)
			}
			if got != "[]" {
				t.Errorf("compactJSON(%s): got %q, want \"[]\"", tc.name, got)
			}
		})
	}
}
