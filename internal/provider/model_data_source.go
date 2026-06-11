package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*modelDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*modelDataSource)(nil)
)

type modelDataSource struct {
	client *httpClient
}

func newModelDataSource() datasource.DataSource {
	return &modelDataSource{}
}

type modelDataSourceModel struct {
	ModelID         types.String `tfsdk:"model_id"`
	ID              types.String `tfsdk:"id"`
	DisplayName     types.String `tfsdk:"display_name"`
	ProviderID      types.String `tfsdk:"provider_id"`
	ProviderModelID types.String `tfsdk:"provider_model_id"`
	DeploymentName  types.String `tfsdk:"deployment_name"`
	Capability      types.String `tfsdk:"capability"`
	ModelType       types.String `tfsdk:"model_type"`
	Enabled         types.Bool   `tfsdk:"enabled"`
	IsDefault       types.Bool   `tfsdk:"is_default"`
}

func (d *modelDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_model"
}

func (d *modelDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up an existing gateway model by its model_id.",
		Attributes: map[string]schema.Attribute{
			"model_id": schema.StringAttribute{
				Required:    true,
				Description: "The model id to look up.",
			},
			"id":                schema.StringAttribute{Computed: true, Description: "Server-assigned internal id."},
			"display_name":      schema.StringAttribute{Computed: true},
			"provider_id":       schema.StringAttribute{Computed: true},
			"provider_model_id": schema.StringAttribute{Computed: true},
			"deployment_name":   schema.StringAttribute{Computed: true},
			"capability":        schema.StringAttribute{Computed: true},
			"model_type":        schema.StringAttribute{Computed: true},
			"enabled":           schema.BoolAttribute{Computed: true},
			"is_default":        schema.BoolAttribute{Computed: true},
		},
	}
}

func (d *modelDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d.client = req.ProviderData.(*httpClient)
}

func (d *modelDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg modelDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out modelAPI
	if err := d.client.do(ctx, "GET", "/api/v1/admin/models/"+cfg.ModelID.ValueString(), nil, nil, &out); err != nil {
		resp.Diagnostics.AddError("Read model failed", err.Error())
		return
	}
	cfg.ID = types.StringValue(out.ID)
	cfg.ModelID = types.StringValue(out.ModelID)
	cfg.DisplayName = types.StringValue(out.DisplayName)
	cfg.ProviderID = types.StringValue(out.ProviderID)
	cfg.ProviderModelID = types.StringValue(out.ProviderModelID)
	cfg.DeploymentName = strOrNull(out.DeploymentName)
	cfg.Capability = types.StringValue(out.Capability)
	cfg.ModelType = types.StringValue(out.ModelType)
	cfg.Enabled = types.BoolValue(out.Enabled)
	cfg.IsDefault = types.BoolValue(out.IsDefault)
	resp.Diagnostics.Append(resp.State.Set(ctx, cfg)...)
}
