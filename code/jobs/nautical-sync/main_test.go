package main

import (
	"os"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name       string
		env        map[string]string
		wantProj   string
		wantRegion string
		wantBucket string
		wantCoast  string
		wantNGA    string
	}{
		{
			name: "all environment variables set with gs prefix",
			env: map[string]string{
				"PROJECT_ID":                    "navallog",
				"VERTEX_LOCATION":               "us-west1",
				"NAVALPLAN_PUBLICATIONS_BUCKET": "gs://navallog-nautical-publications",
				"COAST_PILOT_CORPUS_ID":         "coast-pilot-corpus",
				"NGA_CORPUS_ID":                 "nga-sailing-directions-corpus",
			},
			wantProj:   "navallog",
			wantRegion: "us-west1",
			wantBucket: "navallog-nautical-publications",
			wantCoast:  "coast-pilot-corpus",
			wantNGA:    "nga-sailing-directions-corpus",
		},
		{
			name: "fallback to GOOGLE_CLOUD_PROJECT and REGION without gs prefix",
			env: map[string]string{
				"GOOGLE_CLOUD_PROJECT": "fallback-project",
				"REGION":               "us-central1",
				"GCS_BUCKET":           "my-custom-bucket",
			},
			wantProj:   "fallback-project",
			wantRegion: "us-central1",
			wantBucket: "my-custom-bucket",
			wantCoast:  "",
			wantNGA:    "",
		},
		{
			name: "default region when none provided",
			env: map[string]string{
				"GCP_PROJECT": "another-project",
			},
			wantProj:   "another-project",
			wantRegion: "us-central1",
			wantBucket: "",
			wantCoast:  "",
			wantNGA:    "",
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
		})
	}
}
