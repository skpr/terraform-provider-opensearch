package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	opensearch "github.com/opensearch-project/opensearch-go/v2"
	opensearchapi "github.com/opensearch-project/opensearch-go/v2/opensearchapi"
	requestsigner "github.com/opensearch-project/opensearch-go/v2/signer/awsv2"
	"github.com/opensearch-project/opensearch-go/v4"
	"github.com/opensearch-project/opensearch-go/v4/opensearchapi"
)

var _ provider.Provider = &OpenSearchProvider{}

type OpenSearchProvider struct {
	version string
}

// OpenSearchProviderModel describes the provider data model.
type OpenSearchProviderModel struct {
	Address    types.String `tfsdk:"address"`
	Username   types.String `tfsdk:"username"`
	Password   types.String `tfsdk:"password"`
	Insecure   types.Bool   `tfsdk:"insecure"`
	UseSigV4   types.Bool   `tfsdk:"use_sig_v4"`
	Profile    types.String `tfsdk:"profile"`
	Region     types.String `tfsdk:"region"`
	AwsService types.String `tfsdk:"aws_service"`
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
			"use_sig_v4": schema.BoolAttribute{
				MarkdownDescription: "Whether to use AWS SigV4 signing for requests",
				Optional:            true,
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "The AWS region for SigV4 signing",
				Optional:            true,
			},
			"profile": schema.StringAttribute{
				MarkdownDescription: "The AWS profile to use from the shared credentials file",
				Optional:            true,
			},
			"aws_service": schema.StringAttribute{
				MarkdownDescription: "The AWS service name for SigV4 signing (e.g., 'es' for OpenSearch Service, 'aoss' for OpenSearch Serverless)",
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

	if !data.UseSigV4.ValueBool() {
		config.Username = data.Username.ValueString()
		config.Password = data.Password.ValueString()
	} else {
		var (
			awsConfig aws.Config
			err       error
		)

		if data.Profile.IsNull() {
			awsConfig, err = config.LoadDefaultConfig(ctx)
		} else {
			awsConfig, err = config.LoadDefaultConfig(ctx, config.WithSharedConfigProfile(data.Profile.ValueString()))
		}

		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to get AWS config, got error: %s", err))
			return
		}

		// Service name:
		// - "es"  for Amazon OpenSearch Service domains
		// - "aoss" for Amazon OpenSearch Serverless
		service := data.AwsService.ValueString()
		if service == "" {
			service = "es"
		}

		signer, err := requestsigner.NewSignerWithService(awsConfig, service)
		if err != nil {
			resp.Diagnostics.AddError("Unable to create SigV4 signer", err.Error())
			return
		}

		config.Signer = signer
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
