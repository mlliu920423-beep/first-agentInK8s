// Package llm is the single point where LLM clients are constructed.
//
// Keeping this in one file means:
//   - `ARK_API_KEY` / `ARK_MODEL_ID` are read exactly once (fail-fast on
//     startup, not on first request).
//   - Every specialist and the host multi-agent share one configured model.
package llm

import (
	"context"
	"fmt"
	"os"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/components/model"
)

// NewArkModel builds a Volcengine Ark chat model from env.
//
// Required:
//
//	ARK_API_KEY    — API key from the Volcengine console
//	ARK_MODEL_ID   — endpoint / model id (e.g. "ep-xxxx" or "doubao-...")
//
// Optional:
//
//	ARK_BASE_URL   — override the default endpoint (e.g. VPC / regional)
//	ARK_REGION     — region string; SDK default is "cn-beijing"
func NewArkModel(ctx context.Context) (model.ToolCallingChatModel, error) {
	apiKey := os.Getenv("ARK_API_KEY")
	modelID := os.Getenv("ARK_MODEL_ID")
	if apiKey == "" || modelID == "" {
		return nil, fmt.Errorf("ARK_API_KEY and ARK_MODEL_ID must both be set")
	}
	cfg := &ark.ChatModelConfig{
		APIKey: apiKey,
		Model:  modelID,
	}
	if v := os.Getenv("ARK_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("ARK_REGION"); v != "" {
		cfg.Region = v
	}
	m, err := ark.NewChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("ark.NewChatModel: %w", err)
	}
	return m, nil
}
