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
var _ resource.Resource = &ModelGroupResource{}

// NewModelGroupResource is a helper function to simplify the provider implementation.
func NewModelGroupResource() resource.Resource {
	return &ModelGroupResource{}
}

// ModelGroupResource is the resource implementation.
type ModelGroupResource struct {
	config opensearchapi.Config
}

// ModelGroupModel describes the Model Register resource data model.
type ModelGroupModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

// Metadata returns the data source type name.
func (r *ModelGroupResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = fmt.Sprintf("%s_model_group", req.ProviderTypeName)
}

// Schema defines the schema for the Model Register resource.
func (r *ModelGroupResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Model group resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier for the model group.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable model name.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					// Registering again is the only supported “update”.
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the model group.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

// Configure prepares the OpenSearch client for data sources and resources.
func (r *ModelGroupResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func (r *ModelGroupResource) client() (*opensearchapi.Client, error) {
	return opensearchapi.NewClient(r.config)
}

// Create registers a new model in OpenSearch.
func (r *ModelGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ModelGroupModel

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

	request := skpropensearch.ModelGroupCreateRequest{
		Name:        data.Name.ValueString(),
		Description: data.Description.ValueString(),
	}

	requestBodyBytes, err := json.Marshal(request)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating model group request body",
			fmt.Sprintf("Could not create model group create request body: %s", err.Error()),
		)
		return
	}

	registerRequest, err := http.NewRequestWithContext(ctx, "POST", "/_plugins/_ml/model_groups/_register", bytes.NewReader(requestBodyBytes))
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating model group request",
			fmt.Sprintf("Could not create model group create request: %s", err.Error()),
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
		resp.Diagnostics.AddError("Error reading model group create response", err.Error())
		return
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		resp.Diagnostics.AddError(
			"Error registering model",
			fmt.Sprintf("OpenSearch returned %d: %s", response.StatusCode, string(body)),
		)
		return
	}

	var createResponse skpropensearch.ModelGroupCreateResponse

	if err := json.Unmarshal(body, &createResponse); err != nil {
		resp.Diagnostics.AddError(
			"Error parsing model group create response",
			fmt.Sprintf("Could not parse model group create response: %s", err.Error()),
		)
		return
	}

	data.ID = types.StringValue(createResponse.ModelGroupID)

	tflog.Trace(ctx, "created Model Group resource", map[string]any{
		"model_group_id": createResponse.ModelGroupID,
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read the resource state from OpenSearch for our model.
func (r *ModelGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ModelGroupModel

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

	getReq, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("/_plugins/_ml/model_groups/%s", data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating model get request", err.Error())
		return
	}

	getReq.Header.Set("Content-Type", "application/json")
	getReq.Header.Set("Accept", "application/json")

	httpResp, err := client.Client.Perform(getReq)
	if err != nil {
		resp.Diagnostics.AddError("Error reading model", err.Error())
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
func (r *ModelGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ModelGroupModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// All updatable fields are RequiresReplace, so Update should not be called for changes.
	// Still, if called (e.g. drift-only), just persist planned state.
	tflog.Trace(ctx, "updated Model Group resource (no-op update)", map[string]any{
		"model_group_id": data.ID.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete the model from OpenSearch.
func (r *ModelGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ModelGroupModel

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

	delReq, err := http.NewRequestWithContext(ctx, "DELETE", fmt.Sprintf("/_plugins/_ml/model_groups/%s", data.ID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating model group delete request", err.Error())
		return
	}

	delReq.Header.Set("Content-Type", "application/json")
	delReq.Header.Set("Accept", "application/json")

	httpResp, err := client.Client.Perform(delReq)
	if err != nil {
		resp.Diagnostics.AddError("Error deleting model group", err.Error())
		return
	}

	body, readErr := io.ReadAll(httpResp.Body)
	_ = httpResp.Body.Close()
	if readErr != nil {
		resp.Diagnostics.AddError("Error reading model group delete response", readErr.Error())
		return
	}

	// Treat 404 as already deleted.
	if httpResp.StatusCode == http.StatusNotFound {
		tflog.Trace(ctx, "model group already deleted", map[string]any{
			"model_group_id": data.ID.ValueString(),
		})
		return
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		resp.Diagnostics.AddError(
			"Error deleting model group",
			fmt.Sprintf("OpenSearch returned %d: %s", httpResp.StatusCode, string(body)),
		)
		return
	}

	tflog.Trace(ctx, "deleted Model Group resource", map[string]any{
		"model_group_id": data.ID.ValueString(),
	})
}
