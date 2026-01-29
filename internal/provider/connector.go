package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/opensearch-project/opensearch-go/v4/opensearchapi"

	skpropensearch "github.com/skpr/terraform-provider-opensearch/internal/opensearch"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &ConnectorResource{}

// NewConnectorResource is a helper function to simplify the provider implementation.
func NewConnectorResource() resource.Resource {
	return &ConnectorResource{}
}

// ConnectorResource is the resource implementation.
type ConnectorResource struct {
	config opensearchapi.Config
}

// ConnectorModel describes the Model Register resource data model.
type ConnectorModel struct {
	ID   types.String `tfsdk:"id"`
	Body types.String `tfsdk:"body"`
}

// Metadata returns the data source type name.
func (r *ConnectorResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = fmt.Sprintf("%s_connector", req.ProviderTypeName)
}

// Schema defines the schema for the Model Register resource.
func (r *ConnectorResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Connector resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier for the connector.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"body": schema.StringAttribute{
				MarkdownDescription: "A JSON payload which defines the connector configuration.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					// Registering again is the only supported “update”.
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

// Configure prepares the OpenSearch client for data sources and resources.
func (r *ConnectorResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	opensearchConfig, ok := req.ProviderData.(opensearchapi.Config)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected opensearchapi.Config, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.config = opensearchConfig
}

// Returns a configured OpenSearch client.
// https://github.com/opensearch-project/opensearch-go/blob/main/_samples/json.go
func (r *ConnectorResource) client() (*opensearchapi.Client, error) {
	return opensearchapi.NewClient(r.config)
}

// Create registers a new model in OpenSearch.
func (r *ConnectorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ConnectorModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client, err := r.client()
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating OpenSearch client",
			fmt.Sprintf("Could not create OpenSearch client: %s", err.Error()),
		)
		return
	}

	registerRequest, err := http.NewRequestWithContext(ctx, "POST", "/_plugins/_ml/connectors/_create", bytes.NewReader([]byte(data.Body.ValueString())))
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating connector request",
			fmt.Sprintf("Could not create connector create request: %s", err.Error()),
		)
		return
	}

	registerRequest.Header.Set("Content-Type", "application/json")
	registerRequest.Header.Set("Accept", "application/json")

	response, err := client.Client.Perform(registerRequest)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error registering model",
			fmt.Sprintf("Could not register model: %s", err.Error()),
		)
		return
	}

	// Decode then close explicitly (don’t defer inside loops; this is fine here).
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		resp.Diagnostics.AddError("Error reading connector create response", err.Error())
		return
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		resp.Diagnostics.AddError(
			"Error registering connector",
			fmt.Sprintf("OpenSearch returned %d: %s", response.StatusCode, string(body)),
		)
		return
	}

	var createResponse skpropensearch.ConnectorCreateResponse

	if err := json.Unmarshal(body, &createResponse); err != nil {
		resp.Diagnostics.AddError(
			"Error parsing connector create response",
			fmt.Sprintf("Could not parse connector create response: %s", err.Error()),
		)
		return
	}

	data.ID = types.StringValue(createResponse.ConnectorID)

	tflog.Trace(ctx, "created Connector resource", map[string]any{
		"connector_id": createResponse.ConnectorID,
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read the resource state from OpenSearch for our model.
func (r *ConnectorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ConnectorModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// If we don’t have an ID, nothing to read.
	if data.ID.IsNull() || data.ID.IsUnknown() || data.ID.ValueString() == "" {
		resp.State.RemoveResource(ctx)
		return
	}

	client, err := r.client()
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating OpenSearch client",
			fmt.Sprintf("Could not create OpenSearch client: %s", err.Error()),
		)
		return
	}

	getReq, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("/_plugins/_ml/connectors/%s", data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating connector get request", err.Error())
		return
	}

	getReq.Header.Set("Content-Type", "application/json")
	getReq.Header.Set("Accept", "application/json")

	httpResp, err := client.Client.Perform(getReq)
	if err != nil {
		resp.Diagnostics.AddError("Error reading connector", err.Error())
		return
	}

	// If it’s gone, tell Terraform to drop it from state.
	if httpResp.StatusCode == http.StatusNotFound {
		resp.State.RemoveResource(ctx)
		return
	}

	body, err := io.ReadAll(httpResp.Body)
	_ = httpResp.Body.Close()
	if err != nil {
		resp.Diagnostics.AddError("Error reading model get response", err.Error())
		return
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		resp.Diagnostics.AddError(
			"Error reading model",
			fmt.Sprintf("OpenSearch returned %d: %s", httpResp.StatusCode, string(body)),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update is not supported; registering a new model is the only way to change anything.
func (r *ConnectorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ConnectorModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// All updatable fields are RequiresReplace, so Update should not be called for changes.
	// Still, if called (e.g. drift-only), just persist planned state.
	tflog.Trace(ctx, "updated Connector resource (no-op update)", map[string]any{
		"connector_id": data.ID.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete the model from OpenSearch.
func (r *ConnectorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ConnectorModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Nothing to delete if missing ID.
	if data.ID.IsNull() || data.ID.IsUnknown() || data.ID.ValueString() == "" {
		return
	}

	client, err := r.client()
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating OpenSearch client",
			fmt.Sprintf("Could not create OpenSearch client: %s", err.Error()),
		)
		return
	}

	delReq, err := http.NewRequestWithContext(ctx, "DELETE", fmt.Sprintf("/_plugins/_ml/connectors/%s", data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating connector delete request", err.Error())
		return
	}

	delReq.Header.Set("Content-Type", "application/json")
	delReq.Header.Set("Accept", "application/json")

	httpResp, err := client.Client.Perform(delReq)
	if err != nil {
		resp.Diagnostics.AddError("Error deleting connector", err.Error())
		return
	}

	body, readErr := io.ReadAll(httpResp.Body)
	_ = httpResp.Body.Close()
	if readErr != nil {
		resp.Diagnostics.AddError("Error reading connector delete response", readErr.Error())
		return
	}

	// Treat 404 as already deleted.
	if httpResp.StatusCode == http.StatusNotFound {
		tflog.Trace(ctx, "connector already deleted", map[string]any{
			"connector_id": data.ID.ValueString(),
		})
		return
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		resp.Diagnostics.AddError(
			"Error deleting connector",
			fmt.Sprintf("OpenSearch returned %d: %s", httpResp.StatusCode, string(body)),
		)
		return
	}

	tflog.Trace(ctx, "deleted Connector resource", map[string]any{
		"connector_id": data.ID.ValueString(),
	})
}
