package handlers_test

import (
	"bytes"
	"encoding/json"
	"errors"
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
	planDist := 10.0
	avgSpd := 6.2
	maxSpd := 7.8
	dur := "02:00:00"
	planDur := "01:30:00"
	stopID := int64(100)

	tests := []struct {
		name           string
		setupMock      func(m *MockStore)
		expectedStatus int
		verifyDebrief  func(t *testing.T, d *model.TrackDebrief)
	}{
		{
			name: "rejects debriefing planned track directly",
			setupMock: func(m *MockStore) {
				m.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
				m.On("GetVoyageTrack", trackID).Return(&model.VoyageTrack{
					ID:       trackID,
					VoyageID: voyageID,
					Kind:     "planned",
					Name:     "Planned Route",
				}, nil)
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "successfully pairs recorded track with planned track and generates conclusions",
			setupMock: func(m *MockStore) {
				m.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
				m.On("GetVoyageTrack", trackID).Return(&model.VoyageTrack{
					ID:               trackID,
					VoyageID:         voyageID,
					VoyageStopID:     &stopID,
					Kind:             "recorded",
					Name:             "Leg 1 Actual",
					DistanceNM:       &recDist,
					DurationInterval: &dur,
					AvgSpeedKts:      &avgSpd,
					MaxSpeedKts:      &maxSpd,
				}, nil)
				m.On("ListVoyageTracks", voyageID).Return([]model.VoyageTrack{
					{
						ID:               "plan-1",
						VoyageID:         voyageID,
						VoyageStopID:     &stopID,
						Kind:             "planned",
						Name:             "Leg 1 Plan",
						DistanceNM:       &planDist,
						DurationInterval: &planDur,
					},
				}, nil)
				m.On("ListStops", voyageID, 100, 0).Return([]model.Stop{}, nil)
				m.On("UpdateVoyageTrackDebrief", trackID, mock.AnythingOfType("model.RawJSON")).Return(nil)
			},
			expectedStatus: http.StatusOK,
			verifyDebrief: func(t *testing.T, d *model.TrackDebrief) {
				if d.TrackID != trackID {
					t.Errorf("Debrief track ID = %s, want %s", d.TrackID, trackID)
				}
				if d.PlannedTrackID == nil || *d.PlannedTrackID != "plan-1" {
					t.Errorf("Planned track ID = %v, want plan-1", d.PlannedTrackID)
				}
				if d.PlannedTrackName != "Leg 1 Plan" {
					t.Errorf("Planned track name = %s, want Leg 1 Plan", d.PlannedTrackName)
				}
				if d.RecordedDistanceNM != recDist {
					t.Errorf("Recorded distance = %v, want %v", d.RecordedDistanceNM, recDist)
				}
				if d.PlannedDistanceNM != planDist {
					t.Errorf("Planned distance = %v, want %v", d.PlannedDistanceNM, planDist)
				}
				if d.DistanceDeltaNM != 2.5 {
					t.Errorf("Distance delta = %v, want 2.5", d.DistanceDeltaNM)
				}
				if d.DistanceVariancePct != 25.0 {
					t.Errorf("Variance pct = %v, want 25.0", d.DistanceVariancePct)
				}
				if d.Conclusions == "" {
					t.Errorf("Expected non-empty conclusions comparing actual to planned")
				}
				if len(d.Observations) == 0 {
					t.Errorf("Expected observations in debrief")
				}
			},
		},
		{
			name: "falls back to direct rhumb line when no planned track exists",
			setupMock: func(m *MockStore) {
				m.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
				m.On("GetVoyageTrack", trackID).Return(&model.VoyageTrack{
					ID:               trackID,
					VoyageID:         voyageID,
					Kind:             "recorded",
					Name:             "Passage Leg",
					DistanceNM:       &recDist,
					DurationInterval: &dur,
					AvgSpeedKts:      &avgSpd,
					MaxSpeedKts:      &maxSpd,
				}, nil)
				m.On("ListVoyageTracks", voyageID).Return([]model.VoyageTrack{}, nil)
				m.On("ListStops", voyageID, 100, 0).Return([]model.Stop{}, nil)
				m.On("UpdateVoyageTrackDebrief", trackID, mock.AnythingOfType("model.RawJSON")).Return(nil)
			},
			expectedStatus: http.StatusOK,
			verifyDebrief: func(t *testing.T, d *model.TrackDebrief) {
				if d.PlannedTrackName != "Direct Rhumb Line Course" {
					t.Errorf("Planned track name = %s, want Direct Rhumb Line Course", d.PlannedTrackName)
				}
				if d.Conclusions == "" {
					t.Errorf("Expected non-empty conclusions")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := new(MockStore)
			tt.setupMock(mockStore)
			handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})

			req := httptest.NewRequest(http.MethodPost, "/api/v1/voyages/10/track/track-uuid-1/debrief", nil)
			req.SetPathValue("id", "10")
			req.SetPathValue("trackId", trackID)
			req = addPerson(req, personID)

			w := httptest.NewRecorder()
			handler.DebriefVoyageTrack(w, req)

			if w.Code != tt.expectedStatus {
				t.Fatalf("DebriefVoyageTrack() code = %d, want %d; body = %s", w.Code, tt.expectedStatus, w.Body.String())
			}

			if tt.verifyDebrief != nil {
				var debrief model.TrackDebrief
				if err := json.Unmarshal(w.Body.Bytes(), &debrief); err != nil {
					t.Fatal(err)
				}
				tt.verifyDebrief(t, &debrief)
			}
			mockStore.AssertExpectations(t)
		})
	}
}

func TestDebriefAllVoyageTracks(t *testing.T) {
	personID := int64(1)
	otherPersonID := int64(2)
	voyageID := int64(10)
	planDist := 10.0
	planDur := "01:30:00"
	recDist1 := 12.5
	avgSpd1 := 6.2
	maxSpd1 := 7.8
	dur1 := "02:00:00"
	recDist2 := 18.0
	avgSpd2 := 7.0
	maxSpd2 := 8.5
	dur2 := "02:30:00"
	stopID := int64(100)

	tests := []struct {
		name           string
		voyageID       string
		withAuth       bool
		authPersonID   int64
		setupMock      func(m *MockStore)
		expectedStatus int
		expectCount    int
		verifyDebriefs func(t *testing.T, debriefs []*model.TrackDebrief)
	}{
		{
			name:           "unauthorized when no person in context",
			voyageID:       "10",
			withAuth:       false,
			setupMock:      func(m *MockStore) {},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "invalid voyage id",
			voyageID:       "invalid",
			withAuth:       true,
			authPersonID:   personID,
			setupMock:      func(m *MockStore) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:         "voyage not found",
			voyageID:     "10",
			withAuth:     true,
			authPersonID: personID,
			setupMock: func(m *MockStore) {
				m.On("GetVoyage", voyageID).Return(nil, errors.New("not found"))
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name:         "forbidden when voyage owned by different person",
			voyageID:     "10",
			withAuth:     true,
			authPersonID: otherPersonID,
			setupMock: func(m *MockStore) {
				m.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
			},
			expectedStatus: http.StatusForbidden,
		},
		{
			name:         "successfully debriefs only recorded tracks pairing with planned routes",
			voyageID:     "10",
			withAuth:     true,
			authPersonID: personID,
			setupMock: func(m *MockStore) {
				m.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
				tracks := []model.VoyageTrack{
					{
						ID:               "plan-track-1",
						VoyageID:         voyageID,
						VoyageStopID:     &stopID,
						Kind:             "planned",
						Name:             "Leg 1 Plan",
						DistanceNM:       &planDist,
						DurationInterval: &planDur,
					},
					{
						ID:               "rec-track-1",
						VoyageID:         voyageID,
						VoyageStopID:     &stopID,
						Kind:             "recorded",
						Name:             "Leg 1 Actual",
						DistanceNM:       &recDist1,
						DurationInterval: &dur1,
						AvgSpeedKts:      &avgSpd1,
						MaxSpeedKts:      &maxSpd1,
					},
					{
						ID:               "rec-track-2",
						VoyageID:         voyageID,
						Kind:             "recorded",
						Name:             "Leg 2 Actual",
						DistanceNM:       &recDist2,
						DurationInterval: &dur2,
						AvgSpeedKts:      &avgSpd2,
						MaxSpeedKts:      &maxSpd2,
					},
				}
				stops := []model.Stop{
					{ID: 101, VoyageID: voyageID, LocationName: "Stop 1"},
					{ID: 102, VoyageID: voyageID, LocationName: "Stop 2"},
					{ID: 103, VoyageID: voyageID, LocationName: "Stop 3"},
				}
				m.On("ListVoyageTracks", voyageID).Return(tracks, nil)
				m.On("ListStops", voyageID, 100, 0).Return(stops, nil)
				m.On("UpdateVoyageTrackDebrief", "rec-track-1", mock.AnythingOfType("model.RawJSON")).Return(nil)
				m.On("UpdateVoyageTrackDebrief", "rec-track-2", mock.AnythingOfType("model.RawJSON")).Return(nil)
			},
			expectedStatus: http.StatusOK,
			expectCount:    2,
			verifyDebriefs: func(t *testing.T, debriefs []*model.TrackDebrief) {
				if debriefs[0].TrackID != "rec-track-1" {
					t.Errorf("Debrief 0 track ID = %s, want rec-track-1", debriefs[0].TrackID)
				}
				if debriefs[0].PlannedTrackID == nil || *debriefs[0].PlannedTrackID != "plan-track-1" {
					t.Errorf("Debrief 0 planned track ID = %v, want plan-track-1", debriefs[0].PlannedTrackID)
				}
				if debriefs[0].PlannedTrackName != "Leg 1 Plan" {
					t.Errorf("Debrief 0 planned name = %s, want Leg 1 Plan", debriefs[0].PlannedTrackName)
				}
				if debriefs[0].StartStopID == nil || *debriefs[0].StartStopID != 101 {
					t.Errorf("Debrief 0 start stop ID = %v, want 101", debriefs[0].StartStopID)
				}
				if debriefs[0].DistanceVariancePct != 25.0 {
					t.Errorf("Debrief 0 variance pct = %v, want 25.0", debriefs[0].DistanceVariancePct)
				}
				if debriefs[0].Conclusions == "" {
					t.Errorf("Debrief 0 missing conclusions")
				}
				if debriefs[1].TrackID != "rec-track-2" {
					t.Errorf("Debrief 1 track ID = %s, want rec-track-2", debriefs[1].TrackID)
				}
				if debriefs[1].StartStopID == nil || *debriefs[1].StartStopID != 102 {
					t.Errorf("Debrief 1 start stop ID = %v, want 102", debriefs[1].StartStopID)
				}
				if debriefs[1].Conclusions == "" {
					t.Errorf("Debrief 1 missing conclusions")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := new(MockStore)
			tt.setupMock(mockStore)
			handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})

			req := httptest.NewRequest(http.MethodPost, "/api/v1/voyages/"+tt.voyageID+"/track/debrief", nil)
			req.SetPathValue("id", tt.voyageID)
			if tt.withAuth {
				req = addPerson(req, tt.authPersonID)
			}

			w := httptest.NewRecorder()
			handler.DebriefAllVoyageTracks(w, req)

			if w.Code != tt.expectedStatus {
				t.Fatalf("DebriefAllVoyageTracks() status = %d, want %d; body = %s", w.Code, tt.expectedStatus, w.Body.String())
			}

			if tt.expectedStatus == http.StatusOK {
				var debriefs []*model.TrackDebrief
				if err := json.Unmarshal(w.Body.Bytes(), &debriefs); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if len(debriefs) != tt.expectCount {
					t.Errorf("Debrief count = %d, want %d", len(debriefs), tt.expectCount)
				}
				if tt.verifyDebriefs != nil {
					tt.verifyDebriefs(t, debriefs)
				}
			}
			mockStore.AssertExpectations(t)
		})
	}
}

func TestResolveDebriefStartStop(t *testing.T) {
	stop100 := model.Stop{ID: 100, LocationName: "Marina"}
	stop101 := model.Stop{ID: 101, LocationName: "Cove"}
	stop102 := model.Stop{ID: 102, LocationName: "Harbor"}
	stops := []model.Stop{stop100, stop101, stop102}

	id100 := int64(100)
	id101 := int64(101)
	id102 := int64(102)

	tests := []struct {
		name          string
		debrief       model.TrackDebrief
		track         *model.VoyageTrack
		stops         []model.Stop
		wantStopID    *int64
		wantStopTitle string
	}{
		{
			name: "matches explicit Leg 1 to stop 0",
			debrief: model.TrackDebrief{
				TrackName: "Voyage - Leg 1: Marina to Cove",
			},
			stops:         stops,
			wantStopID:    &id100,
			wantStopTitle: "Marina to Cove",
		},
		{
			name: "matches explicit Leg 2 to stop 1",
			debrief: model.TrackDebrief{
				TrackName: "Voyage - Leg 2: Cove to Harbor",
			},
			stops:         stops,
			wantStopID:    &id101,
			wantStopTitle: "Cove to Harbor",
		},
		{
			name: "corrects ending stop ID when debrief has destination stop",
			debrief: model.TrackDebrief{
				TrackName:    "Leg 1 Passage",
				VoyageStopID: &id101,
			},
			stops:         stops,
			wantStopID:    &id100,
			wantStopTitle: "Marina to Cove",
		},
		{
			name: "corrects last stop reference to preceding starting stop",
			debrief: model.TrackDebrief{
				TrackName:    "Final Leg",
				VoyageStopID: &id102,
			},
			stops:         stops,
			wantStopID:    &id101,
			wantStopTitle: "Cove to Harbor",
		},
		{
			name: "matches stop by location name when no leg number present",
			debrief: model.TrackDebrief{
				TrackName: "Passage Cove to Harbor",
			},
			stops:         stops,
			wantStopID:    &id101,
			wantStopTitle: "Cove to Harbor",
		},
		{
			name: "uploaded track with raw gpx name matches stop title from stops",
			debrief: model.TrackDebrief{
				TrackName:   "2024-08-12 14:23:10.gpx",
				StartStopID: &id100,
			},
			stops:         stops,
			wantStopID:    &id100,
			wantStopTitle: "Marina to Cove",
		},
		{
			name: "empty stops retains existing VoyageStopID safely",
			debrief: model.TrackDebrief{
				TrackName:    "Track",
				VoyageStopID: &id100,
			},
			stops:         nil,
			wantStopID:    &id100,
			wantStopTitle: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tt.debrief
			handlers.ResolveDebriefStartStop(&d, tt.track, tt.stops)
			if tt.wantStopID == nil {
				if d.StartStopID != nil {
					t.Errorf("StartStopID = %v, want nil", d.StartStopID)
				}
			} else {
				if d.StartStopID == nil || *d.StartStopID != *tt.wantStopID {
					t.Errorf("StartStopID = %v, want %v", d.StartStopID, *tt.wantStopID)
				}
			}
			if d.StopTitle != tt.wantStopTitle {
				t.Errorf("StopTitle = %q, want %q", d.StopTitle, tt.wantStopTitle)
			}
		})
	}
}
