package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"app/models"

	"github.com/go-chi/chi/v5"
	"google.golang.org/api/docs/v1"
)

type ExportRequest struct {
	Mode string `json:"mode"`
}

type ExportResponse struct {
	Title    string          `json:"title"`
	Requests []*docs.Request `json:"requests"`
}

func (h *Handler) ExportVoyage(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	voyageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Voyage ID", http.StatusBadRequest)
		return
	}

	// Fetch Data
	voyage, err := h.DB.GetVoyage(voyageID)
	if err != nil {
		http.Error(w, "Voyage not found", http.StatusNotFound)
		return
	}

	stops, err := h.DB.ListStops(voyageID)
	if err != nil {
		http.Error(w, "Failed to list stops", http.StatusInternalServerError)
		return
	}

	briefings := make(map[int64]*models.Briefing)
	for _, s := range stops {
		b, err := h.DB.GetBriefing(s.ID)
		if err == nil {
			briefings[s.ID] = b
		}
	}

	// Build Content
	var requests []*docs.Request

	// Header
	requests = append(requests, &docs.Request{
		InsertText: &docs.InsertTextRequest{
			Text:                 fmt.Sprintf("%s\n%s - %s\n\n", voyage.Title, voyage.StartDate.Format("Jan 02"), voyage.EndDate.Format("Jan 02, 2006")),
			EndOfSegmentLocation: &docs.EndOfSegmentLocation{},
		},
	})

	// Map (if available)
	mapboxToken := os.Getenv("NAVALPLAN_MB_TOKEN")
	if mapboxToken != "" && voyage.Latitude != nil && voyage.Longitude != nil {
		// Mapbox Static Image API
		mapURL := fmt.Sprintf("https://api.mapbox.com/styles/v1/mapbox/streets-v11/static/%f,%f,10,0/600x400?access_token=%s",
			*voyage.Longitude, *voyage.Latitude, mapboxToken)

		requests = append(requests, &docs.Request{
			InsertInlineImage: &docs.InsertInlineImageRequest{
				Uri:                  mapURL,
				EndOfSegmentLocation: &docs.EndOfSegmentLocation{},
			},
		})

		requests = append(requests, &docs.Request{
			InsertText: &docs.InsertTextRequest{
				Text:                 "\n\n",
				EndOfSegmentLocation: &docs.EndOfSegmentLocation{},
			},
		})
	}

	for _, stop := range stops {
		text := fmt.Sprintf("\n----------------\nStop: %s\nDate: %s\n", stop.LocationName, stop.TargetDate.Format("2006-01-02"))
		requests = append(requests, &docs.Request{
			InsertText: &docs.InsertTextRequest{
				Text:                 text,
				EndOfSegmentLocation: &docs.EndOfSegmentLocation{},
			},
		})

		if b, ok := briefings[stop.ID]; ok {
			// Unmarshal briefing parts to string for display
			// Weather
			var w map[string]interface{}
			json.Unmarshal(b.WeatherSummary, &w)
			if summary, ok := w["summary"].(string); ok {
				requests = append(requests, &docs.Request{
					InsertText: &docs.InsertTextRequest{
						Text:                 fmt.Sprintf("Weather: %s\n", summary),
						EndOfSegmentLocation: &docs.EndOfSegmentLocation{},
					},
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ExportResponse{
		Title:    fmt.Sprintf("Logbook: %s", voyage.Title),
		Requests: requests,
	})
}
