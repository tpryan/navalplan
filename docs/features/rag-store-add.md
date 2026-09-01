# Addendum: NGA Sailing Directions (Pubs 120–200) Integration

This addendum expands the automated nautical data pipeline to ingest the U.S. National Geospatial-Intelligence Agency (NGA) **Sailing Directions** (Planning Guides and Enroute Guides), providing coverage for all international waters and foreign coastlines outside the United States.

---

### 1. Architectural Changes & Corpus Strategy

NGA Sailing Directions consists of **5 Planning Guides** (Pubs 120, 140, 160, 180, 200) and **37 Enroute Guides** (Pubs 121–195).

```
GCS Bucket: gs://[PROJECT_ID]-nautical-data/
├── noaa-coast-pilot/
│   └── latest/ (CPB1_WEB.pdf ... CPB10_WEB.pdf)
└── nga-sailing-directions/
    ├── planning/ (Pub 140, 160, etc.)
    └── enroute/  (Pub 121, 141, etc.)
            │
            ▼
Vertex AI RAG Architecture (Multi-Corpus RetrieveContexts)
├── Corpus A: projects/.../ragCorpora/noaa-coast-pilot
└── Corpus B: projects/.../ragCorpora/nga-sailing-directions

```

#### Corpus Design

Rather than merging disparate national datasets into one unindexed pool, NGA data is housed in a dedicated corpus:

* **Corpus ID**: `nga-sailing-directions-corpus`
* **Embedding Model**: `publishers/google/models/text-embedding-005`
* **Chunk Settings**: 768 tokens, 128 overlap.
* **Search Strategy**: Hybrid search with keyword weighting to preserve port coordinates, fairway buoy IDs, and foreign geographical headlands.
* **Unified Tool Option**: Vertex AI `retrieveContexts` supports multi-corpus querying in a single API call by passing both `ragCorpus` references under `ragResources`.

---

### 2. Implementation Tasks for Coding Agent

* [ ] **Task A1: Storage & Infrastructure Provisioning**
* Update `scripts/setup_infra.sh` to provision the NGA bucket directory structure and create the `nga-sailing-directions-corpus` in Vertex AI.


* [ ] **Task A2: Expand Ingestion Worker (`code/jobs/nautical-sync`)**
* Generalize `code/jobs/coast-pilot-sync` into `code/jobs/nautical-sync`.
* Add the NGA crawler client targeting the NGA Maritime Safety Information (MSI) publication API.
* Implement GCS staging and automated Vertex AI import for all 42 NGA PDF volumes.


* [ ] **Task A3: Update Cloud Run Job & Scheduler**
* Retarget the monthly cron job to trigger both NOAA and NGA sync workflows.


* [ ] **Task A4: Implement Tool in Researcher (`code/services/researcher/internal/tool/`)**
* Implement `sailing_directions.go` supporting dual-corpus search (NOAA Coast Pilot for US, NGA Sailing Directions for International).


* [ ] **Task A5: Tool Registration & MCP Schema**
* Register `query_sailing_directions` in `service.go`, `adapters.go`, and `mcp_tool_spec.json`.


* [ ] **Task A6: Agent Prompt Updates**
* Update `pilot.md`, `commodore.md`, and `lookout.md` with guidelines on navigating international waters, foreign ports, and offshore ocean passages.


* [ ] **Task A7: Automated Verification**
* Write `scripts/agent_tests/test_sailing_directions.sh` to test international passage scenarios (e.g., English Channel, Caribbean, Bahamas).



---

### 3. Detailed Technical Specifications

#### Task A1: Infrastructure Updates (`scripts/setup_infra.sh`)

Append to infrastructure provisioning:

```bash
# 1. Create NGA RAG Corpus
python3 - <<EOF
from google.cloud import aiplatform
from vertexai.preview import rag

aiplatform.init(project="${PROJECT_ID}", location="${REGION}")
corpus = rag.create_corpus(
    display_name="nga-sailing-directions-corpus",
    description="NGA Sailing Directions Pubs 120-200 (Planning and Enroute Guides)"
)
print(f"CORPUS_ID={corpus.name.split('/')[-1]}")
EOF

```

---

#### Task A2: Ingestion Logic for NGA Sailing Directions

NGA hosts publication metadata via the MSI public API:

* **Planning Guides**: `[https://msi.nga.mil/api/publications/download?type=download&key=16694008/SFH00000/](https://msi.nga.mil/api/publications/download?type=download&key=16694008/SFH00000/)` (or via endpoint `/api/publications/get-publication-details?pubType=SDP`)
* **Enroute Guides**: Query `/api/publications/get-publication-details?pubType=SDE`

Extend the sync worker (`code/jobs/nautical-sync/main.go`):

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"

	"cloud.google.com/go/storage"
)

type NGAPubDetail struct {
	PubType  string `json:"pubType"`
	PubNum   string `json:"pubNum"`
	Title    string `json:"title"`
	DownloadURL string `json:"downloadUrl"`
}

type NGAPubsResponse struct {
	Publications []NGAPubDetail `json:"publications"`
}

func syncNGAPublications(ctx context.Context, bucketName string) error {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	pubTypes := []string{"SDP", "SDE"} // Planning Guides & Enroute Guides
	for _, pt := range pubTypes {
		apiURL := fmt.Sprintf("https://msi.nga.mil/api/publications/get-publication-details?pubType=%s", pt)
		resp, err := http.Get(apiURL)
		if err != nil {
			return fmt.Errorf("failed fetching NGA list for %s: %w", pt, err)
		}
		defer resp.Body.Close()

		var pubData NGAPubsResponse
		if err := json.NewDecoder(resp.Body).Decode(&pubData); err != nil {
			return err
		}

		for _, pub := range pubData.Publications {
			if pub.DownloadURL == "" {
				continue
			}
			destObj := fmt.Sprintf("nga-sailing-directions/%s/Pub_%s.pdf", pt, pub.PubNum)
			if err := downloadAndStreamToGCS(ctx, client, pub.DownloadURL, bucketName, destObj); err != nil {
				fmt.Fprintf(os.Stderr, "Error syncing Pub %s: %v\n", pub.PubNum, err)
			}
		}
	}
	return nil
}

func downloadAndStreamToGCS(ctx context.Context, client *storage.Client, fileURL, bucket, objectPath string) error {
	resp, err := http.Get(fileURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	wc := client.Bucket(bucket).Object(objectPath).NewWriter(ctx)
	wc.ContentType = "application/pdf"
	if _, err := io.Copy(wc, resp.Body); err != nil {
		wc.Close()
		return err
	}
	return wc.Close()
}

```

---

#### Task A4 & A5: Go Tool Implementation in Researcher Service

Create `code/services/researcher/internal/tool/sailing_directions.go` to query both NOAA Coast Pilot and NGA Sailing Directions based on geographic intent.

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

type SailingDirectionsTool struct {
	projectID         string
	location          string
	coastPilotCorpus  string
	ngaCorpus         string
	client            *http.Client
}

func NewSailingDirectionsTool(projectID, location, coastPilotCorpus, ngaCorpus string) (*SailingDirectionsTool, error) {
	client, err := google.DefaultClient(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("failed to init google auth client: %w", err)
	}
	return &SailingDirectionsTool{
		projectID:        projectID,
		location:         location,
		coastPilotCorpus: coastPilotCorpus,
		ngaCorpus:        ngaCorpus,
		client:           client,
	}, nil
}

type SailingDirectionsInput struct {
	Query      string `json:"query" jsonschema:"description=Specific route, harbor, channel depth, or hazard question"`
	Territory  string `json:"territory,omitempty" jsonschema:"enum=us,enum=international,enum=all,description=Scope of pilot data: 'us' for NOAA Coast Pilot, 'international' for NGA Sailing Directions, 'all' for both"`
}

type ragResource struct {
	RagCorpus string `json:"ragCorpus"`
}

type retrieveRequest struct {
	VertexRagStore struct {
		RagResources []ragResource `json:"ragResources"`
	} `json:"vertexRagStore"`
	Query struct {
		Text string `json:"text"`
	} `json:"query"`
}

func (t *SailingDirectionsTool) Execute(ctx context.Context, input SailingDirectionsInput) (string, error) {
	if input.Query == "" {
		return "", fmt.Errorf("query is required")
	}

	endpoint := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s:retrieveContexts",
		t.location, t.projectID, t.location)

	var resources []ragResource
	switch input.Territory {
	case "international":
		resources = append(resources, ragResource{
			RagCorpus: fmt.Sprintf("projects/%s/locations/%s/ragCorpora/%s", t.projectID, t.location, t.ngaCorpus),
		})
	case "us":
		resources = append(resources, ragResource{
			RagCorpus: fmt.Sprintf("projects/%s/locations/%s/ragCorpora/%s", t.projectID, t.location, t.coastPilotCorpus),
		})
	default: // "all"
		resources = append(resources,
			ragResource{RagCorpus: fmt.Sprintf("projects/%s/locations/%s/ragCorpora/%s", t.projectID, t.location, t.coastPilotCorpus)},
			ragResource{RagCorpus: fmt.Sprintf("projects/%s/locations/%s/ragCorpora/%s", t.projectID, t.location, t.ngaCorpus)},
		)
	}

	var reqBody retrieveRequest
	reqBody.VertexRagStore.RagResources = resources
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
		return "", fmt.Errorf("retrieveContexts failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("vertex rag error (%d): %s", resp.StatusCode, string(body))
	}

	return formatRagContextResponse(resp.Body)
}

```

#### Tool Registration (`code/services/researcher/mcp_tool_spec.json`)

Add the unified declaration:

```json
{
  "name": "query_sailing_directions",
  "description": "Searches official hydrographic pilot books (NOAA Coast Pilot for US waters and NGA Sailing Directions for all international/foreign waters). Returns authoritative channel depths, bridge vertical clearances, tidal rips, buoyage, hazards, and port regulations.",
  "parameters": {
    "type": "object",
    "properties": {
      "query": {
        "type": "string",
        "description": "The specific navigational question, harbor name, or passage leg (e.g., 'English Channel traffic separation schemes' or 'Chesapeake Bay bridge clearances')."
      },
      "territory": {
        "type": "string",
        "enum": ["us", "international", "all"],
        "description": "Select 'us' for NOAA Coast Pilot, 'international' for NGA Sailing Directions, or 'all' if unknown."
      }
    },
    "required": ["query"]
  }
}

```

---

#### Task A6: Prompt Updates for International Waters

* **`code/services/researcher/internal/prompt/pilot.md`**:
```markdown
### Sailing Directions & Pilotage Verification
When planning approaches to any coastal harbor, sound, or foreign waters, you MUST call `query_sailing_directions`.
- For U.S. coastal waters and Great Lakes, set `territory="us"`.
- For foreign or international ports (e.g., Bahamas, Caribbean, Europe, Mediterranean, Pacific), set `territory="international"`.
- Extract controlling channel depths, lock dimensions, local pilot boarding stations, and overhead cable/bridge clearances.

```


* **`code/services/researcher/internal/prompt/commodore.md`**:
```markdown
### Ocean & Regional Passage Routing
Consult `query_sailing_directions` (using `territory="international"` or `"all"`) for ocean basin passage planning, prevailing seasonal winds, currents, and ocean routing advice found in the NGA Planning Guides.

```


* **`code/services/researcher/internal/prompt/lookout.md`**:
```markdown
### International Hazard Evaluation
When screening offshore or foreign routes, use `query_sailing_directions` to cross-reference mandatory ship reporting systems, international Traffic Separation Schemes (TSS), military exercise areas, and regional piracy or navigational safety warnings.

```



---

### 4. Verification & Testing

Create `scripts/agent_tests/test_sailing_directions.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

# 1. Test US Coast Pilot retrieval
echo "Testing NOAA Coast Pilot lookup..."
curl -s -X POST http://localhost:8080/agent/pilot \
  -H "Content-Type: application/json" \
  -d '{"prompt": "What is the controlling depth and bridge clearance for Cape Cod Canal?"}' | grep -E "Coast Pilot|clearance|feet"

# 2. Test International NGA Sailing Directions retrieval
echo "Testing NGA Sailing Directions lookup..."
curl -s -X POST http://localhost:8080/agent/pilot \
  -H "Content-Type: application/json" \
  -d '{"prompt": "What are the pilotage requirements and approach hazards for Nassau Harbour, Bahamas?"}' | grep -E "Pub|Sailing Directions|Nassau"

```