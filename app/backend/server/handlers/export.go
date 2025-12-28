package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"app/models"

	"github.com/go-chi/chi/v5"
	"google.golang.org/api/docs/v1"
)

type ExportRequest struct {
	Mode string `json:"mode"`
}

type ExportResponse struct {
	DocURL string `json:"doc_url"`
	DocID  string `json:"doc_id"`
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

		// Init Docs API

		ctx := context.Background()

	

		// Create Doc

		title := fmt.Sprintf("Logbook: %s", voyage.Title)

		createdDoc, err := h.Docs.Create(ctx, title)

		if err != nil {

			log.Printf("Failed to create doc: %v", err)

			http.Error(w, "Failed to create Google Doc.", http.StatusInternalServerError)

			return

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

				// Facilities

				// ... (Simplified for now) ...

			}

		}

	

		if len(requests) > 0 {

			err = h.Docs.BatchUpdate(ctx, createdDoc.DocumentId, requests)

			if err != nil {

				log.Printf("Failed to populate doc: %v", err)

			}

		}

	// Update DB
	docIDStr := createdDoc.DocumentId
	voyage.GoogleDocID = &docIDStr
	now := time.Now()
	voyage.LastExportedAt = &now
	h.DB.UpdateVoyage(voyage)

	docURL := fmt.Sprintf("https://docs.google.com/document/d/%s", createdDoc.DocumentId)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ExportResponse{
		DocURL: docURL,
		DocID:  createdDoc.DocumentId,
	})
}
