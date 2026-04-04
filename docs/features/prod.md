To prepare `navalplan` for production on Cloud Run with the same security architecture as `navallog` (public backend, private/secure agent), here is the plan and the necessary files.

### 1. Architecture Overview

* **Service A (`navalplan-backend`)**: The public-facing web server. It handles user requests and serves the frontend. It is allowed to be accessed by `allUsers`.
* **Service B (`navalplan-researcher`)**: The private agent service. It is deployed with `--no-allow-unauthenticated`. It can ONLY be accessed by accounts (or services) presenting a valid OIDC Identity Token.
* **Communication**: The backend calls the agent using an authenticated HTTP client that generates an OIDC token for the agent's specific URL.

### 2. Create `cloudbuild.yaml` for the Backend

This build configuration will compile the frontend, move the static assets to the backend directory, and then deploy the backend service.

**File:** `cloudbuild.yaml` (Place in the root of `navalplan`)

```yaml
steps:
# 1. Install Frontend Dependencies
- name: 'gcr.io/cloud-builders/npm'
  args: ['install']
  dir: 'code/app/frontend'

# 2. Build Frontend
# This creates the production assets (usually in code/app/frontend/dist)
- name: 'gcr.io/cloud-builders/npm'
  args: ['run', 'build']
  dir: 'code/app/frontend'

# 3. Prepare Static Assets for Backend
# Move the built frontend assets to the folder the Go backend expects (./static.min)
- name: 'ubuntu'
  script: |
    rm -rf code/app/backend/static.min
    mv code/app/frontend/dist code/app/backend/static.min
  
# 4. Test Backend
- name: 'golang'
  args: ['test', './...']
  dir: 'code/app/backend'
  env:
  - 'GO111MODULE=on'

# 5. Deploy Backend to Cloud Run
- name: 'gcr.io/cloud-builders/gcloud'
  args:
  - 'run'
  - 'deploy'
  - 'navalplan-backend'
  - '--source'
  - '.'
  - '--region'
  - 'us-central1'
  - '--project'
  - 'navalplan' # Ensure this matches your project ID
  - '--allow-unauthenticated' # Publicly accessible
  - '--set-env-vars'
  - 'NAVALPLAN_DB_HOST=your-db-host,NAVALPLAN_DB_USER=your-db-user,NAVALPLAN_DB_PASS=your-db-pass,NAVALPLAN_DB_NAME=navalplan'
  dir: 'code/app/backend'
  
logsBucket: gs://navalplan-logging-bucket

```

*(Note: You will need to populate the `set-env-vars` with your actual production DB credentials or use Secret Manager).*

### 3. Secure Agent Communication (Code Change)

The current `research.go` uses a standard `http.Post`, which will fail because the agent now rejects unauthenticated requests. You must update the handler to use Google's `idtoken` library to authenticate the request.

**File:** `code/app/backend/server/handlers/research.go`

First, add the import:

```go
import (
    // ... existing imports
    "google.golang.org/api/idtoken" // Add this
)

```

Then, update `performStopResearch` to create an authenticated client:

```go
func (h *Handler) performStopResearch(stop *models.Stop) {
    log.SetPrefix("researcher-agent")

    agentURL := os.Getenv("NAVALPLAN_AGENT_URL")
    if agentURL == "" {
        // Fallback for local dev (if local agent doesn't require auth)
        agentURL = "http://127.0.0.1:8081" 
    }

    // ... (appName, userID setup) ...

    ctx := context.Background()

    // SECURE CLIENT CREATION
    // If we are calling a Cloud Run service securely, we need an ID Token.
    // We check if the URL looks like a remote Cloud Run URL (or force it via config).
    var client *http.Client
    var err error

    if strings.Contains(agentURL, "run.app") {
        // Create an authenticated client that appends the OIDC token for the specific audience (agentURL)
        client, err = idtoken.NewClient(ctx, agentURL)
        if err != nil {
            log.Errorf("Failed to create authenticated client: %v", err)
            return
        }
    } else {
        // Default client for localhost development
        client = http.DefaultClient
    }

    // ... (Create Session Logic - use 'client' instead of http.Post) ...
    // Note: Creating the session also requires auth if the agent is secure.
    
    // 1. Create Session
    createSessionURL := fmt.Sprintf("%s/api/apps/%s/users/%s/sessions/%s", agentURL, appName, userID, sessionID)
    respSession, err := client.Post(createSessionURL, "application/json", nil)
    if err != nil {
        log.Infof("Failed to create agent session: %v", err)
    } else {
        respSession.Body.Close()
    }

    // ... (Prompt setup) ...

    // 2. Run Agent
    // Use the authenticated 'client' here
    resp, err := client.Post(agentURL+"/api/run", "application/json", bytes.NewBuffer(jsonData))
    if err != nil {
        log.Infof("Failed to call agent: %v", err)
        return
    }
    defer resp.Body.Close()

    // ... (Rest of the function remains the same)
}

```

*Don't forget to run `go get google.golang.org/api/idtoken` in `code/app/backend` to update your `go.mod`.*

### 4. Deployment Plan

1. **Deploy the Agent First**
Run the agent build. This must be done first so you can get the URL for the backend configuration.
```bash
gcloud builds submit --config cloudbuild-agent.yaml .

```


*Result:* You will get a Service URL (e.g., `https://navalplan-researcher-xyz.a.run.app`).
2. **Deploy the Backend**
Update your `cloudbuild.yaml` (or your Cloud Run revision) to include the Agent URL.
```bash
gcloud builds submit --config cloudbuild.yaml --substitutions=_AGENT_URL="https://navalplan-researcher-xyz.a.run.app" .

```


*(Alternatively, set the `NAVALPLAN_AGENT_URL` environment variable in the Cloud Console for the `navalplan-backend` service).*
3. **Configure Custom Domain**
Once the backend is deployed, map the domain:
```bash
gcloud beta run domain-mappings create --service navalplan-backend --domain plan.wakelog.app --region us-central1

```


4. **Database Connection**
Ensure the `navalplan-backend` service account has the "Cloud SQL Client" role and that the VPC connector (if used) or public IP configuration allows it to reach your Postgres instance.