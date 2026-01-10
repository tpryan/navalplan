package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/log"
	"github.com/joho/godotenv"
	"github.com/tpryan/navalplan/services/researcher/tools"
)

func main() {
	log.SetLevel(log.DebugLevel)
	godotenv.Load("../../.env")

	key := os.Getenv("GOOGLE_MAPS_API_KEY")
	if key == "" {
		log.Error("GOOGLE_MAPS_API_KEY is not set")
		os.Exit(1)
	}
	log.Info("Using API Key: " + key[:5] + "...")

	// Use one of the failing locations from the logs (British Virgin Islands)
	args := tools.PlacesArgs{
		Query:     "Marinas",
		Latitude:  18.424838,
		Longitude: -64.567140,
		Radius:    9260,
	}

	log.Info("Testing FindPlaces...", "query", args.Query, "lat", args.Latitude, "lng", args.Longitude)

	resp, err := tools.FindPlaces(args)
	if err != nil {
		log.Fatal("Tool execution error:", err)
	}

	if resp.Error != "" {
		log.Fatal("API Error returned:", resp.Error)
	}

	log.Info("Success!", "count", len(resp.Places))
	for i, p := range resp.Places {
		fmt.Printf("%d. %s (%s)\n", i+1, p.Name, p.Address)
	}
}
