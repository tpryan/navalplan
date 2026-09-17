package handlers_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"app/internal/agent"
	"app/internal/model"
	"app/internal/server/handlers"

	"github.com/stretchr/testify/mock"
)

const sampleRouteGPX = `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="NavalPlan" xmlns="http://www.topografix.com/GPX/1/1">
  <rte>
    <name>Solent Passage</name>
    <rtept lat="50.76" lon="-1.30"><name>Start</name></rtept>
    <rtept lat="50.71" lon="-1.50"><name>Finish</name></rtept>
  </rte>
</gpx>`

func TestUploadVoyageTrack(t *testing.T) {
	personID := int64(1)
	voyageID := int64(10)

	tests := []struct {
		name       string
		personID   *int64
		voyage     *model.Voyage
		voyageErr  error
		body       []byte
		isMulti    bool
		wantStatus int
	}{
		{
			name:       "unauthorized when no person",
			personID:   nil,
			voyage:     &model.Voyage{ID: voyageID, PersonID: personID},
			body:       []byte(sampleRouteGPX),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "forbidden when not owner",
			personID:   &personID,
			voyage:     &model.Voyage{ID: voyageID, PersonID: 999}, // different owner
			body:       []byte(sampleRouteGPX),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "bad request on invalid gpx",
			personID:   &personID,
			voyage:     &model.Voyage{ID: voyageID, PersonID: personID},
			body:       []byte("not xml"),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "successful upload raw body",
			personID:   &personID,
			voyage:     &model.Voyage{ID: voyageID, PersonID: personID},
			body:       []byte(sampleRouteGPX),
			wantStatus: http.StatusCreated,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockStore := new(MockStore)
			handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})

			if tc.voyage != nil {
				mockStore.On("GetVoyage", voyageID).Return(tc.voyage, tc.voyageErr)
			}
			if tc.wantStatus == http.StatusCreated {
				mockStore.On("ListStops", voyageID, 100, 0).Return([]model.Stop{}, nil)
				mockStore.On("CreateVoyageTrack", mock.AnythingOfType("*model.VoyageTrack")).Return(nil)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/voyages/10/track", bytes.NewReader(tc.body))
			req.SetPathValue("id", "10")
			if tc.personID != nil {
				req = addPerson(req, *tc.personID)
			}

			w := httptest.NewRecorder()
			handler.UploadVoyageTrack(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("UploadVoyageTrack() code = %d, want %d; body = %s", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

func TestUploadStopTrack(t *testing.T) {
	personID := int64(1)
	voyageID := int64(10)
	stopID := int64(20)

	t.Run("upload multipart to stop", func(t *testing.T) {
		mockStore := new(MockStore)
		handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})

		mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
		mockStore.On("GetStop", stopID).Return(&model.Stop{ID: stopID, VoyageID: voyageID}, nil)
		mockStore.On("CreateVoyageTrack", mock.MatchedBy(func(tr *model.VoyageTrack) bool {
			return tr.VoyageStopID != nil && *tr.VoyageStopID == stopID
		})).Return(nil)

		// Create multipart form
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("file", "leg1.gpx")
		if err != nil {
			t.Fatal(err)
		}
		part.Write([]byte(sampleRouteGPX))
		writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/voyages/10/stops/20/track", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.SetPathValue("id", "10")
		req.SetPathValue("stopId", "20")
		req = addPerson(req, personID)

		w := httptest.NewRecorder()
		handler.UploadStopTrack(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("UploadStopTrack() code = %d, want 201; body = %s", w.Code, w.Body.String())
		}
		mockStore.AssertExpectations(t)
	})
}

func TestListVoyageTracks(t *testing.T) {
	personID := int64(1)
	voyageID := int64(10)

	t.Run("owner lists tracks successfully", func(t *testing.T) {
		mockStore := new(MockStore)
		handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})

		mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
		mockStore.On("ListVoyageTracks", voyageID).Return([]model.VoyageTrack{
			{ID: "track-123", VoyageID: voyageID, Kind: "planned", Name: "Leg 1"},
		}, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/voyages/10/track", nil)
		req.SetPathValue("id", "10")
		req = addPerson(req, personID)

		w := httptest.NewRecorder()
		handler.ListVoyageTracks(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("ListVoyageTracks() code = %d, want 200", w.Code)
		}
		var tracks []model.VoyageTrack
		if err := json.Unmarshal(w.Body.Bytes(), &tracks); err != nil {
			t.Fatal(err)
		}
		if len(tracks) != 1 || tracks[0].ID != "track-123" {
			t.Errorf("Unexpected tracks output: %+v", tracks)
		}
	})
}

func TestDeleteVoyageTrack(t *testing.T) {
	personID := int64(1)
	voyageID := int64(10)
	trackID := "track-uuid-1"

	t.Run("deletes track successfully", func(t *testing.T) {
		mockStore := new(MockStore)
		handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})

		mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
		mockStore.On("GetVoyageTrack", trackID).Return(&model.VoyageTrack{ID: trackID, VoyageID: voyageID}, nil)
		mockStore.On("DeleteVoyageTrack", trackID).Return(nil)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/voyages/10/track/track-uuid-1", nil)
		req.SetPathValue("id", "10")
		req.SetPathValue("trackId", trackID)
		req = addPerson(req, personID)

		w := httptest.NewRecorder()
		handler.DeleteVoyageTrack(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("DeleteVoyageTrack() code = %d, want 204", w.Code)
		}
		mockStore.AssertExpectations(t)
	})
}

func TestDebriefVoyageTrack(t *testing.T) {
	personID := int64(1)
	voyageID := int64(10)
	trackID := "track-uuid-1"
	recDist := 12.5
	avgSpd := 6.2
	maxSpd := 7.8
	dur := "02:00:00"

	t.Run("returns debrief analysis successfully", func(t *testing.T) {
		mockStore := new(MockStore)
		handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})

		mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
		mockStore.On("GetVoyageTrack", trackID).Return(&model.VoyageTrack{
			ID:               trackID,
			VoyageID:         voyageID,
			Kind:             "recorded",
			Name:             "Passage Leg",
			DistanceNM:       &recDist,
			DurationInterval: &dur,
			AvgSpeedKts:      &avgSpd,
			MaxSpeedKts:      &maxSpd,
		}, nil)
		mockStore.On("ListVoyageTracks", voyageID).Return([]model.VoyageTrack{}, nil)
		mockStore.On("UpdateVoyageTrackDebrief", trackID, mock.AnythingOfType("model.RawJSON")).Return(nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/voyages/10/track/track-uuid-1/debrief", nil)
		req.SetPathValue("id", "10")
		req.SetPathValue("trackId", trackID)
		req = addPerson(req, personID)

		w := httptest.NewRecorder()
		handler.DebriefVoyageTrack(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("DebriefVoyageTrack() code = %d, want 200; body = %s", w.Code, w.Body.String())
		}

		var debrief model.TrackDebrief
		if err := json.Unmarshal(w.Body.Bytes(), &debrief); err != nil {
			t.Fatal(err)
		}
		if debrief.TrackID != trackID {
			t.Errorf("Debrief track ID = %s, want %s", debrief.TrackID, trackID)
		}
		if debrief.RecordedDistanceNM != recDist {
			t.Errorf("Recorded distance = %v, want %v", debrief.RecordedDistanceNM, recDist)
		}
		if len(debrief.Observations) == 0 {
			t.Errorf("Expected observations in debrief")
		}
		mockStore.AssertExpectations(t)
	})
}
