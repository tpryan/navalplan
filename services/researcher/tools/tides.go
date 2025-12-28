package tools

import (
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type TideArgs struct {
	Latitude  float64 `json:"latitude" description:"Decimal latitude"`
	Longitude float64 `json:"longitude" description:"Decimal longitude"`
	Date      string  `json:"date" description:"Date in YYYY-MM-DD format"`
}

type TideResult string

func NewTideTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "get_tides",
		Description: "Retrieves high and low tide predictions for a specific date.",
	}, func(ctx tool.Context, args TideArgs) (TideResult, error) {
		return "Tide API unavailable. Please search for 'Tide table [Location] [Date]'", nil
	})
}