This technical implementation plan is structured for execution by an automated coding agent or software engineer.

---

### Phase 1: Architecture & Resource Specifications

* **Ingestion Worker**: A Cloud Run Job (`coast-pilot-sync`) written in Go or Python, packaged as a container, executed monthly via Cloud Scheduler.
* **Storage**: GCS bucket `gs://[PROJECT_ID]-coast-pilot/` configured with Object Versioning and Lifecycle Management (retain current version in `/latest/`, archive previous versions to `/archive/YYYY-MM/`).
* **Vector Store**: Vertex AI RAG Engine Corpus (`coast-pilot-corpus`) configured with chunk size 768, overlap 128, embedding model `publishers/google/models/text-embedding-005`, and hybrid search enabled ($\alpha = 0.6$).
* **Application Tool**: Go tool `query_coast_pilot` registered in `code/services/researcher/internal/tool/` exposed via the tool registry and MCP spec.
* **Agent Consumer Personas**: `pilot`, `harbourmaster`, `lookout`, `commodore`, and `specialist`.

---

### Phase 2: Implementation Task Checklist

* [ ] **Task 1: Infrastructure Provisioning (`scripts/setup_infra.sh`)**
* Create GCS bucket `gs://${PROJECT_ID}-coast-pilot`.
* Provision Vertex AI RAG Corpus `projects/${PROJECT_ID}/locations/${REGION}/ragCorpora/coast-pilot-corpus`.
* Assign `roles/storage.objectAdmin` and `roles/aiplatform.user` to the workload service account.


* [ ] **Task 2: Ingestion Job Service (`code/jobs/coast-pilot-sync/`)**
* Implement downloader for NOAA Coast Pilot volumes 1–10.
* Implement Cloud Storage uploader.
* Implement Vertex AI RAG file import caller.
* Write `Dockerfile` and Cloud Run Job configuration.


* [ ] **Task 3: Scheduler Setup (`scripts/setup_scheduler.sh`)**
* Create Cloud Scheduler cron trigger (`0 0 1 * *`) targeting the Cloud Run Job.


* [ ] **Task 4: Go RAG Tool in Researcher Service (`code/services/researcher/internal/tool/`)**
* Implement `coast_pilot.go` calling Vertex AI `retrieveContexts`.
* Write unit and mock tests in `coast_pilot_test.go`.


* [ ] **Task 5: Tool Registration & MCP Schema**
* Register `CoastPilotTool` in `service.go`.
* Register Function Declaration in `adapters.go`.
* Add tool schema to `mcp_tool_spec.json`.


* [ ] **Task 6: Agent System Prompts (`code/services/researcher/internal/prompt/`)**
* Update `pilot.md`, `harbourmaster.md`, `lookout.md`, `commodore.md`, and `specialist.md`.


* [ ] **Task 7: Integration & End-to-End Verification**
* Write shell test script `scripts/agent_tests/test_coast_pilot.sh`.
* Run regression tests across existing agent test suites.



---

### Phase 3: Detailed Task Specifications

#### Task 1: Infrastructure Provisioning

Update `scripts/setup_infra.sh` to include GCS and Vertex AI setup:

```bash
# 1. Create Storage Bucket
gcloud storage buckets create gs://${PROJECT_ID}-coast-pilot \
  --project=${PROJECT_ID} \
  --location=${REGION} \
  --uniform-bucket-level-access

# 2. Grant Vertex AI service agent access to read bucket
VERTEX_SA=$(gcloud projects get-iam-policy ${PROJECT_ID} \
  --flatten="bindings[].members" \
  --filter="bindings.role:roles/aiplatform.serviceAgent" \
  --format="value(bindings.members)" | head -n 1)

gcloud storage buckets add-iam-policy-binding gs://${PROJECT_ID}-coast-pilot \
  --member="${VERTEX_SA}" \
  --role="roles/storage.objectViewer"

```

Provision the RAG Corpus using Python/REST or Terraform:

```python
# scripts/setup_rag_corpus.py
from google.cloud import aiplatform
from vertexai.preview import rag

def init_corpus(project_id: str, location: str, corpus_display_name: str):
    aiplatform.init(project=project_id, location=location)
    corpus = rag.create_corpus(
        display_name=corpus_display_name,
        description="US Coast Guard / NOAA Coast Pilots Volumes 1-10"
    )
    print(f"Created corpus: {corpus.name}")

if __name__ == "__main__":
    import sys
    init_corpus(sys.argv[1], sys.argv[2], "coast-pilot-corpus")

```

---

#### Task 2: Ingestion Job (`code/jobs/coast-pilot-sync`)

Create directory `code/jobs/coast-pilot-sync` containing:

1. `main.go`:
* Fetch `[https://nauticalcharts.noaa.gov/publications/coast-pilot/index.html](https://nauticalcharts.noaa.gov/publications/coast-pilot/index.html)` or direct volume URLs:
`[https://nauticalcharts.noaa.gov/publications/coast-pilot/files/cp](https://nauticalcharts.noaa.gov/publications/coast-pilot/files/cp){N}/CPB{N}_WEB.pdf` (for $N \in [1..10]$).
* Stream download to GCS under `gs://${BUCKET}/latest/CPB{N}_WEB.pdf`.
* Trigger Vertex AI RAG Import API:
`POST https://${REGION}[-aiplatform.googleapis.com/v1beta1/projects/$](https://-aiplatform.googleapis.com/v1beta1/projects/$){PROJECT_ID}/locations/${REGION}/ragCorpora/${CORPUS_ID}/ragFiles:import`
with `ragFileChunkingConfig` (`chunkSize: 768`, `chunkOverlap: 128`).


2. `Dockerfile`:
* Multi-stage build based on `golang:1.24-alpine` -> `alpine:3.20`.


3. Cloud Build trigger or deployment step in `Makefile`:
```makefile
deploy-coast-pilot-job:
	gcloud run jobs deploy coast-pilot-sync \
		--source=code/jobs/coast-pilot-sync \
		--region=$(REGION) \
		--service-account=$(SERVICE_ACCOUNT) \
		--set-env-vars=GCS_BUCKET=$(PROJECT_ID)-coast-pilot,CORPUS_ID=$(CORPUS_ID),REGION=$(REGION)

```



---

#### Task 3: Cloud Scheduler Setup

```bash
gcloud scheduler jobs create http trigger-coast-pilot-sync \
  --location=${REGION} \
  --schedule="0 0 1 * *" \
  --uri="https://${REGION}-run.googleapis.com/apis/run.googleapis.com/v1/namespaces/${PROJECT_ID}/jobs/coast-pilot-sync:run" \
  --http-method=POST \
  --oauth-service-account-email=${SERVICE_ACCOUNT}

```

---

#### Task 4: Go RAG Tool in Researcher Service

Create `code/services/researcher/internal/tool/coast_pilot.go`:

```go
package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2/google"
)

type CoastPilotTool struct {
	projectID string
	location  string
	corpusID  string
	client    *http.Client
}

func NewCoastPilotTool(projectID, location, corpusID string) (*CoastPilotTool, error) {
	client, err := google.DefaultClient(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("failed to init google auth client: %w", err)
	}
	return &CoastPilotTool{
		projectID: projectID,
		location:  location,
		corpusID:  corpusID,
		client:    client,
	}, nil
}

type CoastPilotInput struct {
	Query      string `json:"query"`
	TopK       int    `json:"top_k,omitempty"`
}

type retrieveContextsRequest struct {
	VertexRagStore struct {
		RagResources []struct {
			RagCorpus string `json:"ragCorpus"`
		} `json:"ragResources"`
	} `json:"vertexRagStore"`
	Query struct {
		Text string `json:"text"`
	} `json:"query"`
}

func (t *CoastPilotTool) Execute(ctx context.Context, input CoastPilotInput) (string, error) {
	if input.Query == "" {
		return "", fmt.Errorf("query parameter is required")
	}
	topK := input.TopK
	if topK <= 0 || topK > 8 {
		topK = 5
	}

	endpoint := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s:retrieveContexts",
		t.location, t.projectID, t.location)

	var reqBody retrieveContextsRequest
	reqBody.VertexRagStore.RagResources = []struct {
		RagCorpus string `json:"ragCorpus"`
	}{
		{RagCorpus: fmt.Sprintf("projects/%s/locations/%s/ragCorpora/%s", t.projectID, t.location, t.corpusID)},
	}
	reqBody.Query.Text = input.Query

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("retrieveContexts call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("vertex rag error (%d): %s", resp.StatusCode, string(body))
	}

	// Format retrieved texts and source citations into structured text block
	return formatRagContextResponse(resp.Body)
}

```

---

#### Task 5: Tool Registration & Specs

1. **`code/services/researcher/internal/tool/service.go`**:
* Add `CoastPilotTool` to `Registry`.
* Add `RegisterCoastPilotTool(tool *CoastPilotTool)`.
* In `DefaultRegistry()`, instantiate using env vars (`COAST_PILOT_CORPUS_ID`, `VERTEX_LOCATION`, `GCP_PROJECT_ID`).


2. **`code/services/researcher/internal/tool/adapters.go`**:
* Register the `query_coast_pilot` declaration:
* Parameter `query` (string, required): Search prompt for harbor, route, hazard, or regulations.
* Parameter `top_k` (integer, optional): Number of contexts to return.




3. **`code/services/researcher/mcp_tool_spec.json`**:
* Add tool metadata and parameter schema.



---

#### Task 6: Prompt Updates (`code/services/researcher/internal/prompt/`)

Incorporate Coast Pilot consultation directives:

* **`pilot.md`**:
```markdown
### Coast Pilot Verification
Always call `query_coast_pilot` when evaluating leg approaches, harbors, inlets, or channels.
Extract:
- Controlling depths vs. vessel draft
- Bridge vertical clearances (closed and open) and cable overhead limits
- Local tidal current eddies, rapids, or rips (e.g., Woods Hole, Hell Gate)

```


* **`harbourmaster.md`**:
```markdown
### Coast Pilot Harbour Facilities
Consult `query_coast_pilot` for designated federal anchorage regulations, speed limit bylaws, municipal mooring basins, and VHF contact instructions for the harbormaster.

```


* **`lookout.md`**:
```markdown
### Coast Pilot Navigation Hazards
Use `query_coast_pilot` to identify unexploded ordnance areas, submerged wrecks, prominent shoals, and Traffic Separation Schemes (TSS) relevant to the voyage.

```


* **`commodore.md` & `specialist.md**`:
```markdown
Reference `query_coast_pilot` for authoritative answers on coastal pilotage rules, Intracoastal Waterway (ICW) mileage, and seasonal passage strategies.

```



---

#### Task 7: Test & Verification Checklist

1. **Unit Test (`code/services/researcher/internal/tool/coast_pilot_test.go`)**:
* Mock HTTP response from Vertex AI `retrieveContexts`.
* Verify correct parameter unmarshaling, context formatting, and citation generation.


2. **Integration Verification Script (`scripts/agent_tests/test_coast_pilot.sh`)**:
* Execute `test_navigator_agent.sh` and `test_researcher_agent.sh` to ensure no regression in existing tool adapters.
* Send test query: `"What are the bridge clearances and controlling depths for entering Newport Harbor?"`
* Assert response contains explicit Coast Pilot Volume citations.