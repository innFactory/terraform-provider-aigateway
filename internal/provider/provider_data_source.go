package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*providerDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*providerDataSource)(nil)
)

type providerDataSource struct {
	client *httpClient
}

func newProviderDataSource() datasource.DataSource {
	return &providerDataSource{}
}

type providerDataSourceModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	Type       types.String `tfsdk:"type"`
	Endpoint   types.String `tfsdk:"endpoint"`
	AuthType   types.String `tfsdk:"auth_type"`
	Region     types.String `tfsdk:"region"`
	ProjectID  types.String `tfsdk:"project_id"`
	APIVersion types.String `tfsdk:"api_version"`
	Enabled    types.Bool   `tfsdk:"enabled"`
}

func (d *providerDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_provider"
}

func (d *providerDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an existing gateway provider by id or name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Provider id. One of id or name is required.",
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Provider name. One of id or name is required.",
			},
			"type":        schema.StringAttribute{Computed: true},
			"endpoint":    schema.StringAttribute{Computed: true},
			"auth_type":   schema.StringAttribute{Computed: true},
			"region":      schema.StringAttribute{Computed: true},
			"project_id":  schema.StringAttribute{Computed: true},
			"api_version": schema.StringAttribute{Computed: true},
			"enabled":     schema.BoolAttribute{Computed: true},
		},
	}
}

func (d *providerDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d.client = req.ProviderData.(*httpClient)
}

func (d *providerDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg providerDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	wantID := cfg.ID.ValueString()
	wantName := cfg.Name.ValueString()
	if wantID == "" && wantName == "" {
		resp.Diagnostics.AddError("Missing lookup key", "Set either id or name on data.aigateway_provider.")
		return
	}

	var list []providerAPI
	if err := d.client.do(ctx, "GET", "/api/v1/admin/providers", nil, nil, &list); err != nil {
		resp.Diagnostics.AddError("List providers failed", err.Error())
		return
	}
	for i := range list {
		p := &list[i]
		if (wantID != "" && p.ID == wantID) || (wantID == "" && p.Name == wantName) {
			cfg.ID = types.StringValue(p.ID)
			cfg.Name = types.StringValue(p.Name)
			cfg.Type = types.StringValue(p.Type)
			cfg.Endpoint = types.StringValue(p.Endpoint)
			cfg.AuthType = types.StringValue(p.AuthType)
			cfg.Region = strOrNull(p.Region)
			cfg.ProjectID = strOrNull(p.ProjectID)
			cfg.APIVersion = strOrNull(p.APIVersion)
			cfg.Enabled = types.BoolValue(p.Enabled)
			resp.Diagnostics.Append(resp.State.Set(ctx, cfg)...)
			return
		}
	}
	key := wantID
	if key == "" {
		key = wantName
	}
	resp.Diagnostics.AddError("Provider not found", fmt.Sprintf("No gateway provider matched %q.", key))
}
