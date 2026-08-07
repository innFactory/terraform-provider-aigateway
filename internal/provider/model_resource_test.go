package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// managed_by is Optional (NOT Computed). When the config sets it and the gateway
// echoes it back, apply() must preserve the value exactly.
func TestModelApplyPreservesManagedBy(t *testing.T) {
	r := &modelResource{}
	m := &modelResourceModel{ManagedBy: types.StringValue("companygpt-terraform")}
	a := &modelAPI{ID: "model_x", ModelID: "gpt-5.4", ManagedBy: "companygpt-terraform", Enabled: true}

	r.apply(m, a)

	if m.ManagedBy.ValueString() != "companygpt-terraform" {
		t.Errorf("managed_by must round-trip, got %q", m.ManagedBy.ValueString())
	}
}

// managed_by is Optional-only: when unset in config its planned value is null
// (known, not unknown). The gateway returns an empty string. apply() must leave
// the model value untouched (null) and must NOT force it to unknown — otherwise
// we'd reproduce the api_version inconsistency bug class.
func TestModelApplyLeavesUnsetManagedByNull(t *testing.T) {
	r := &modelResource{}
	m := &modelResourceModel{ManagedBy: types.StringNull()} // unset in config => null plan value
	a := &modelAPI{ID: "model_x", ModelID: "gpt-5.4", ManagedBy: "", Enabled: true}

	r.apply(m, a)

	if m.ManagedBy.IsUnknown() {
		t.Fatal("managed_by must not become unknown after apply")
	}
	if !m.ManagedBy.IsNull() {
		t.Errorf("managed_by should stay null when unset and gateway returns none, got %q", m.ManagedBy.ValueString())
	}
}

// enabled/is_default are Optional+Computed → unset plans as UNKNOWN, not null.
// defBool must return the default in BOTH cases so models aren't created disabled.
func TestDefBoolDefaultsOnNullAndUnknown(t *testing.T) {
	if got := defBool(types.BoolNull(), true); got != true {
		t.Errorf("null => default true, got %v", got)
	}
	if got := defBool(types.BoolUnknown(), true); got != true {
		t.Errorf("unknown => default true, got %v", got)
	}
	if got := defBool(types.BoolValue(false), true); got != false {
		t.Errorf("explicit false must win over default true, got %v", got)
	}
	if got := defBool(types.BoolNull(), false); got != false {
		t.Errorf("null => default false, got %v", got)
	}
}

// ── by-id addressing (duplicate model names across providers) ───────────────
//
// Gateway >= v0.16.16 resolves /api/v1/admin/models/{model_id} doc-id-first
// and answers 409 when a bare NAME matches models under multiple providers.
// The resource therefore addresses by the server doc id whenever state knows
// it, falling back to the name only for legacy state without an id.

func TestModelAdminPathPrefersDocID(t *testing.T) {
	p, byName := modelAdminPath(types.StringValue("model_abc"), types.StringValue("gpt-4o"))
	if p != "/api/v1/admin/models/model_abc" || byName {
		t.Errorf("id set: got (%q, byName=%v), want doc-id path", p, byName)
	}
	for name, id := range map[string]types.String{
		"null":    types.StringNull(),
		"unknown": types.StringUnknown(),
		"empty":   types.StringValue(""),
	} {
		p, byName := modelAdminPath(id, types.StringValue("gpt-4o"))
		if p != "/api/v1/admin/models/gpt-4o" || !byName {
			t.Errorf("%s id: got (%q, byName=%v), want name fallback", name, p, byName)
		}
	}
}

func TestResolveModelImportID(t *testing.T) {
	list := []modelAPI{
		{ID: "model_a", ModelID: "claude-opus-4-7", ProviderID: "provider_tf"},
		{ID: "model_b", ModelID: "claude-opus-4-7", ProviderID: "provider_jena"},
		{ID: "model_c", ModelID: "gpt-4o", ProviderID: "provider_azure"},
	}
	if got, err := resolveModelImportID(list, "gpt-4o"); err != nil || got != "model_c" {
		t.Errorf("unique name: got (%q, %v), want model_c", got, err)
	}
	if _, err := resolveModelImportID(list, "claude-opus-4-7"); err == nil ||
		!strings.Contains(err.Error(), "ambiguous") ||
		!strings.Contains(err.Error(), "model_a") || !strings.Contains(err.Error(), "model_b") {
		t.Errorf("ambiguous name must error listing candidate doc ids, got %v", err)
	}
	if _, err := resolveModelImportID(list, "nope"); err == nil || !strings.Contains(err.Error(), "no model") {
		t.Errorf("missing name must error, got %v", err)
	}
}

// ── framework-level plumbing helpers ─────────────────────────────────────────

func modelTestSchema(t *testing.T) resource.SchemaResponse {
	t.Helper()
	var sr resource.SchemaResponse
	(&modelResource{}).Schema(context.Background(), resource.SchemaRequest{}, &sr)
	if sr.Diagnostics.HasError() {
		t.Fatalf("schema: %v", sr.Diagnostics)
	}
	return sr
}

func mustModelState(t *testing.T, m modelResourceModel) tfsdk.State {
	t.Helper()
	st := tfsdk.State{Schema: modelTestSchema(t).Schema}
	if d := st.Set(context.Background(), m); d.HasError() {
		t.Fatalf("state set: %v", d)
	}
	return st
}

func mustModelPlan(t *testing.T, m modelResourceModel) tfsdk.Plan {
	t.Helper()
	p := tfsdk.Plan{Schema: modelTestSchema(t).Schema}
	if d := p.Set(context.Background(), m); d.HasError() {
		t.Fatalf("plan set: %v", d)
	}
	return p
}

func modelTestServer(t *testing.T, handler http.HandlerFunc) *modelResource {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &modelResource{client: newClient(srv.URL, "test-key", "test")}
}

func echoModelHandler(gotMethod, gotPath *string, a modelAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*gotMethod, *gotPath = r.Method, r.URL.Path
		_ = json.NewEncoder(w).Encode(a)
	}
}

// ── Read / Update / Delete hit the id-based path when state has the doc id ──

func TestModelReadAddressesByDocID(t *testing.T) {
	var method, path string
	r := modelTestServer(t, echoModelHandler(&method, &path, modelAPI{
		ID: "model_abc", ModelID: "claude-opus-4-7", DisplayName: "Opus",
		ProviderID: "provider_tf", ProviderModelID: "claude-opus-4-7",
		Capability: "chat", ModelType: "chat", Enabled: true,
	}))
	st := mustModelState(t, modelResourceModel{
		ID:      types.StringValue("model_abc"),
		ModelID: types.StringValue("claude-opus-4-7"),
	})
	resp := &resource.ReadResponse{State: st}
	r.Read(context.Background(), resource.ReadRequest{State: st}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("read: %v", resp.Diagnostics)
	}
	if method != "GET" || path != "/api/v1/admin/models/model_abc" {
		t.Errorf("read hit %s %s, want GET /api/v1/admin/models/model_abc", method, path)
	}
}

func TestModelReadFallsBackToNameWithoutDocID(t *testing.T) {
	var method, path string
	r := modelTestServer(t, echoModelHandler(&method, &path, modelAPI{
		ID: "model_abc", ModelID: "claude-opus-4-7", DisplayName: "Opus",
		ProviderID: "provider_tf", ProviderModelID: "claude-opus-4-7",
		Capability: "chat", ModelType: "chat", Enabled: true,
	}))
	st := mustModelState(t, modelResourceModel{ModelID: types.StringValue("claude-opus-4-7")})
	resp := &resource.ReadResponse{State: st}
	r.Read(context.Background(), resource.ReadRequest{State: st}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("read: %v", resp.Diagnostics)
	}
	if path != "/api/v1/admin/models/claude-opus-4-7" {
		t.Errorf("read hit %s, want name-based path", path)
	}
	// The name-based read must backfill the doc id so the NEXT operation
	// addresses by id.
	var got modelResourceModel
	if d := resp.State.Get(context.Background(), &got); d.HasError() {
		t.Fatalf("state get: %v", d)
	}
	if got.ID.ValueString() != "model_abc" {
		t.Errorf("read must backfill id, got %q", got.ID.ValueString())
	}
}

func TestModelReadAmbiguousNameYieldsReimportDiagnostic(t *testing.T) {
	r := modelTestServer(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"detail": "Model name 'claude-opus-4-7' exists under multiple providers — address it by its doc id instead: model_a (provider provider_tf), model_b (provider provider_jena)",
		})
	})
	st := mustModelState(t, modelResourceModel{ModelID: types.StringValue("claude-opus-4-7")})
	resp := &resource.ReadResponse{State: st}
	r.Read(context.Background(), resource.ReadRequest{State: st}, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("ambiguous by-name read must error, not silently drop the resource")
	}
	detail := resp.Diagnostics.Errors()[0].Detail()
	if !strings.Contains(detail, "re-import") || !strings.Contains(detail, "model_<uuid>") {
		t.Errorf("409 diagnostic must tell the user to re-import by doc id, got %q", detail)
	}
}

func TestModelUpdateAddressesByDocID(t *testing.T) {
	var method, path string
	r := modelTestServer(t, echoModelHandler(&method, &path, modelAPI{
		ID: "model_abc", ModelID: "claude-opus-4-7", DisplayName: "Opus v2",
		ProviderID: "provider_tf", ProviderModelID: "claude-opus-4-7",
		Capability: "chat", ModelType: "chat", Enabled: true,
	}))
	m := modelResourceModel{
		ID:              types.StringValue("model_abc"),
		ModelID:         types.StringValue("claude-opus-4-7"),
		DisplayName:     types.StringValue("Opus v2"),
		ProviderID:      types.StringValue("provider_tf"),
		ProviderModelID: types.StringValue("claude-opus-4-7"),
	}
	resp := &resource.UpdateResponse{State: mustModelState(t, m)}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan:  mustModelPlan(t, m),
		State: mustModelState(t, m),
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}
	if method != "PUT" || path != "/api/v1/admin/models/model_abc" {
		t.Errorf("update hit %s %s, want PUT /api/v1/admin/models/model_abc", method, path)
	}
}

func TestModelDeleteAddressesByDocID(t *testing.T) {
	var method, path string
	r := modelTestServer(t, func(w http.ResponseWriter, req *http.Request) {
		method, path = req.Method, req.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	st := mustModelState(t, modelResourceModel{
		ID:      types.StringValue("model_abc"),
		ModelID: types.StringValue("claude-opus-4-7"),
	})
	resp := &resource.DeleteResponse{State: st}
	r.Delete(context.Background(), resource.DeleteRequest{State: st}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("delete: %v", resp.Diagnostics)
	}
	if method != "DELETE" || path != "/api/v1/admin/models/model_abc" {
		t.Errorf("delete hit %s %s, want DELETE /api/v1/admin/models/model_abc", method, path)
	}
}

// ── import: by doc id, by unique name, ambiguous name ────────────────────────

func TestModelImportByDocID(t *testing.T) {
	r := modelTestServer(t, func(w http.ResponseWriter, req *http.Request) {
		t.Errorf("import by doc id must not call the API, hit %s %s", req.Method, req.URL.Path)
	})
	resp := &resource.ImportStateResponse{State: mustModelState(t, modelResourceModel{})}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "model_abc"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("import: %v", resp.Diagnostics)
	}
	var got modelResourceModel
	if d := resp.State.Get(context.Background(), &got); d.HasError() {
		t.Fatalf("state get: %v", d)
	}
	if got.ID.ValueString() != "model_abc" {
		t.Errorf("id = %q, want model_abc", got.ID.ValueString())
	}
	if !got.ModelID.IsNull() {
		t.Errorf("model_id must stay null (filled by the follow-up Read), got %q", got.ModelID.ValueString())
	}
}

func TestModelImportByUniqueNameResolvesDocID(t *testing.T) {
	var path string
	r := modelTestServer(t, func(w http.ResponseWriter, req *http.Request) {
		path = req.URL.Path
		_ = json.NewEncoder(w).Encode([]modelAPI{
			{ID: "model_a", ModelID: "claude-opus-4-7", ProviderID: "provider_tf"},
			{ID: "model_c", ModelID: "gpt-4o", ProviderID: "provider_azure"},
		})
	})
	resp := &resource.ImportStateResponse{State: mustModelState(t, modelResourceModel{})}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "gpt-4o"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("import: %v", resp.Diagnostics)
	}
	if path != "/api/v1/admin/models" {
		t.Errorf("import resolved via %s, want the LIST endpoint", path)
	}
	var got modelResourceModel
	if d := resp.State.Get(context.Background(), &got); d.HasError() {
		t.Fatalf("state get: %v", d)
	}
	if got.ID.ValueString() != "model_c" || got.ModelID.ValueString() != "gpt-4o" {
		t.Errorf("import must record both handles, got id=%q model_id=%q",
			got.ID.ValueString(), got.ModelID.ValueString())
	}
}

func TestModelImportAmbiguousNameErrorsCleanly(t *testing.T) {
	r := modelTestServer(t, func(w http.ResponseWriter, req *http.Request) {
		_ = json.NewEncoder(w).Encode([]modelAPI{
			{ID: "model_a", ModelID: "claude-opus-4-7", ProviderID: "provider_tf"},
			{ID: "model_b", ModelID: "claude-opus-4-7", ProviderID: "provider_jena"},
		})
	})
	resp := &resource.ImportStateResponse{State: mustModelState(t, modelResourceModel{})}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "claude-opus-4-7"}, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("ambiguous name import must error")
	}
	detail := resp.Diagnostics.Errors()[0].Detail()
	if !strings.Contains(detail, "ambiguous") || !strings.Contains(detail, "model_a") {
		t.Errorf("error must call out ambiguity and candidate doc ids, got %q", detail)
	}
}

// ambiguousModelRefDetail (fallback_chain / deployment_group / data source):
// a 409 gains the doc-id hint, other errors pass through untouched.
func TestAmbiguousModelRefDetail(t *testing.T) {
	conflict := &apiError{Status: http.StatusConflict, Message: "exists under multiple providers"}
	if got := ambiguousModelRefDetail(conflict); !strings.Contains(got, "aigateway_model.<name>.id") {
		t.Errorf("409 must gain the doc-id hint, got %q", got)
	}
	plain := &apiError{Status: http.StatusBadRequest, Message: "bad"}
	if got := ambiguousModelRefDetail(plain); got != plain.Error() {
		t.Errorf("non-409 must pass through, got %q", got)
	}
}

// Version-skew guard: a by-doc-id read that 404s (gateway < v0.16.16 resolves
// names only) must retry by name instead of pruning the resource from state.
func TestModelReadDocID404FallsBackToName(t *testing.T) {
	var paths []string
	r := modelTestServer(t, func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.URL.Path)
		if strings.HasSuffix(req.URL.Path, "/model_abc") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(modelAPI{
			ID: "model_abc", ModelID: "gpt-5.1", DisplayName: "GPT 5.1",
			ProviderID: "provider_tf", ProviderModelID: "gpt-5.1",
			Capability: "chat", ModelType: "chat", Enabled: true,
		})
	})
	st := mustModelState(t, modelResourceModel{
		ID:      types.StringValue("model_abc"),
		ModelID: types.StringValue("gpt-5.1"),
	})
	resp := &resource.ReadResponse{State: st}
	r.Read(context.Background(), resource.ReadRequest{State: st}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("read: %v", resp.Diagnostics)
	}
	if len(paths) != 2 || paths[1] != "/api/v1/admin/models/gpt-5.1" {
		t.Errorf("expected id-read then name-fallback, got %v", paths)
	}
	var got modelResourceModel
	if d := resp.State.Get(context.Background(), &got); d.HasError() {
		t.Fatalf("state get: %v", d)
	}
	if got.ID.ValueString() != "model_abc" {
		t.Errorf("resource must stay in state with id backfilled, got %q", got.ID.ValueString())
	}
}

// Create on 409 adopts the existing (provider, model_id) doc and aligns it.
func TestModelCreateAdoptsExistingOnConflict(t *testing.T) {
	var putPath string
	r := modelTestServer(t, func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == "POST":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"Model already exists: gpt-5.1"}`))
		case req.Method == "GET":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]modelAPI{
				{ID: "model_other", ModelID: "gpt-5.1", ProviderID: "provider_OTHER"},
				{ID: "model_mine", ModelID: "gpt-5.1", ProviderID: "provider_tf"},
			})
		case req.Method == "PUT":
			putPath = req.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(modelAPI{
				ID: "model_mine", ModelID: "gpt-5.1", DisplayName: "gpt-5.1",
				ProviderID: "provider_tf", ProviderModelID: "gpt-5.1",
				Capability: "chat", ModelType: "chat", Enabled: true,
			})
		}
	})
	plan := mustModelPlan(t, modelResourceModel{
		ModelID:         types.StringValue("gpt-5.1"),
		DisplayName:     types.StringValue("gpt-5.1"),
		ProviderID:      types.StringValue("provider_tf"),
		ProviderModelID: types.StringValue("gpt-5.1"),
	})
	resp := &resource.CreateResponse{State: mustModelState(t, modelResourceModel{})}
	r.Create(context.Background(), resource.CreateRequest{Plan: plan}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("create should adopt on conflict: %v", resp.Diagnostics)
	}
	if putPath != "/api/v1/admin/models/model_mine" {
		t.Errorf("adoption must align the SAME-provider doc, PUT %q", putPath)
	}
	var got modelResourceModel
	if d := resp.State.Get(context.Background(), &got); d.HasError() {
		t.Fatalf("state get: %v", d)
	}
	if got.ID.ValueString() != "model_mine" {
		t.Errorf("adopted id, got %q", got.ID.ValueString())
	}
}

// A conflict whose existing doc belongs to ANOTHER provider is a real error.
func TestModelCreateConflictOtherProviderErrors(t *testing.T) {
	r := modelTestServer(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case "POST":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"Model already exists: gpt-5.1"}`))
		case "GET":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]modelAPI{
				{ID: "model_other", ModelID: "gpt-5.1", ProviderID: "provider_OTHER"},
			})
		}
	})
	plan := mustModelPlan(t, modelResourceModel{
		ModelID:         types.StringValue("gpt-5.1"),
		DisplayName:     types.StringValue("gpt-5.1"),
		ProviderID:      types.StringValue("provider_tf"),
		ProviderModelID: types.StringValue("gpt-5.1"),
	})
	resp := &resource.CreateResponse{State: mustModelState(t, modelResourceModel{})}
	r.Create(context.Background(), resource.CreateRequest{Plan: plan}, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("conflict owned by another provider must error, not adopt")
	}
}

// The gateway refuses to CREATE an enabled model with no input/output price
// (since v0.19.0): a $0 request bills nothing AND never moves a budget counter,
// so the model is invisible to every cap. `allow_unpriced` is the acknowledgement
// that unblocks the create-then-price workflow (terraform creates at 0, the
// update-pricing/confirm-pricing flow attaches the real ai-prices.eu rate after).
// It has to reach the wire as `allowUnpriced`, which is the exact name the
// gateway's CreateModelRequest deserializes.
func TestModelCreateBodySendsAllowUnpriced(t *testing.T) {
	body := modelCreateBody{
		ModelID:       "gpt-5.6-luna",
		Enabled:       true,
		AllowUnpriced: true,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["allowUnpriced"] != true {
		t.Errorf("allowUnpriced must be sent as true, got %v (payload: %s)", got["allowUnpriced"], raw)
	}
}

// Default is off: an unset allow_unpriced must not silently opt every model out
// of the price gate. `omitempty` drops the false, and the gateway's serde default
// is false — so the field is simply absent and the gate applies.
func TestModelCreateBodyOmitsAllowUnpricedWhenFalse(t *testing.T) {
	raw, err := json.Marshal(modelCreateBody{ModelID: "gpt-5.4", Enabled: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "allowUnpriced") {
		t.Errorf("an unset allow_unpriced must not appear on the wire, got: %s", raw)
	}
	if defBool(types.BoolNull(), false) {
		t.Error("null allow_unpriced must default to false")
	}
}

// allow_unpriced is a create-time acknowledgement, not model state: the gateway
// neither stores nor returns it. apply() must therefore leave it exactly as
// configured — nulling it here would produce a permanent diff on every refresh.
func TestModelApplyLeavesAllowUnpricedUntouched(t *testing.T) {
	r := &modelResource{}
	m := &modelResourceModel{AllowUnpriced: types.BoolValue(true)}
	a := &modelAPI{ID: "model_x", ModelID: "gpt-5.6-luna", Enabled: true}

	r.apply(m, a)

	if !m.AllowUnpriced.ValueBool() {
		t.Error("allow_unpriced must survive a read; the server never echoes it")
	}
}
