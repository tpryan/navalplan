package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	aiplatform "cloud.google.com/go/aiplatform/apiv1beta1"
	"cloud.google.com/go/aiplatform/apiv1beta1/aiplatformpb"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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
	ctx, span := otel.Tracer("navalplan-backend").Start(ctx, "reasoning_engine:"+appName,
		trace.WithAttributes(
			attribute.String("cloud.resource_id", resourceName),
			attribute.String("cloud_resource_id", resourceName),
			attribute.String("gen_ai.agent.name", appName),
			attribute.String("gen_ai.operation.name", "reasoning_engine_query"),
		),
	)
	defer span.End()

	input, err := structpb.NewStruct(map[string]any{
		"input":     prompt,
		"appName":   appName,
		"userID":    userID,
		"sessionID": sessionID,
	})
	if err != nil {
		span.RecordError(err)
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
		span.RecordError(err)
		return "", fmt.Errorf("reasoning engine query failed: %w", err)
	}

	// ADK returns a struct that matches AgentEvent.
	// We need to extract the text from the response.
	output := resp.GetOutput()
	if output == nil {
		err := fmt.Errorf("reasoning engine returned empty output")
		span.RecordError(err)
		return "", err
	}

	// ADK response structure in Reasoning Engine is typically wrapped.
	// We'll look for content/parts/text.
	return extractTextFromValue(output), nil
}

func parseAndAppendChunk(ctx context.Context, line string, sb *strings.Builder) {
	var event AgentEvent
	if err := json.Unmarshal([]byte(line), &event); err == nil {
		if (event.Content.Role == "" || event.Content.Role == "model") && len(event.Content.Parts) > 0 {
			for _, p := range event.Content.Parts {
				if p.Text != "" && !p.Thought {
					sb.WriteString(p.Text)
				}
			}
		}
	} else {
		var generic struct {
			Content string `json:"content"`
			Text    string `json:"text"`
			Output  struct {
				Content string `json:"content"`
				Text    string `json:"text"`
			} `json:"output"`
		}
		if err := json.Unmarshal([]byte(line), &generic); err == nil && (generic.Content != "" || generic.Text != "" || generic.Output.Content != "" || generic.Output.Text != "") {
			if generic.Content != "" {
				sb.WriteString(generic.Content)
			} else if generic.Text != "" {
				sb.WriteString(generic.Text)
			} else if generic.Output.Content != "" {
				sb.WriteString(generic.Output.Content)
			} else if generic.Output.Text != "" {
				sb.WriteString(generic.Output.Text)
			}
		} else {
			if !strings.HasPrefix(line, "{") && !strings.HasPrefix(line, "[") {
				if ctx != nil {
					slog.DebugContext(ctx, "Failed to unmarshal stream chunk line, writing raw plain text", "data", line)
				}
				sb.WriteString(line)
			} else if ctx != nil {
				slog.WarnContext(ctx, "Failed to unmarshal JSON stream chunk line", "data", line)
			}
		}
	}
}

func (r *ReasoningEngineRunner) RunStreaming(ctx context.Context, resourceName, appName, userID, sessionID, prompt string) (string, error) {
	ctx, span := otel.Tracer("navalplan-backend").Start(ctx, "reasoning_engine:"+appName,
		trace.WithAttributes(
			attribute.String("cloud.resource_id", resourceName),
			attribute.String("cloud_resource_id", resourceName),
			attribute.String("gen_ai.agent.name", appName),
			attribute.String("gen_ai.operation.name", "reasoning_engine_query"),
		),
	)
	defer span.End()

	input, err := structpb.NewStruct(map[string]any{
		"input":     prompt,
		"appName":   appName,
		"userID":    userID,
		"sessionID": sessionID,
	})
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("failed to create input struct: %w", err)
	}

	req := &aiplatformpb.StreamQueryReasoningEngineRequest{
		Name:  resourceName,
		Input: input,
	}

	stream, err := r.Client.StreamQueryReasoningEngine(ctx, req)
	if err != nil {
		span.RecordError(err)
		slog.WarnContext(ctx, "StreamQueryReasoningEngine call failed, falling back to RunSync", "error", err)
		return r.RunSync(ctx, resourceName, appName, userID, sessionID, prompt)
	}

	var sb strings.Builder
	var lineBuf bytes.Buffer

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			span.RecordError(err)
			if sb.Len() > 0 {
				return sb.String(), nil
			}
			slog.WarnContext(ctx, "StreamQueryReasoningEngine receive error, falling back to RunSync", "error", err)
			return r.RunSync(ctx, resourceName, appName, userID, sessionID, prompt)
		}

		if resp != nil && len(resp.Data) > 0 {
			lineBuf.Write(resp.Data)

			for {
				lineBytes, err := lineBuf.ReadBytes('\n')
				if err != nil {
					if len(lineBytes) > 0 {
						lineBuf.Write(lineBytes)
					}
					break
				}

				line := strings.TrimSpace(string(lineBytes))
				if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") {
					continue
				}

				if strings.HasPrefix(line, "data:") {
					line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
					if line == "" || line == "[DONE]" {
						continue
					}
				}

				parseAndAppendChunk(ctx, line, &sb)
			}
		}
	}

	if lineBuf.Len() > 0 {
		line := strings.TrimSpace(lineBuf.String())
		if line != "" && !strings.HasPrefix(line, ":") && !strings.HasPrefix(line, "event:") {
			if strings.HasPrefix(line, "data:") {
				line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
			if line != "" && line != "[DONE]" {
				parseAndAppendChunk(ctx, line, &sb)
			}
		}
	}

	if sb.Len() == 0 {
		slog.WarnContext(ctx, "StreamQueryReasoningEngine returned no text, falling back to RunSync", "appName", appName)
		return r.RunSync(ctx, resourceName, appName, userID, sessionID, prompt)
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
		if val.StructValue == nil {
			return ""
		}

		// 1. Check for "content" key
		if content := val.StructValue.Fields["content"]; content != nil {
			if str := content.GetStringValue(); str != "" {
				return str
			}
			if contentStruct := content.GetStructValue(); contentStruct != nil {
				if parts := contentStruct.Fields["parts"]; parts != nil {
					if partsList := parts.GetListValue(); partsList != nil {
						var sb strings.Builder
						for _, item := range partsList.Values {
							if partStruct := item.GetStructValue(); partStruct != nil {
								if thoughtVal := partStruct.Fields["thought"]; thoughtVal != nil && thoughtVal.GetBoolValue() {
									continue
								}
								if text := partStruct.Fields["text"]; text != nil {
									sb.WriteString(text.GetStringValue())
								}
							}
						}
						if sb.Len() > 0 {
							return sb.String()
						}
					}
				}
			}
		}

		// 2. Check for "output" wrapper
		if output := val.StructValue.Fields["output"]; output != nil {
			if res := extractTextFromValue(output); res != "" {
				return res
			}
		}

		// 3. Check for "text" key
		if text := val.StructValue.Fields["text"]; text != nil {
			if str := text.GetStringValue(); str != "" {
				return str
			}
		}
	case *structpb.Value_ListValue:
		if val.ListValue == nil {
			return ""
		}
		var sb strings.Builder
		for _, item := range val.ListValue.Values {
			sb.WriteString(extractTextFromValue(item))
		}
		return sb.String()
	}

	return ""
}
