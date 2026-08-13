package tracing

import (
	"testing"
)

func TestResolveProjectID(t *testing.T) {
	tests := []struct {
		name              string
		inputProjectID    string
		envVars           map[string]string
		expectedProjectID string
	}{
		{
			name:              "explicit parameter returned first",
			inputProjectID:    "my-explicit-project",
			envVars:           map[string]string{"GOOGLE_CLOUD_PROJECT": "env-project"},
			expectedProjectID: "my-explicit-project",
		},
		{
			name:              "fallback to GOOGLE_CLOUD_PROJECT",
			inputProjectID:    "",
			envVars:           map[string]string{"GOOGLE_CLOUD_PROJECT": "google-cloud-proj"},
			expectedProjectID: "google-cloud-proj",
		},
		{
			name:              "fallback to GCP_PROJECT",
			inputProjectID:    "",
			envVars:           map[string]string{"GCP_PROJECT": "gcp-proj"},
			expectedProjectID: "gcp-proj",
		},
		{
			name:              "fallback to GCLOUD_PROJECT",
			inputProjectID:    "",
			envVars:           map[string]string{"GCLOUD_PROJECT": "gcloud-proj"},
			expectedProjectID: "gcloud-proj",
		},
		{
			name:              "fallback to PROJECT_ID",
			inputProjectID:    "",
			envVars:           map[string]string{"PROJECT_ID": "proj-id"},
			expectedProjectID: "proj-id",
		},
		{
			name:           "ignore numeric project number in NAVALPLAN_RESOURCE_ID when GOOGLE_CLOUD_PROJECT set",
			inputProjectID: "",
			envVars: map[string]string{
				"GOOGLE_CLOUD_PROJECT":  "navallog",
				"NAVALPLAN_RESOURCE_ID": "projects/70159681032/locations/us-central1/reasoningEngines/1643463669536784384",
			},
			expectedProjectID: "navallog",
		},
		{
			name:           "fallback to NAVALPLAN_RESOURCE_ID extraction when string project ID present",
			inputProjectID: "",
			envVars: map[string]string{
				"NAVALPLAN_RESOURCE_ID": "projects/navallog/locations/us-central1/reasoningEngines/1643463669536784384",
			},
			expectedProjectID: "navallog",
		},
		{
			name:           "fallback to OTEL_RESOURCE_ATTRIBUTES extraction",
			inputProjectID: "",
			envVars: map[string]string{
				"OTEL_RESOURCE_ATTRIBUTES": "cloud.resource_id=projects/navallog/locations/us-central1/reasoningEngines/12345",
			},
			expectedProjectID: "navallog",
		},
		{
			name:              "empty when no env vars or parameter provided",
			inputProjectID:    "",
			envVars:           map[string]string{},
			expectedProjectID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear host env vars first for test isolation
			for _, k := range []string{
				"GOOGLE_CLOUD_PROJECT", "GCP_PROJECT", "GCLOUD_PROJECT", "PROJECT_ID",
				"NAVALPLAN_RESOURCE_ID", "OTEL_RESOURCE_ATTRIBUTES",
			} {
				t.Setenv(k, "")
			}
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}
			res := ResolveProjectID(tt.inputProjectID)
			if res != tt.expectedProjectID {
				t.Errorf("ResolveProjectID(%q) = %q, want %q", tt.inputProjectID, res, tt.expectedProjectID)
			}
		})
	}
}
