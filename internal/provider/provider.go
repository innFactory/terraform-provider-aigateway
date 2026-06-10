package provider

import (
	"context"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = (*aigatewayProvider)(nil)

type aigatewayProvider struct {
	version string
}

// New returns the provider factory used by main.go.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &aigatewayProvider{version: version}
	}
}

func (p *aigatewayProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "aigateway"
	resp.Version = p.version
}

type providerModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	AdminKey types.String `tfsdk:"admin_api_key"`
}

func (p *aigatewayProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Configures an innFactory AI Gateway — manage providers, models, API keys, " +
			"budgets and tenant settings declaratively. Talks to the gateway admin API " +
			"using the full-admin API key (GATEWAY_ADMIN_API_KEY).",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional: true,
				Description: "Base URL of the gateway, e.g. https://innfactory26.aigateway.agentic-web.eu. " +
					"Override via env AIGATEWAY_ENDPOINT.",
			},
			"admin_api_key": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				Description: "Full-admin API key (matches GATEWAY_ADMIN_API_KEY on the gateway). " +
					"Override via env AIGATEWAY_ADMIN_API_KEY (recommended).",
			},
		},
	}
}

func (p *aigatewayProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := strFromAttrOrEnv(data.Endpoint, "AIGATEWAY_ENDPOINT", "")
	adminKey := strFromAttrOrEnv(data.AdminKey, "AIGATEWAY_ADMIN_API_KEY", "")

	if endpoint == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("endpoint"),
			"Missing endpoint",
			"Set provider.endpoint or export AIGATEWAY_ENDPOINT (the gateway base URL).",
		)
	}
	if adminKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("admin_api_key"),
			"Missing admin API key",
			"Set provider.admin_api_key or export AIGATEWAY_ADMIN_API_KEY (matches GATEWAY_ADMIN_API_KEY on the gateway).",
		)
	}
	// Refuse to send the bearer to a non-HTTPS endpoint unless explicitly
	// allowed (local dev / in-cluster http).
	insecure := os.Getenv("AIGATEWAY_INSECURE_ENDPOINT") == "1"
	if endpoint != "" && !insecure && !strings.HasPrefix(endpoint, "https://") {
		resp.Diagnostics.AddAttributeError(
			path.Root("endpoint"),
			"Refusing to send admin key over non-HTTPS",
			"endpoint must be https:// (set AIGATEWAY_INSECURE_ENDPOINT=1 to allow http for local/in-cluster use).",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint = strings.TrimRight(endpoint, "/")
	client := newClient(endpoint, adminKey, p.version)
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *aigatewayProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		newProviderResource,
		newModelResource,
		newAPIKeyResource,
		newTenantSettingsResource,
	}
}

func (p *aigatewayProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func strFromAttrOrEnv(v types.String, envKey, fallback string) string {
	if !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
		return v.ValueString()
	}
	if env := os.Getenv(envKey); env != "" {
		return env
	}
	return fallback
}
