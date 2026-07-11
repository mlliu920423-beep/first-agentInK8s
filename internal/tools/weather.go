package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// Stubbed weather data. Real integrations would call an external HTTP API;
// canned data keeps the demo hermetic and deterministic.
var cannedWeather = map[string]weatherOut{
	"beijing":   {City: "Beijing", TempC: 21, Summary: "sunny"},
	"shanghai":  {City: "Shanghai", TempC: 27, Summary: "cloudy"},
	"guangzhou": {City: "Guangzhou", TempC: 30, Summary: "humid, thunderstorms"},
	"shenzhen":  {City: "Shenzhen", TempC: 29, Summary: "partly cloudy"},
	"hangzhou":  {City: "Hangzhou", TempC: 24, Summary: "light rain"},
}

type weatherIn struct {
	City string `json:"city" jsonschema:"description=city name in English or Pinyin"`
}

type weatherOut struct {
	City    string  `json:"city"`
	TempC   float64 `json:"temp_c"`
	Summary string  `json:"summary"`
}

func newWeatherTool() (tool.BaseTool, error) {
	return utils.InferTool(
		"weather",
		"Look up current weather for a city. Returns temperature (C) and a short summary. Coverage limited to major Chinese cities.",
		func(ctx context.Context, in *weatherIn) (*weatherOut, error) {
			key := strings.ToLower(strings.TrimSpace(in.City))
			if w, ok := cannedWeather[key]; ok {
				return &w, nil
			}
			return nil, fmt.Errorf("no weather data for city %q (known: beijing, shanghai, guangzhou, shenzhen, hangzhou)", in.City)
		},
	)
}
