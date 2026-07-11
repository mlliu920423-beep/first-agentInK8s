package tools

import (
	"context"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

type timeIn struct {
	TZ string `json:"tz" jsonschema:"description=IANA timezone name (e.g. Asia/Shanghai UTC America/New_York); empty means UTC"`
}

type timeOut struct {
	TZ      string `json:"tz"`
	RFC3339 string `json:"rfc3339"`
}

func newCurrentTimeTool() (tool.BaseTool, error) {
	return utils.InferTool(
		"current_time",
		"Return the current time in the requested IANA timezone (RFC3339). Use whenever the user asks for the time, date, or 'now'.",
		func(ctx context.Context, in *timeIn) (*timeOut, error) {
			tz := in.TZ
			if tz == "" {
				tz = "UTC"
			}
			loc, err := time.LoadLocation(tz)
			if err != nil {
				return nil, err
			}
			return &timeOut{TZ: tz, RFC3339: time.Now().In(loc).Format(time.RFC3339)}, nil
		},
	)
}
