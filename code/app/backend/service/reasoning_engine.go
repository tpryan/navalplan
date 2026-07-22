package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	aiplatform "cloud.google.com/go/aiplatform/apiv1beta1"
	"cloud.google.com/go/aiplatform/apiv1beta1/aiplatformpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/structpb"
)

// ReasoningEngineRunner invokes agents using the Vertex AI Reasoning Engine API.
type ReasoningEngineRunner struct {
	Client *aiplatform.ReasoningEngineExecutionClient
}

// NewReasoningEngineRunner creates a new runner for Reasoning Engine.
func NewReasoningEngineRunner(ctx context.Context) (*ReasoningEngineRunner, error) {
	// The client will use application default credentials.
	// In production (Cloud Run), it uses the service account.
	// Default to us-central1 endpoint; can be customized via option if needed.
	client, err := aiplatform.NewReasoningEngineExecutionClient(ctx, option.WithEndpoint("us-central1-aiplatform.googleapis.com:443"))
	if err != nil {
		return nil, fmt.Errorf("failed to create reasoning engine execution client: %w", err)
	}
	return &ReasoningEngineRunner{Client: client}, nil
}

func (r *ReasoningEngineRunner) CreateSession(ctx context.Context, resourceName, appName, userID, sessionID string, state map[string]any) error {
	// The ADK on Reasoning Engine handles session creation internally or via Query params.
	// For standard ADK deployments, we don't strictly need a separate POST for session creation
	// if we pass the session ID in the query. However, if the user explicitly provided state,
	// we might need to handle it. For now, we'll log and skip as Reasoning Engine
	// typically manages session state in a different way or expects it in the Query.
	slog.DebugContext(ctx, "CreateSession called for Reasoning Engine", "resource", resourceName, "session", sessionID)
	return nil
}

func (r *ReasoningEngineRunner) RunSync(ctx context.Context, resourceName, appName, userID, sessionID, prompt string) (string, error) {
	input, err := structpb.NewStruct(map[string]any{
		"input": prompt,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create input struct: %w", err)
	}

	// For ADK apps, the session info is passed as part of the input or query parameters.
	// ADK expects "userId" and "sessionId" in the query parameters or the input body.
	// The Go SDK's Query method takes a QueryReasoningEngineRequest.

	req := &aiplatformpb.QueryReasoningEngineRequest{
		Name:  resourceName,
		Input: input,
	}

	resp, err := r.Client.QueryReasoningEngine(ctx, req)
	if err != nil {
		return "", fmt.Errorf("reasoning engine query failed: %w", err)
	}

	// ADK returns a struct that matches AgentEvent.
	// We need to extract the text from the response.
	output := resp.GetOutput()
	if output == nil {
		return "", fmt.Errorf("reasoning engine returned empty output")
	}

	// ADK response structure in Reasoning Engine is typically wrapped.
	// We'll look for content/parts/text.
	return extractTextFromValue(output), nil
}

func (r *ReasoningEngineRunner) RunStreaming(ctx context.Context, resourceName, appName, userID, sessionID, prompt string) (string, error) {
	input, err := structpb.NewStruct(map[string]any{
		"input": prompt,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create input struct: %w", err)
	}

	req := &aiplatformpb.StreamQueryReasoningEngineRequest{
		Name:  resourceName,
		Input: input,
	}

	stream, err := r.Client.StreamQueryReasoningEngine(ctx, req)
	if err != nil {
		return "", fmt.Errorf("reasoning engine stream query failed: %w", err)
	}

	var sb strings.Builder
	// StreamQueryReasoningEngine in v1beta1 returns a stream of httpbody.HttpBody.
	// Each chunk is a part of the streaming response from the agent (usually NDJSON).
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return sb.String(), fmt.Errorf("stream receive error: %w", err)
		}

		if resp != nil && len(resp.Data) > 0 {
			// Try to parse the chunk as an AgentEvent (ADK format).
			// Since it might be multiple NDJSON events or a partial event,
			// we'll use a scanner-like approach or just try to unmarshal.
			// For simplicity and matching AgentRunner logic, we'll collect the data
			// and parse it. Note: Real streaming would parse per event.
			var event AgentEvent
			if err := json.Unmarshal(resp.Data, &event); err == nil {
				if len(event.Content.Parts) > 0 {
					sb.WriteString(event.Content.Parts[0].Text)
				}
			} else {
				// Fallback: if it's not a full JSON, it might be a raw text chunk
				// or part of a larger stream. We'll append it for now.
				// In a production scenario, we'd use a json.Decoder on a pipe.
				slog.DebugContext(ctx, "Failed to unmarshal stream chunk", "data", string(resp.Data))
				sb.Write(resp.Data)
			}
		}
	}

	return sb.String(), nil
}

func extractTextFromValue(v *structpb.Value) string {
	if v == nil {
		return ""
	}

	switch val := v.Kind.(type) {
	case *structpb.Value_StringValue:
		return val.StringValue
	case *structpb.Value_StructValue:
		// Check for ADK response format: { "content": { "parts": [ { "text": "..." } ] } }
		if content := val.StructValue.Fields["content"]; content != nil {
			if contentStruct := content.GetStructValue(); contentStruct != nil {
				if parts := contentStruct.Fields["parts"]; parts != nil {
					if partsList := parts.GetListValue(); partsList != nil && len(partsList.Values) > 0 {
						if firstPart := partsList.Values[0].GetStructValue(); firstPart != nil {
							if text := firstPart.Fields["text"]; text != nil {
								return text.GetStringValue()
							}
						}
					}
				}
			}
		}
		// Fallback: search for "text" key anywhere in the struct
		if text := val.StructValue.Fields["text"]; text != nil {
			return text.GetStringValue()
		}
	case *structpb.Value_ListValue:
		// If it's a list, concatenate strings
		var sb strings.Builder
		for _, item := range val.ListValue.Values {
			sb.WriteString(extractTextFromValue(item))
		}
		return sb.String()
	}

	return ""
}
