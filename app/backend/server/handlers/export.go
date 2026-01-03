package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"app/models"

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
	idStr := r.PathValue("id")
	voyageID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Voyage ID", http.StatusBadRequest)
		return
	}

	// Fetch Data
	voyage, err := h.DB.GetVoyage(r.Context(), voyageID)
	if err != nil {
		http.Error(w, "Voyage not found", http.StatusNotFound)
		return
	}

	stops, err := h.DB.ListStops(r.Context(), voyageID)
	if err != nil {
		http.Error(w, "Failed to list stops", http.StatusInternalServerError)
		return
	}

	briefings := make(map[int64]*models.Briefing)
	for _, s := range stops {
		b, err := h.DB.GetBriefing(r.Context(), s.ID)
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
