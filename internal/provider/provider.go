package provider

import (
	"context"
	"crypto/tls"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/opensearch-project/opensearch-go/v4"
	"github.com/opensearch-project/opensearch-go/v4/opensearchapi"
)

var _ provider.Provider = &OpenSearchProvider{}

type OpenSearchProvider struct {
	version string
}

// OpenSearchProviderModel describes the provider data model.
type OpenSearchProviderModel struct {
	Address  types.String `tfsdk:"address"`
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
	Insecure types.Bool   `tfsdk:"insecure"`
}

func (p *OpenSearchProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "opensearch"
	resp.Version = p.version
}

func (p *OpenSearchProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"address": schema.StringAttribute{
				MarkdownDescription: "The OpenSearch address",
				Required:            true,
			},
			"username": schema.StringAttribute{
				MarkdownDescription: "The OpenSearch username",
				Optional:            true,
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "The OpenSearch password",
				Optional:            true,
				Sensitive:           true,
			},
			"insecure": schema.BoolAttribute{
				MarkdownDescription: "Whether to skip TLS verification",
				Optional:            true,
			},
		},
	}
}

func (p *OpenSearchProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data OpenSearchProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	config := opensearch.Config{
		Addresses: []string{data.Address.ValueString()},
		Username:  data.Username.ValueString(),
		Password:  data.Password.ValueString(),
	}

	if data.Insecure.ValueBool() {
		config.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // For testing only. Use certificate for validation.
		}
	}

	apiconfig := opensearchapi.Config{
		Client: config,
	}

	resp.DataSourceData = apiconfig
	resp.ResourceData = apiconfig
}

func (p *OpenSearchProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewModelGroupResource,
		NewConnectorResource,
		NewModelRegisterResource,
	}
}

func (p *OpenSearchProvider) EphemeralResources(ctx context.Context) []func() ephemeral.EphemeralResource {
	return []func() ephemeral.EphemeralResource{}
}

func (p *OpenSearchProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func (p *OpenSearchProvider) Functions(ctx context.Context) []func() function.Function {
	return []func() function.Function{}
}

func NewOpenSearchProvider(version string) func() provider.Provider {
	return func() provider.Provider {
		return &OpenSearchProvider{
			version: version,
		}
	}
}
