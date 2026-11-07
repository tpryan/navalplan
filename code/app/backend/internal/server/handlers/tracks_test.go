package handlers_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
			voyage:     &model.Voyage{ID: voyageID, PersonID: 999},
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
				mockStore.On("ListDays", voyageID, 100, 0).Return([]model.VoyageDay{}, nil)
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

func TestUploadDayTrack(t *testing.T) {
	personID := int64(1)
	voyageID := int64(10)
	dayID := int64(20)

	t.Run("upload multipart to day", func(t *testing.T) {
		mockStore := new(MockStore)
		handler := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})

		mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
		mockStore.On("GetDay", dayID).Return(&model.VoyageDay{ID: dayID, VoyageID: voyageID}, nil)
		mockStore.On("CreateVoyageTrack", mock.MatchedBy(func(tr *model.VoyageTrack) bool {
			return tr.VoyageDayID != nil && *tr.VoyageDayID == dayID
		})).Return(nil)

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("file", "leg1.gpx")
		if err != nil {
			t.Fatal(err)
		}
		part.Write([]byte(sampleRouteGPX))
		writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/voyages/10/days/20/track", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.SetPathValue("id", "10")
		req.SetPathValue("dayId", "20")
		req = addPerson(req, personID)

		w := httptest.NewRecorder()
		handler.UploadDayTrack(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("UploadDayTrack() code = %d, want 201; body = %s", w.Code, w.Body.String())
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
	dayID := int64(100)

	tests := []struct {
		name           string
		setupMock      func(m *MockStore)
		expectedStatus int
		verifyDebrief  func(t *testing.T, d *model.TrackDebrief)
	}{
		{
			name: "rejects debriefing planned track directly when only one planned track exists",
			setupMock: func(m *MockStore) {
				m.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
				m.On("GetVoyageTrack", trackID).Return(&model.VoyageTrack{
					ID:       trackID,
					VoyageID: voyageID,
					Kind:     "planned",
					Name:     "Planned Route",
				}, nil)
				m.On("ListVoyageTracks", voyageID).Return([]model.VoyageTrack{
					{
						ID:       trackID,
						VoyageID: voyageID,
						Kind:     "planned",
						Name:     "Planned Route",
					},
				}, nil)
				m.On("ListDays", voyageID, 100, 0).Return([]model.VoyageDay{}, nil)
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "compares multiple planned routes when debriefing a planned track with >= 2 candidate plans",
			setupMock: func(m *MockStore) {
				distA := 11.2
				distB := 15.8
				m.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
				m.On("GetVoyageTrack", trackID).Return(&model.VoyageTrack{
					ID:          trackID,
					VoyageID:    voyageID,
					VoyageDayID: &dayID,
					Kind:        "planned",
					Name:        "Plan A Inside",
					DistanceNM:  &distA,
				}, nil)
				m.On("ListVoyageTracks", voyageID).Return([]model.VoyageTrack{
					{
						ID:          trackID,
						VoyageID:    voyageID,
						VoyageDayID: &dayID,
						Kind:        "planned",
						Name:        "Plan A Inside",
						DistanceNM:  &distA,
					},
					{
						ID:          "plan-2",
						VoyageID:    voyageID,
						VoyageDayID: &dayID,
						Kind:        "planned",
						Name:        "Plan B Outside",
						DistanceNM:  &distB,
					},
				}, nil)
				m.On("ListDays", voyageID, 100, 0).Return([]model.VoyageDay{
					{ID: dayID, VoyageID: voyageID, StartLocationName: "Cowes", EndLocationName: "Yarmouth"},
				}, nil)
				m.On("GetBriefing", dayID).Return((*model.Briefing)(nil), errors.New("no briefing"))
				m.On("UpdateVoyageTrackDebrief", mock.AnythingOfType("string"), mock.AnythingOfType("model.RawJSON")).Return(nil)
			},
			expectedStatus: http.StatusOK,
			verifyDebrief: func(t *testing.T, d *model.TrackDebrief) {
				if d.Mode != "comparison" {
					t.Errorf("Mode = %q, want 'comparison'", d.Mode)
				}
				if len(d.CandidateTracks) != 2 {
					t.Errorf("CandidateTracks len = %d, want 2", len(d.CandidateTracks))
				}
				if d.RecommendedTrackName == "" {
					t.Errorf("expected non-empty RecommendedTrackName")
				}
			},
		},
		{
			name: "successfully pairs recorded track with planned track and generates conclusions",
			setupMock: func(m *MockStore) {
				m.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
				m.On("GetVoyageTrack", trackID).Return(&model.VoyageTrack{
					ID:               trackID,
					VoyageID:         voyageID,
					VoyageDayID:      &dayID,
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
						VoyageDayID:      &dayID,
						Kind:             "planned",
						Name:             "Leg 1 Plan",
						DistanceNM:       &planDist,
						DurationInterval: &planDur,
					},
				}, nil)
				m.On("ListDays", voyageID, 100, 0).Return([]model.VoyageDay{}, nil)
				m.On("GetBriefing", dayID).Return(nil, nil).Maybe()
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
			name: "includes day briefing weather and tide data in debrief",
			setupMock: func(m *MockStore) {
				m.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
				m.On("GetVoyageTrack", trackID).Return(&model.VoyageTrack{
					ID:               trackID,
					VoyageID:         voyageID,
					VoyageDayID:      &dayID,
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
						VoyageDayID:      &dayID,
						Kind:             "planned",
						Name:             "Leg 1 Plan",
						DistanceNM:       &planDist,
						DurationInterval: &planDur,
					},
				}, nil)
				m.On("ListDays", voyageID, 100, 0).Return([]model.VoyageDay{
					{ID: dayID, VoyageID: voyageID, StartLocationName: "Marina", EndLocationName: "Cove"},
				}, nil)
				m.On("GetBriefing", dayID).Return(&model.Briefing{
					ID:             1,
					DayID:          dayID,
					WeatherSummary: model.RawJSON(`{"summary":"Clear sky","condition":"Clear sky","wind_speed_kt":18.5,"wind_direction":"ENE","wave_height_ft":4.5,"wave_period":7}`),
					Tides:          model.RawJSON(`[{"time":"2026-05-01T12:00:00Z","height_ft":2.4,"type":"high"}]`),
				}, nil)
				m.On("UpdateVoyageTrackDebrief", trackID, mock.AnythingOfType("model.RawJSON")).Return(nil)
			},
			expectedStatus: http.StatusOK,
			verifyDebrief: func(t *testing.T, d *model.TrackDebrief) {
				if !strings.Contains(d.WeatherImpact, "18 kt ENE winds") && !strings.Contains(d.WeatherImpact, "19 kt ENE winds") {
					t.Errorf("WeatherImpact = %q, expected wind speed and direction from briefing", d.WeatherImpact)
				}
				if !strings.Contains(d.WeatherImpact, "4.5 ft seas") {
					t.Errorf("WeatherImpact = %q, expected wave height from briefing", d.WeatherImpact)
				}
			},
		},
		{
			name: "rejects debrief when no planned track exists",
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
				m.On("ListDays", voyageID, 100, 0).Return([]model.VoyageDay{}, nil)
			},
			expectedStatus: http.StatusBadRequest,
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
	dayID := int64(100)

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
			name:         "rejects debrief all when no planned tracks exist",
			voyageID:     "10",
			withAuth:     true,
			authPersonID: personID,
			setupMock: func(m *MockStore) {
				m.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
				tracks := []model.VoyageTrack{
					{
						ID:       "rec-track-1",
						VoyageID: voyageID,
						Kind:     "recorded",
						Name:     "Leg 1 Actual",
					},
				}
				m.On("ListVoyageTracks", voyageID).Return(tracks, nil)
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:         "rejects debrief all when only planned tracks exist",
			voyageID:     "10",
			withAuth:     true,
			authPersonID: personID,
			setupMock: func(m *MockStore) {
				m.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
				tracks := []model.VoyageTrack{
					{
						ID:       "plan-track-1",
						VoyageID: voyageID,
						Kind:     "planned",
						Name:     "Leg 1 Plan",
					},
				}
				m.On("ListVoyageTracks", voyageID).Return(tracks, nil)
			},
			expectedStatus: http.StatusBadRequest,
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
						VoyageDayID:      &dayID,
						Kind:             "planned",
						Name:             "Leg 1 Plan",
						DistanceNM:       &planDist,
						DurationInterval: &planDur,
					},
					{
						ID:               "plan-track-2",
						VoyageID:         voyageID,
						Kind:             "planned",
						Name:             "Leg 2 Plan",
						DistanceNM:       &planDist,
						DurationInterval: &planDur,
					},
					{
						ID:               "rec-track-1",
						VoyageID:         voyageID,
						VoyageDayID:      &dayID,
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
				days := []model.VoyageDay{
					{ID: 101, VoyageID: voyageID, StartLocationName: "Stop 1", EndLocationName: "Stop 2"},
					{ID: 102, VoyageID: voyageID, StartLocationName: "Stop 2", EndLocationName: "Stop 3"},
				}
				m.On("ListVoyageTracks", voyageID).Return(tracks, nil)
				m.On("ListDays", voyageID, 100, 0).Return(days, nil)
				m.On("GetBriefing", mock.AnythingOfType("int64")).Return(nil, nil).Maybe()
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
				if debriefs[0].StartDayID == nil || *debriefs[0].StartDayID != 101 {
					t.Errorf("Debrief 0 start day ID = %v, want 101", debriefs[0].StartDayID)
				}
				if debriefs[0].DistanceVariancePct != 25.0 {
					t.Errorf("Debrief 0 variance pct = %v, want 25.0", debriefs[0].DistanceVariancePct)
				}
				if debriefs[0].DurationVariancePct == nil || math.Abs(*debriefs[0].DurationVariancePct-33.33) > 0.1 {
					t.Errorf("Debrief 0 duration variance pct = %v, want ~33.3", debriefs[0].DurationVariancePct)
				}
				if debriefs[0].DurationDelta != "+30m" {
					t.Errorf("Debrief 0 duration delta = %s, want +30m", debriefs[0].DurationDelta)
				}
				if debriefs[0].Conclusions == "" {
					t.Errorf("Debrief 0 missing conclusions")
				}
				if debriefs[1].TrackID != "rec-track-2" {
					t.Errorf("Debrief 1 track ID = %s, want rec-track-2", debriefs[1].TrackID)
				}
				if debriefs[1].PlannedTrackID == nil || *debriefs[1].PlannedTrackID != "plan-track-2" {
					t.Errorf("Debrief 1 planned track ID = %v, want plan-track-2", debriefs[1].PlannedTrackID)
				}
				if debriefs[1].PlannedTrackName != "Leg 2 Plan" {
					t.Errorf("Debrief 1 planned name = %s, want Leg 2 Plan", debriefs[1].PlannedTrackName)
				}
				if debriefs[1].StartDayID == nil || *debriefs[1].StartDayID != 102 {
					t.Errorf("Debrief 1 start day ID = %v, want 102", debriefs[1].StartDayID)
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

func TestResolveDebriefDay(t *testing.T) {
	day100 := model.VoyageDay{ID: 100, StartLocationName: "Marina", EndLocationName: "Cove"}
	day101 := model.VoyageDay{ID: 101, StartLocationName: "Cove", EndLocationName: "Harbor"}
	days := []model.VoyageDay{day100, day101}

	id100 := int64(100)
	id101 := int64(101)

	tests := []struct {
		name         string
		debrief      model.TrackDebrief
		track        *model.VoyageTrack
		days         []model.VoyageDay
		wantDayID    *int64
		wantDayTitle string
	}{
		{
			name: "matches explicit Leg 1 to day 0",
			debrief: model.TrackDebrief{
				TrackName: "Voyage - Leg 1: Marina to Cove",
			},
			days:         days,
			wantDayID:    &id100,
			wantDayTitle: "Marina to Cove",
		},
		{
			name: "matches explicit Leg 2 to day 1",
			debrief: model.TrackDebrief{
				TrackName: "Voyage - Leg 2: Cove to Harbor",
			},
			days:         days,
			wantDayID:    &id101,
			wantDayTitle: "Cove to Harbor",
		},
		{
			name: "matches day by location name when no leg number present",
			debrief: model.TrackDebrief{
				TrackName: "Passage Cove to Harbor",
			},
			days:         days,
			wantDayID:    &id101,
			wantDayTitle: "Cove to Harbor",
		},
		{
			name: "uploaded track with raw gpx name matches day title from days",
			debrief: model.TrackDebrief{
				TrackName:  "2024-08-12 14:23:10.gpx",
				StartDayID: &id100,
			},
			days:         days,
			wantDayID:    &id100,
			wantDayTitle: "Marina to Cove",
		},
		{
			name: "empty days retains existing VoyageDayID safely",
			debrief: model.TrackDebrief{
				TrackName:   "Track",
				VoyageDayID: &id100,
			},
			days:         nil,
			wantDayID:    &id100,
			wantDayTitle: "",
		},
		{
			name: "round trip circuit correctly pairs leg 1 with day 0 by start/end coordinates",
			debrief: model.TrackDebrief{
				TrackName: "Hodges Creek Marina - Anegada Reef Hotel",
			},
			track: &model.VoyageTrack{
				Name: "Hodges Creek Marina - Anegada Reef Hotel",
				SimplifiedGeoJSON: []byte(`{
					"type": "Feature",
					"geometry": {
						"type": "LineString",
						"coordinates": [[-64.5674, 18.4252], [-64.3848, 18.7229]]
					}
				}`),
			},
			days: []model.VoyageDay{
				{ID: 26, StartLocationName: "Parham Town, Tortola", StartLatitude: 18.4329, StartLongitude: -64.5575, EndLocationName: "Anegada", EndLatitude: 18.7194, EndLongitude: -64.3847, TargetDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)},
				{ID: 27, StartLocationName: "Anegada", StartLatitude: 18.7194, StartLongitude: -64.3847, EndLocationName: "Belle Vue, Jost Van Dyke", EndLatitude: 18.4371, EndLongitude: -64.7496, TargetDate: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)},
				{ID: 28, StartLocationName: "Belle Vue, Jost Van Dyke", StartLatitude: 18.4371, StartLongitude: -64.7496, EndLocationName: "Parham Town, Tortola", EndLatitude: 18.4264, EndLongitude: -64.5559, TargetDate: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)},
			},
			wantDayID:    func() *int64 { id := int64(26); return &id }(),
			wantDayTitle: "Parham Town, Tortola to Anegada",
		},
		{
			name: "round trip circuit resolves leg 2 correctly",
			debrief: model.TrackDebrief{
				TrackName: "Anegada Reef Hotel - North Latitude Marina",
			},
			track: &model.VoyageTrack{
				Name: "Anegada Reef Hotel - North Latitude Marina",
				SimplifiedGeoJSON: []byte(`{
					"type": "Feature",
					"geometry": {
						"type": "LineString",
						"coordinates": [[-64.3855, 18.7238], [-64.7478, 18.4357]]
					}
				}`),
			},
			days: []model.VoyageDay{
				{ID: 26, StartLocationName: "Parham Town, Tortola", StartLatitude: 18.4329, StartLongitude: -64.5575, EndLocationName: "Anegada", EndLatitude: 18.7194, EndLongitude: -64.3847, TargetDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)},
				{ID: 27, StartLocationName: "Anegada", StartLatitude: 18.7194, StartLongitude: -64.3847, EndLocationName: "Belle Vue, Jost Van Dyke", EndLatitude: 18.4371, EndLongitude: -64.7496, TargetDate: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)},
				{ID: 28, StartLocationName: "Belle Vue, Jost Van Dyke", StartLatitude: 18.4371, StartLongitude: -64.7496, EndLocationName: "Parham Town, Tortola", EndLatitude: 18.4264, EndLongitude: -64.5559, TargetDate: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)},
			},
			wantDayID:    func() *int64 { id := int64(27); return &id }(),
			wantDayTitle: "Anegada to Belle Vue, Jost Van Dyke",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tt.debrief
			handlers.ResolveDebriefDay(&d, tt.track, tt.days)
			if tt.wantDayID == nil {
				if d.StartDayID != nil {
					t.Errorf("StartDayID = %v, want nil", d.StartDayID)
				}
			} else {
				if d.StartDayID == nil || *d.StartDayID != *tt.wantDayID {
					t.Errorf("StartDayID = %v, want %v", d.StartDayID, *tt.wantDayID)
				}
			}
			if d.DayTitle != tt.wantDayTitle {
				t.Errorf("DayTitle = %q, want %q", d.DayTitle, tt.wantDayTitle)
			}
		})
	}
}

func TestParseDurationMinutes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantMins float64
		wantOk   bool
	}{
		{"colon hh:mm:ss", "02:30:00", 150, true},
		{"colon hh:mm", "01:45", 105, true},
		{"words hours and mins", "2h 15m", 135, true},
		{"empty", "", 0, false},
		{"N/A", "N/A", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := handlers.ParseDurationMinutes(tc.input)
			if ok != tc.wantOk || math.Abs(got-tc.wantMins) > 0.01 {
				t.Errorf("ParseDurationMinutes(%q) = (%v, %v), want (%v, %v)", tc.input, got, ok, tc.wantMins, tc.wantOk)
			}
		})
	}
}

func TestUpdateVoyageTrack(t *testing.T) {
	personID := int64(1)
	voyageID := int64(10)
	trackID := "track-uuid-rename"

	mockStore := new(MockStore)
	mockStore.On("GetVoyage", voyageID).Return(&model.Voyage{ID: voyageID, PersonID: personID}, nil)
	mockStore.On("GetVoyageTrack", trackID).Return(&model.VoyageTrack{
		ID:       trackID,
		VoyageID: voyageID,
		Kind:     "planned",
		Name:     "Original GPX Name",
	}, nil)
	mockStore.On("UpdateVoyageTrackName", trackID, "North Channel Option A").Return(nil)

	h := handlers.New(mockStore, "test_content", "http://test-agent", &agent.StaticResolver{BaseURL: "http://test-agent"})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/voyages/10/track/"+trackID, strings.NewReader(`{"name":"  North Channel Option A  "}`))
	req.SetPathValue("id", "10")
	req.SetPathValue("trackId", trackID)
	req = addPerson(req, personID)

	rr := httptest.NewRecorder()
	h.UpdateVoyageTrack(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("UpdateVoyageTrack status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var updated model.VoyageTrack
	if err := json.Unmarshal(rr.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if updated.Name != "North Channel Option A" {
		t.Errorf("updated.Name = %q, want %q", updated.Name, "North Channel Option A")
	}
	mockStore.AssertExpectations(t)
}
