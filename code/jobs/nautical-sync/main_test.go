package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name             string
		env              map[string]string
		wantProj         string
		wantRegion       string
		wantBucket       string
		wantCoast        string
		wantNGA          string
		wantLNM          string
		wantSyncTarget   string
		wantBatchSize    int
		wantChunkSize    int
		wantChunkOverlap int
	}{
		{
			name: "all environment variables set with gs prefix and custom rag settings",
			env: map[string]string{
				"PROJECT_ID":                    "navallog",
				"VERTEX_LOCATION":               "us-west1",
				"NAVALPLAN_PUBLICATIONS_BUCKET": "gs://navallog-nautical-publications",
				"COAST_PILOT_CORPUS_ID":         "coast-pilot-corpus",
				"NGA_CORPUS_ID":                 "nga-sailing-directions-corpus",
				"LNM_CORPUS_ID":                 "uscg-lnm-corpus",
				"SYNC_TARGET":                   "lnm",
				"RAG_BATCH_SIZE":                "2",
				"RAG_CHUNK_SIZE":                "1536",
				"RAG_CHUNK_OVERLAP":             "200",
			},
			wantProj:         "navallog",
			wantRegion:       "us-west1",
			wantBucket:       "navallog-nautical-publications",
			wantCoast:        "coast-pilot-corpus",
			wantNGA:          "nga-sailing-directions-corpus",
			wantLNM:          "uscg-lnm-corpus",
			wantSyncTarget:   "lnm",
			wantBatchSize:    2,
			wantChunkSize:    1536,
			wantChunkOverlap: 200,
		},
		{
			name: "fallback to GOOGLE_CLOUD_PROJECT and defaults",
			env: map[string]string{
				"GOOGLE_CLOUD_PROJECT": "fallback-project",
				"REGION":               "us-central1",
				"GCS_BUCKET":           "my-custom-bucket",
			},
			wantProj:         "fallback-project",
			wantRegion:       "us-central1",
			wantBucket:       "my-custom-bucket",
			wantCoast:        "",
			wantNGA:          "",
			wantLNM:          "",
			wantSyncTarget:   "all",
			wantBatchSize:    1,
			wantChunkSize:    1024,
			wantChunkOverlap: 128,
		},
		{
			name: "default region and default rag config when none provided",
			env: map[string]string{
				"GCP_PROJECT":             "another-project",
				"NAVALPLAN_LNM_CORPUS_ID": "custom-lnm-corpus",
			},
			wantProj:         "another-project",
			wantRegion:       "us-central1",
			wantBucket:       "",
			wantCoast:        "",
			wantNGA:          "",
			wantLNM:          "custom-lnm-corpus",
			wantSyncTarget:   "all",
			wantBatchSize:    1,
			wantChunkSize:    1024,
			wantChunkOverlap: 128,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			os.Clearenv()
			for k, v := range tc.env {
				os.Setenv(k, v)
			}
			cfg := loadConfig()
			if cfg.ProjectID != tc.wantProj {
				t.Errorf("ProjectID = %q, want %q", cfg.ProjectID, tc.wantProj)
			}
			if cfg.Region != tc.wantRegion {
				t.Errorf("Region = %q, want %q", cfg.Region, tc.wantRegion)
			}
			if cfg.GCSBucket != tc.wantBucket {
				t.Errorf("GCSBucket = %q, want %q", cfg.GCSBucket, tc.wantBucket)
			}
			if cfg.CoastPilotCorpusID != tc.wantCoast {
				t.Errorf("CoastPilotCorpusID = %q, want %q", cfg.CoastPilotCorpusID, tc.wantCoast)
			}
			if cfg.NGACorpusID != tc.wantNGA {
				t.Errorf("NGACorpusID = %q, want %q", cfg.NGACorpusID, tc.wantNGA)
			}
			if cfg.LNMCorpusID != tc.wantLNM {
				t.Errorf("LNMCorpusID = %q, want %q", cfg.LNMCorpusID, tc.wantLNM)
			}
			if cfg.SyncTarget != tc.wantSyncTarget {
				t.Errorf("SyncTarget = %q, want %q", cfg.SyncTarget, tc.wantSyncTarget)
			}
			if cfg.BatchSize != tc.wantBatchSize {
				t.Errorf("BatchSize = %d, want %d", cfg.BatchSize, tc.wantBatchSize)
			}
			if cfg.ChunkSize != tc.wantChunkSize {
				t.Errorf("ChunkSize = %d, want %d", cfg.ChunkSize, tc.wantChunkSize)
			}
			if cfg.ChunkOverlap != tc.wantChunkOverlap {
				t.Errorf("ChunkOverlap = %d, want %d", cfg.ChunkOverlap, tc.wantChunkOverlap)
			}
		})
	}
}

func TestDiscoverLNMDocuments(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/local-notices-to-mariners-by-cg-district", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>
			<svg>
				<a xlink:href="/local-notices-to-mariners?district=1+0&amp;subdistrict=n">District 1</a>
				<a xlink:href="/local-notices-to-mariners?district=11+0&amp;subdistrict=n">District 11</a>
			</svg>
		</body></html>`))
	})
	mux.HandleFunc("/local-notices-to-mariners", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		dist := r.URL.Query().Get("district")
		if dist == "1 0" {
			_, _ = w.Write([]byte(`<html><body>
				<a href="/sites/default/files/pdf/lnms/lnm01072026.pdf">Week 7</a>
				<a href="/sites/default/files/pdf/lnms/lnm01082026.pdf">Week 8</a>
			</body></html>`))
			return
		}
		_, _ = w.Write([]byte(`<html><body>
			<a href="/sites/default/files/pdf/lnms/lnm11082026.pdf">D11 Week 8</a>
		</body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	docs, err := discoverLNMDocuments(context.Background(), srv.Client(), srv.URL+"/local-notices-to-mariners-by-cg-district")
	if err != nil {
		t.Fatalf("discoverLNMDocuments() error = %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("discoverLNMDocuments() returned %d docs, want 2 (latest per district): %+v", len(docs), docs)
	}
	if docs[0].District != "d1" || docs[0].Filename != "lnm01082026.pdf" || docs[0].Week != 8 || docs[0].Year != 2026 {
		t.Errorf("docs[0] = %+v, want District=d1 Filename=lnm01082026.pdf Week=8 Year=2026", docs[0])
	}
	if docs[1].District != "d11" || docs[1].Filename != "lnm11082026.pdf" {
		t.Errorf("docs[1] = %+v, want District=d11 Filename=lnm11082026.pdf", docs[1])
	}
}

func TestCandidateLNMWeekYears(t *testing.T) {
	ref := time.Date(2026, 11, 5, 6, 0, 0, 0, time.UTC)
	got := candidateLNMWeekYears(ref, 4)
	if len(got) != 4 {
		t.Fatalf("candidateLNMWeekYears() len = %d, want 4: %+v", len(got), got)
	}
	if got[0] != [2]int{2026, 45} || got[1] != [2]int{2026, 44} {
		t.Errorf("candidateLNMWeekYears() = %+v, want [2026 45], [2026 44] first", got)
	}
}
