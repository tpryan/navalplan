package tools

import (
	"fmt"
	"time"

	"github.com/tpryan/noaago"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type TideArgs struct {
	Latitude  float64 `json:"latitude" description:"Decimal latitude"`
	Longitude float64 `json:"longitude" description:"Decimal longitude"`
	Date      string  `json:"date" description:"Date in YYYY-MM-DD format"`
}

type TideEvent struct {
	Time   string  `json:"time"`
	Type   string  `json:"type"`
	Height float64 `json:"height"`
	Unit   string  `json:"unit"`
}

type TideResult struct {
	StationName   string      `json:"station_name"`
	StationID     string      `json:"station_id"`
	DistanceMiles float64     `json:"distance_miles"`
	Tides         []TideEvent `json:"tides"`
	Error         string      `json:"error,omitempty"`
}

func NewTideTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "get_tides",
		Description: "Retrieves high and low tide predictions for a specific date from the nearest NOAA station.",
	}, func(ctx tool.Context, args TideArgs) (TideResult, error) {
		client := noaago.NewClient()

		fmt.Printf("[TideTool] Searching for tides at %f, %f\n", args.Latitude, args.Longitude)

		// 1. Find nearest station
		// Search within 50 miles. We explicitly filter for "tidepredictions" to find
		// both harmonic and subordinate stations that provide tide data.
		stationOpts := noaago.NewStationOptionsBuilder().
			Nearby(args.Latitude, args.Longitude, 50).
			Type(noaago.StationType("tidepredictions")).
			Build()

		stationsResp, err := client.FindStations(stationOpts)
		if err != nil {
			return TideResult{Error: fmt.Sprintf("Failed to search stations: %v", err)}, nil
		}

		if stationsResp.Count == 0 || len(stationsResp.Stations) == 0 {
			return TideResult{Error: "No tide stations found within 50 miles."}, nil
		}

		fmt.Printf("[TideTool] Found %d stations\n", len(stationsResp.Stations))

		// 2. Iterate through closest stations to find one that supports predictions
		// The API might return Current stations or others that don't support tide predictions.
		limit := 5
		if len(stationsResp.Stations) < limit {
			limit = len(stationsResp.Stations)
		}

		var lastErr error

		for i := 0; i < limit; i++ {
			station := stationsResp.Stations[i]
			fmt.Printf("[TideTool] Trying station %d: %s (%s)\n", i, station.Name, station.ID)

			parsedDate, err := time.Parse("2006-01-02", args.Date)
			if err != nil {
				return TideResult{Error: fmt.Sprintf("Invalid date format: %v", err)}, nil
			}

			// Get for the date with a 48-hour buffer before and after.
			beginDate := parsedDate.Add(-48 * time.Hour)
			endDate := parsedDate.Add(48 * time.Hour)

			tideOpts := noaago.NewTideOptionsBuilder().
				StationID(station.ID).
				Product(noaago.ProductPredictions).
				Datum(noaago.DatumMLLW).
				Units(noaago.UnitsEnglish).
				Interval(noaago.IntervalHighLow).
				TimeZone(noaago.TimeZoneLSTLDT).
				DateRange(beginDate, endDate).
				Build()

			tideResp, err := client.GetTides(tideOpts)
			if err != nil {
				// This station likely doesn't support predictions (e.g. it's a Current station).
				// Try the next one.
				fmt.Printf("[TideTool] Failed to get tides for %s: %v\n", station.Name, err)
				lastErr = err
				continue
			}
			fmt.Printf("[TideTool] Success with station %s\n", station.Name)

			// Success! Convert and return.
			var events []TideEvent
			for _, pt := range tideResp.GetData() {
				val, _ := pt.ValueFloat()
				events = append(events, TideEvent{
					Time:   pt.Time,
					Type:   pt.Type,
					Height: val,
					Unit:   "ft",
				})
			}

			return TideResult{
				StationName: station.Name,
				StationID:   station.ID,
				Tides:       events,
			}, nil
		}

		return TideResult{Error: fmt.Sprintf("Failed to get tides from any nearby stations. Last error: %v", lastErr)}, nil
	})
}
