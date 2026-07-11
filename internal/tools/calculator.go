package tools

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// CalcInput is the argument struct exposed to the LLM. Field tags become the
// JSON schema seen by tool-calling models via utils.InferTool.
type CalcInput struct {
	A  float64 `json:"a"  jsonschema:"description=first operand"`
	Op string  `json:"op" jsonschema:"description=one of + - * /,enum=+,enum=-,enum=*,enum=/"`
	B  float64 `json:"b"  jsonschema:"description=second operand"`
}

type CalcOutput struct {
	Result float64 `json:"result"`
}

func newCalculatorTool() (tool.BaseTool, error) {
	return utils.InferTool(
		"calculator",
		"Perform a single arithmetic operation on two numbers. Use for exact math (add, subtract, multiply, divide).",
		func(ctx context.Context, in *CalcInput) (*CalcOutput, error) {
			var r float64
			switch in.Op {
			case "+":
				r = in.A + in.B
			case "-":
				r = in.A - in.B
			case "*":
				r = in.A * in.B
			case "/":
				if in.B == 0 {
					return nil, fmt.Errorf("division by zero")
				}
				r = in.A / in.B
			default:
				return nil, fmt.Errorf("unsupported op %q (expected + - * /)", in.Op)
			}
			return &CalcOutput{Result: r}, nil
		},
	)
}
