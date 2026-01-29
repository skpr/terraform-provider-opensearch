package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

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
var _ resource.Resource = &ModelRegisterResource{}

// NewModelRegisterResource is a helper function to simplify the provider implementation.
func NewModelRegisterResource() resource.Resource {
	return &ModelRegisterResource{}
}

// ModelRegisterResource is the resource implementation.
type ModelRegisterResource struct {
	config opensearchapi.Config
}

// ModelRegisterModel describes the Model Register resource data model.
type ModelRegisterModel struct {
	ModelID types.String `tfsdk:"model_id"`
	Body    types.String `tfsdk:"body"`
}

// Metadata returns the data source type name.
func (r *ModelRegisterResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = fmt.Sprintf("%s_model_register", req.ProviderTypeName)
}

// Schema defines the schema for the Model Register resource.
func (r *ModelRegisterResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Model registration resource",

		Attributes: map[string]schema.Attribute{
			"model_id": schema.StringAttribute{
				MarkdownDescription: "Model ID returned by OpenSearch after registration completes.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"body": schema.StringAttribute{
				MarkdownDescription: "A JSON payload which defines the model registration configuration.",
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
func (r *ModelRegisterResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func (r *ModelRegisterResource) client() (*opensearchapi.Client, error) {
	return opensearchapi.NewClient(r.config)
}

// Create registers a new model in OpenSearch.
func (r *ModelRegisterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ModelRegisterModel

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

	registerRequest, err := http.NewRequestWithContext(ctx, "POST", "/_plugins/_ml/models/_register?deploy=true", bytes.NewReader([]byte(data.Body.ValueString())))
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating model register request",
			fmt.Sprintf("Could not create model register request: %s", err.Error()),
		)
		return
	}

	registerRequest.Header.Set("Content-Type", "application/json")
	registerRequest.Header.Set("Accept", "application/json")

	registerHTTPResp, err := client.Client.Perform(registerRequest)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error registering model",
			fmt.Sprintf("Could not register model: %s", err.Error()),
		)
		return
	}

	// Decode then close explicitly (don’t defer inside loops; this is fine here).
	body, err := io.ReadAll(registerHTTPResp.Body)
	_ = registerHTTPResp.Body.Close()
	if err != nil {
		resp.Diagnostics.AddError("Error reading model register response", err.Error())
		return
	}

	if registerHTTPResp.StatusCode < 200 || registerHTTPResp.StatusCode >= 300 {
		resp.Diagnostics.AddError(
			"Error registering model",
			fmt.Sprintf("OpenSearch returned %d: %s", registerHTTPResp.StatusCode, string(body)),
		)
		return
	}

	var registerResponse skpropensearch.ModelRegisterResponse

	if err := json.Unmarshal(body, &registerResponse); err != nil {
		resp.Diagnostics.AddError(
			"Error parsing model register response",
			fmt.Sprintf("Could not parse model register response: %s", err.Error()),
		)
		return
	}

	modelID, err := waitForMLTaskCompletion(ctx, client, registerResponse.TaskID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error waiting for model registration task",
			fmt.Sprintf("Could not wait for model registration task: %s", err.Error()),
		)
		return
	}
	if modelID == "" {
		resp.Diagnostics.AddError(
			"Error waiting for model registration task",
			"Task completed but no model_id was returned by OpenSearch.",
		)
		return
	}

	data.ModelID = types.StringValue(modelID)

	tflog.Trace(ctx, "created Model Register resource", map[string]any{
		"model_id": modelID,
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Wait for the given ML task to complete, returning the model ID on success.
func waitForMLTaskCompletion(ctx context.Context, client *opensearchapi.Client, taskID string) (string, error) {
	const (
		pollInterval = 2 * time.Second
		timeout      = 15 * time.Minute
	)

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", fmt.Errorf("timed out after %s waiting for task %s", timeout.String(), taskID)
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("/_plugins/_ml/tasks/%s", taskID), nil)
			if err != nil {
				return "", err
			}

			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")

			httpResp, err := client.Client.Perform(req)
			if err != nil {
				return "", err
			}

			body, readErr := io.ReadAll(httpResp.Body)
			_ = httpResp.Body.Close()

			if readErr != nil {
				return "", readErr
			}

			if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
				return "", fmt.Errorf("OpenSearch returned %d while polling task: %s", httpResp.StatusCode, string(body))
			}

			var taskResp skpropensearch.TaskGetResponse

			if err := json.Unmarshal(body, &taskResp); err != nil {
				return "", err
			}

			if taskResp.State == skpropensearch.TaskStateCompleted {
				if taskResp.ModelID != "" {
					return taskResp.ModelID, nil
				}

				return "", fmt.Errorf("task completed but we could not find the model ID")
			}

			if taskResp.State == skpropensearch.TaskStateFailed {
				return "", fmt.Errorf("task %s failed: %s", taskID, string(body))
			}
		}
	}
}

// Read the resource state from OpenSearch for our model.
func (r *ModelRegisterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ModelRegisterModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// If we don’t have an ID, nothing to read.
	if data.ModelID.IsNull() || data.ModelID.IsUnknown() || data.ModelID.ValueString() == "" {
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

	getReq, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("/_plugins/_ml/models/%s", data.ModelID.ValueString()), nil)
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
func (r *ModelRegisterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ModelRegisterModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// All updatable fields are RequiresReplace, so Update should not be called for changes.
	// Still, if called (e.g. drift-only), just persist planned state.
	tflog.Trace(ctx, "updated Model Register resource (no-op update)", map[string]any{
		"model_id": data.ModelID.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete the model from OpenSearch.
func (r *ModelRegisterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ModelRegisterModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Nothing to delete if missing ID.
	if data.ModelID.IsNull() || data.ModelID.IsUnknown() || data.ModelID.ValueString() == "" {
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

	delReq, err := http.NewRequestWithContext(ctx, "DELETE", fmt.Sprintf("/_plugins/_ml/models/%s", data.ModelID.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating model delete request", err.Error())
		return
	}

	delReq.Header.Set("Content-Type", "application/json")
	delReq.Header.Set("Accept", "application/json")

	httpResp, err := client.Client.Perform(delReq)
	if err != nil {
		resp.Diagnostics.AddError("Error deleting model", err.Error())
		return
	}

	body, readErr := io.ReadAll(httpResp.Body)
	_ = httpResp.Body.Close()
	if readErr != nil {
		resp.Diagnostics.AddError("Error reading model delete response", readErr.Error())
		return
	}

	// Treat 404 as already deleted.
	if httpResp.StatusCode == http.StatusNotFound {
		tflog.Trace(ctx, "model already deleted", map[string]any{
			"model_id": data.ModelID.ValueString(),
		})
		return
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		resp.Diagnostics.AddError(
			"Error deleting model",
			fmt.Sprintf("OpenSearch returned %d: %s", httpResp.StatusCode, string(body)),
		)
		return
	}

	tflog.Trace(ctx, "deleted Model Register resource", map[string]any{
		"model_id": data.ModelID.ValueString(),
	})
}
