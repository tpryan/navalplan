package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"golang.org/x/oauth2/google"
)

const (
	defaultUSCGIndexURL = "https://navcen.uscg.gov/local-notices-to-mariners-by-cg-district"
	browserUserAgent    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"
)

func setBrowserHeaders(req *http.Request, referer string) {
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,application/pdf,application/json,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
}

type Config struct {
	ProjectID          string
	Region             string
	GCSBucket          string
	CoastPilotCorpusID string
	NGACorpusID        string
	LNMCorpusID        string
	LNMIndexURL        string
	SyncTarget         string
	BatchSize          int
	ChunkSize          int
	ChunkOverlap       int
}

type NGAStoredPub struct {
	PubTypeID              int    `json:"pubTypeId"`
	PubDownloadDisplayName string `json:"pubDownloadDisplayName"`
	SectionName            string `json:"sectionName"`
	SectionDisplayName     string `json:"sectionDisplayName"`
	FullFilename           string `json:"fullFilename"`
	S3Key                  string `json:"s3Key"`
	FileSize               int64  `json:"fileSize"`
}

type ragImportRequest struct {
	ImportRagFilesConfig struct {
		GcsSource struct {
			Uris []string `json:"uris"`
		} `json:"gcsSource"`
		RagFileChunkingConfig struct {
			FixedLengthChunking struct {
				ChunkSize    int `json:"chunkSize"`
				ChunkOverlap int `json:"chunkOverlap"`
			} `json:"fixedLengthChunking"`
		} `json:"ragFileChunkingConfig"`
	} `json:"importRagFilesConfig"`
}

func main() {
	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
				a.Key = "severity"
			}
			return a
		},
	}))
	slog.SetDefault(logger)

	cfg := loadConfig()
	if cfg.ProjectID == "" {
		slog.Error("PROJECT_ID environment variable is required")
		os.Exit(1)
	}
	if cfg.GCSBucket == "" {
		cfg.GCSBucket = fmt.Sprintf("%s-nautical-data", cfg.ProjectID)
	}
	if cfg.Region == "" {
		cfg.Region = "us-central1"
	}

	slog.Info("Starting nautical data sync job", "project", cfg.ProjectID, "region", cfg.Region, "bucket", cfg.GCSBucket, "syncTarget", cfg.SyncTarget)

	storageClient, err := storage.NewClient(ctx)
	if err != nil {
		slog.Error("Failed to initialize Cloud Storage client", "error", err)
		os.Exit(1)
	}
	defer storageClient.Close()

	runMonthly := cfg.SyncTarget == "all" || cfg.SyncTarget == "monthly"
	runLNM := cfg.SyncTarget == "all" || cfg.SyncTarget == "lnm" || cfg.SyncTarget == "weekly"

	if runMonthly {
		// 1. Sync NOAA Coast Pilot Volumes 1-10
		slog.Info("Starting NOAA Coast Pilot sync...")
		if err := syncCoastPilot(ctx, storageClient, cfg.GCSBucket); err != nil {
			slog.Error("NOAA Coast Pilot sync encountered error", "error", err)
		} else {
			slog.Info("NOAA Coast Pilot sync completed successfully")
		}

		// 2. Sync NGA Sailing Directions
		slog.Info("Starting NGA Sailing Directions sync...")
		ngaURIs, err := syncNGAPublications(ctx, storageClient, cfg.GCSBucket)
		if err != nil {
			slog.Error("NGA Sailing Directions sync encountered error", "error", err)
		} else {
			slog.Info("NGA Sailing Directions sync completed successfully", "count", len(ngaURIs))
		}

		// 3. Trigger Vertex AI RAG Import for Coast Pilot Corpus
		if cfg.CoastPilotCorpusID != "" {
			slog.Info("Triggering Vertex AI RAG import for Coast Pilot...", "corpusID", cfg.CoastPilotCorpusID)
			var gcsURIs []string
			for vol := 1; vol <= 10; vol++ {
				gcsURIs = append(gcsURIs, fmt.Sprintf("gs://%s/noaa-coast-pilot/latest/CPB%d_WEB.pdf", cfg.GCSBucket, vol))
			}
			if err := triggerRagImport(ctx, cfg, cfg.CoastPilotCorpusID, gcsURIs); err != nil {
				slog.Error("Failed to import Coast Pilot to Vertex AI RAG corpus", "error", err)
			} else {
				slog.Info("Coast Pilot RAG import triggered successfully")
			}
		} else {
			slog.Warn("COAST_PILOT_CORPUS_ID not set, skipping Vertex AI RAG import")
		}

		// 4. Trigger Vertex AI RAG Import for NGA Corpus
		if cfg.NGACorpusID != "" {
			if len(ngaURIs) == 0 {
				slog.Warn("No NGA publications synced, skipping RAG import")
			} else {
				slog.Info("Triggering Vertex AI RAG import for NGA Sailing Directions...", "corpusID", cfg.NGACorpusID, "fileCount", len(ngaURIs))
				if err := triggerRagImport(ctx, cfg, cfg.NGACorpusID, ngaURIs); err != nil {
					slog.Error("Failed to import NGA Sailing Directions to Vertex AI RAG corpus", "error", err)
				} else {
					slog.Info("NGA Sailing Directions RAG import triggered successfully")
				}
			}
		} else {
			slog.Warn("NGA_CORPUS_ID not set, skipping Vertex AI RAG import")
		}
	}

	if runLNM {
		// 5. Sync USCG Local Notice to Mariners (LNM)
		slog.Info("Starting USCG Local Notice to Mariners (LNM) sync...")
		lnmURIs, err := syncUSCGLNM(ctx, storageClient, cfg.GCSBucket, cfg.LNMIndexURL)
		if err != nil {
			slog.Error("USCG LNM sync encountered error", "error", err)
		} else {
			slog.Info("USCG LNM sync completed successfully", "count", len(lnmURIs))
		}

		// 6. Trigger Vertex AI RAG Import for USCG LNM Corpus
		if cfg.LNMCorpusID != "" {
			if len(lnmURIs) == 0 {
				slog.Warn("No USCG LNM publications synced, skipping RAG import")
			} else {
				slog.Info("Triggering Vertex AI RAG import for USCG Local Notice to Mariners...", "corpusID", cfg.LNMCorpusID, "fileCount", len(lnmURIs))
				if err := triggerRagImport(ctx, cfg, cfg.LNMCorpusID, lnmURIs); err != nil {
					slog.Error("Failed to import USCG LNM to Vertex AI RAG corpus", "error", err)
				} else {
					slog.Info("USCG LNM RAG import triggered successfully")
				}
			}
		} else {
			slog.Warn("LNM_CORPUS_ID not set, skipping Vertex AI RAG import")
		}
	}

	slog.Info("Nautical sync job completed")
}

func resolveCorpusResourceName(ctx context.Context, client *http.Client, projectID, region, corpusID string) (string, error) {
	if strings.HasPrefix(corpusID, "projects/") {
		return corpusID, nil
	}
	isNumeric := true
	for _, r := range corpusID {
		if r < '0' || r > '9' {
			isNumeric = false
			break
		}
	}
	if isNumeric {
		return fmt.Sprintf("projects/%s/locations/%s/ragCorpora/%s", projectID, region, corpusID), nil
	}

	listURL := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s/ragCorpora", region, projectID, region)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("listing rag corpora: %w", err)
	}
	defer resp.Body.Close()

	var listResp struct {
		RagCorpora []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"ragCorpora"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return "", fmt.Errorf("decoding rag corpora list: %w", err)
	}

	for _, corpus := range listResp.RagCorpora {
		if corpus.DisplayName == corpusID || strings.HasSuffix(corpus.Name, corpusID) {
			return corpus.Name, nil
		}
	}
	return "", fmt.Errorf("corpus %q not found in project %s (%s)", corpusID, projectID, region)
}

func triggerRagImport(ctx context.Context, cfg Config, corpusID string, gcsURIs []string) error {
	client, err := google.DefaultClient(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return fmt.Errorf("obtaining google auth client: %w", err)
	}
	corpusResource, err := resolveCorpusResourceName(ctx, client, cfg.ProjectID, cfg.Region, corpusID)
	if err != nil {
		slog.Warn("Could not resolve corpus resource name dynamically, using fallback", "error", err)
		corpusResource = fmt.Sprintf("projects/%s/locations/%s/ragCorpora/%s", cfg.ProjectID, cfg.Region, corpusID)
	}
	endpoint := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1beta1/%s/ragFiles:import", cfg.Region, corpusResource)

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 1
	}
	chunkSize := cfg.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 1024
	}
	chunkOverlap := cfg.ChunkOverlap
	if chunkOverlap < 0 {
		chunkOverlap = 128
	}

	var importErrors []error
	totalBatches := (len(gcsURIs) + batchSize - 1) / batchSize
	for i := 0; i < len(gcsURIs); i += batchSize {
		end := i + batchSize
		if end > len(gcsURIs) {
			end = len(gcsURIs)
		}
		batch := gcsURIs[i:end]
		batchIndex := i/batchSize + 1
		slog.Info("Importing batch to RAG corpus", "batchIndex", batchIndex, "totalBatches", totalBatches, "count", len(batch), "uris", batch)

		var reqBody ragImportRequest
		reqBody.ImportRagFilesConfig.GcsSource.Uris = batch
		reqBody.ImportRagFilesConfig.RagFileChunkingConfig.FixedLengthChunking.ChunkSize = chunkSize
		reqBody.ImportRagFilesConfig.RagFileChunkingConfig.FixedLengthChunking.ChunkOverlap = chunkOverlap

		payload, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(httpReq)
		if err != nil {
			return fmt.Errorf("POST ragFiles:import failed: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
			slog.Error("ragFiles:import API error", "status", resp.StatusCode, "body", string(body))
			importErrors = append(importErrors, fmt.Errorf("ragFiles:import HTTP %d: %s", resp.StatusCode, string(body)))
			continue
		}

		var op struct {
			Name string `json:"name"`
			Done bool   `json:"done"`
		}
		if err := json.Unmarshal(body, &op); err != nil {
			slog.Warn("Failed parsing import operation response", "body", string(body), "error", err)
		}

		if op.Name != "" {
			slog.Info("Waiting for RAG import operation to finish...", "operation", op.Name, "batchIndex", batchIndex, "totalBatches", totalBatches)
			if err := waitForRagOperation(ctx, client, cfg.Region, op.Name); err != nil {
				slog.Error("RAG import batch failed", "batchIndex", batchIndex, "operation", op.Name, "error", err)
				importErrors = append(importErrors, fmt.Errorf("operation %s: %w", op.Name, err))
			}
		}

		if end < len(gcsURIs) {
			slog.Info("Cooling down before next batch to prevent rate limit exhaustion...", "cooldownSeconds", 5)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
		}
	}
	if len(importErrors) > 0 {
		return fmt.Errorf("encountered %d import errors: %w", len(importErrors), errors.Join(importErrors...))
	}
	return nil
}

const (
	ragOperationPollInterval = 10 * time.Second
	ragOperationTimeout      = 30 * time.Minute
	maxConsecutiveParseErrs  = 5
)

func waitForRagOperation(ctx context.Context, client *http.Client, region, opName string) error {
	ctx, cancel := context.WithTimeout(ctx, ragOperationTimeout)
	defer cancel()

	opURL := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1beta1/%s", region, opName)
	consecutiveParseErrs := 0
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for RAG operation %s: %w", opName, ctx.Err())
		case <-time.After(ragOperationPollInterval):
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, opURL, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var status struct {
			Done  bool `json:"done"`
			Error *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Metadata *struct {
				GenericMetadata *struct {
					PartialFailures []struct {
						Code    int    `json:"code"`
						Message string `json:"message"`
					} `json:"partialFailures"`
				} `json:"genericMetadata"`
			} `json:"metadata"`
			Response *struct {
				ImportedRagFilesCount string `json:"importedRagFilesCount"`
				FailedRagFilesCount   string `json:"failedRagFilesCount"`
				SkippedRagFilesCount  string `json:"skippedRagFilesCount"`
			} `json:"response"`
		}
		if err := json.Unmarshal(body, &status); err != nil {
			consecutiveParseErrs++
			slog.Warn("Failed parsing operation status", "error", err, "attempt", consecutiveParseErrs)
			if consecutiveParseErrs >= maxConsecutiveParseErrs {
				return fmt.Errorf("exceeded %d consecutive status parse failures for %s: %w", maxConsecutiveParseErrs, opName, err)
			}
			continue
		}
		consecutiveParseErrs = 0
		if status.Done {
			if status.Error != nil {
				return fmt.Errorf("operation failed (%d): %s", status.Error.Code, status.Error.Message)
			}
			if status.Metadata != nil && status.Metadata.GenericMetadata != nil {
				for _, pf := range status.Metadata.GenericMetadata.PartialFailures {
					slog.Warn("Partial failure in RAG import", "code", pf.Code, "message", pf.Message)
				}
			}
			if status.Response != nil {
				slog.Info("RAG import operation completed",
					"imported", status.Response.ImportedRagFilesCount,
					"failed", status.Response.FailedRagFilesCount,
					"skipped", status.Response.SkippedRagFilesCount,
					"operation", opName,
				)
				if status.Response.FailedRagFilesCount != "" && status.Response.FailedRagFilesCount != "0" {
					return fmt.Errorf("RAG import failed for %s files in operation %s", status.Response.FailedRagFilesCount, opName)
				}
			} else {
				slog.Info("RAG import operation completed successfully", "operation", opName)
			}
			return nil
		}
	}
}

func loadConfig() Config {
	projectID := os.Getenv("PROJECT_ID")
	if projectID == "" {
		projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	if projectID == "" {
		projectID = os.Getenv("GCP_PROJECT")
	}
	region := os.Getenv("VERTEX_LOCATION")
	if region == "" {
		region = os.Getenv("REGION")
	}
	if region == "" {
		if gcl := os.Getenv("GOOGLE_CLOUD_LOCATION"); gcl != "global" {
			region = gcl
		}
	}
	if region == "" {
		region = "us-central1"
	}
	bucket := os.Getenv("NAVALPLAN_PUBLICATIONS_BUCKET")
	if bucket == "" {
		bucket = os.Getenv("GCS_BUCKET")
	}
	bucket = strings.TrimPrefix(bucket, "gs://")

	batchSize := 1
	if bs := os.Getenv("RAG_BATCH_SIZE"); bs != "" {
		if n, err := strconv.Atoi(bs); err == nil && n > 0 {
			batchSize = n
		}
	}
	chunkSize := 1024
	if cs := os.Getenv("RAG_CHUNK_SIZE"); cs != "" {
		if n, err := strconv.Atoi(cs); err == nil && n > 0 {
			chunkSize = n
		}
	}
	chunkOverlap := 128
	if co := os.Getenv("RAG_CHUNK_OVERLAP"); co != "" {
		if n, err := strconv.Atoi(co); err == nil && n >= 0 {
			chunkOverlap = n
		}
	}

	lnmCorpusID := os.Getenv("LNM_CORPUS_ID")
	if lnmCorpusID == "" {
		lnmCorpusID = os.Getenv("NAVALPLAN_LNM_CORPUS_ID")
	}
	lnmIndexURL := os.Getenv("LNM_INDEX_URL")
	if lnmIndexURL == "" {
		lnmIndexURL = defaultUSCGIndexURL
	}
	syncTarget := strings.ToLower(strings.TrimSpace(os.Getenv("SYNC_TARGET")))
	if syncTarget == "" {
		syncTarget = "all"
	}

	return Config{
		ProjectID:          projectID,
		Region:             region,
		GCSBucket:          bucket,
		CoastPilotCorpusID: os.Getenv("COAST_PILOT_CORPUS_ID"),
		NGACorpusID:        os.Getenv("NGA_CORPUS_ID"),
		LNMCorpusID:        lnmCorpusID,
		LNMIndexURL:        lnmIndexURL,
		SyncTarget:         syncTarget,
		BatchSize:          batchSize,
		ChunkSize:          chunkSize,
		ChunkOverlap:       chunkOverlap,
	}
}

func syncCoastPilot(ctx context.Context, client *storage.Client, bucketName string) error {
	httpClient := &http.Client{Timeout: 5 * time.Minute}
	for vol := 1; vol <= 10; vol++ {
		url := fmt.Sprintf("https://nauticalcharts.noaa.gov/publications/coast-pilot/files/cp%d/CPB%d_WEB.pdf", vol, vol)
		destObj := fmt.Sprintf("noaa-coast-pilot/latest/CPB%d_WEB.pdf", vol)
		slog.Info("Downloading NOAA Coast Pilot", "volume", vol, "url", url, "dest", destObj)
		if err := streamDownloadToGCS(ctx, httpClient, client, url, bucketName, destObj); err != nil {
			slog.Error("Failed downloading Coast Pilot volume", "volume", vol, "error", err)
		}
	}
	return nil
}

func syncNGAPublications(ctx context.Context, client *storage.Client, bucketName string) ([]string, error) {
	httpClient := &http.Client{Timeout: 5 * time.Minute}
	pubTypes := []struct {
		TypeID int
		Name   string
		Folder string
	}{
		{TypeID: 21, Name: "Planning Guides", Folder: "planning"},
		{TypeID: 22, Name: "Enroute", Folder: "enroute"},
	}

	var syncedURIs []string
	for _, pt := range pubTypes {
		apiURL := fmt.Sprintf("https://msi.nga.mil/api/publications/stored-pubs?pubTypeId=%d", pt.TypeID)
		slog.Info("Fetching NGA publication details", "pubType", pt.Name, "url", apiURL)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		req.Header.Set("Referer", "https://msi.nga.mil/Publications/SDEnroute")

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetching NGA list for %s: %w", pt.Name, err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("NGA API returned status %d for %s: %s", resp.StatusCode, pt.Name, string(body))
		}

		var pubs []NGAStoredPub
		if err := json.NewDecoder(resp.Body).Decode(&pubs); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decoding NGA response for %s: %w", pt.Name, err)
		}
		resp.Body.Close()

		slog.Info("Discovered NGA publications", "pubType", pt.Name, "count", len(pubs))
		for _, pub := range pubs {
			if pub.S3Key == "" {
				continue
			}
			filename := pub.FullFilename
			if filename == "" {
				filename = pub.SectionName + ".pdf"
			}
			downloadURL := fmt.Sprintf("https://msi.nga.mil/api/publications/download?key=%s&type=download", pub.S3Key)
			destObj := fmt.Sprintf("nga-sailing-directions/%s/%s", pt.Folder, filename)
			slog.Info("Downloading NGA publication", "name", pub.SectionDisplayName, "dest", destObj)
			if err := streamDownloadToGCS(ctx, httpClient, client, downloadURL, bucketName, destObj); err != nil {
				slog.Error("Failed syncing NGA publication", "name", pub.SectionDisplayName, "error", err)
				continue
			}
			syncedURIs = append(syncedURIs, fmt.Sprintf("gs://%s/%s", bucketName, destObj))
		}
	}
	return syncedURIs, nil
}

func streamDownloadToGCS(ctx context.Context, httpClient *http.Client, gcsClient *storage.Client, fileURL, bucket, objectPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36")
	if strings.Contains(fileURL, "nga.mil") {
		// msi.nga.mil rejects non-browser HTTP clients without browser User-Agent and Referer headers.
		req.Header.Set("Referer", "https://msi.nga.mil/Publications/SDEnroute")
	} else if strings.Contains(fileURL, "uscg.gov") {
		req.Header.Set("Referer", "https://www.navcen.uscg.gov/local-notices-to-mariners-by-cg-district")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http get %s: %w", fileURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http get %s returned status %d", fileURL, resp.StatusCode)
	}

	wc := gcsClient.Bucket(bucket).Object(objectPath).NewWriter(ctx)
	wc.ContentType = "application/pdf"
	if _, err := io.Copy(wc, resp.Body); err != nil {
		wc.Close()
		return fmt.Errorf("copying to GCS %s: %w", objectPath, err)
	}
	return wc.Close()
}

var (
	hrefAttrRe     = regexp.MustCompile(`(?i)(?:xlink:)?href\s*=\s*["']([^"']+)["']`)
	lnmFilenameRe  = regexp.MustCompile(`(?i)^lnm(\d{2})([a-z]?)(\d{2})(\d{4})\.pdf$`)
	districtPathRe = regexp.MustCompile(`(?i)\b(d0?[1-9]|d1[0-9]|district[-_\s]*[0-9]{1,2})\b`)
	safeFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
)

// defaultUSCGDistrictPages defines all US Coast Guard district LNM pages on navcen.uscg.gov.
var defaultUSCGDistrictPages = []struct {
	District string
	Prefix   string
	URL      string
}{
	{District: "d1", Prefix: "01", URL: "https://www.navcen.uscg.gov/local-notices-to-mariners?district=1+0&subdistrict=n"},
	{District: "d5", Prefix: "05", URL: "https://www.navcen.uscg.gov/local-notices-to-mariners?district=5+0&subdistrict=n"},
	{District: "d7", Prefix: "07", URL: "https://www.navcen.uscg.gov/local-notices-to-mariners?district=7+0&subdistrict=n"},
	{District: "d8g", Prefix: "08g", URL: "https://www.navcen.uscg.gov/local-notices-to-mariners?district=8+0&subdistrict=g"},
	{District: "d8r", Prefix: "08r", URL: "https://www.navcen.uscg.gov/local-notices-to-mariners?district=8+0&subdistrict=r"},
	{District: "d9", Prefix: "09", URL: "https://www.navcen.uscg.gov/local-notices-to-mariners?district=9+0&subdistrict=n"},
	{District: "d11", Prefix: "11", URL: "https://www.navcen.uscg.gov/local-notices-to-mariners?district=11+0&subdistrict=n"},
	{District: "d13", Prefix: "13", URL: "https://www.navcen.uscg.gov/local-notices-to-mariners?district=13+0&subdistrict=n"},
	{District: "d14", Prefix: "14", URL: "https://www.navcen.uscg.gov/local-notices-to-mariners?district=14+0&subdistrict=n"},
	{District: "d17", Prefix: "17", URL: "https://www.navcen.uscg.gov/local-notices-to-mariners?district=17+0&subdistrict=n"},
}

type lnmDocument struct {
	District string
	Filename string
	URL      string
	Year     int
	Week     int
}

func discoverLNMDocuments(ctx context.Context, httpClient *http.Client, indexURL string) ([]lnmDocument, error) {
	if indexURL == "" {
		indexURL = defaultUSCGIndexURL
	}
	body, err := fetchHTMLPage(ctx, httpClient, indexURL)
	if err != nil {
		return nil, fmt.Errorf("fetching LNM index %s: %w", indexURL, err)
	}

	pdfLinks, subpageLinks := extractLNMLinks(indexURL, body)
	// If the index page is the NAVCEN district map or had no subpages, ensure all standard district pages are checked.
	if len(subpageLinks) == 0 && strings.Contains(indexURL, "navcen.uscg.gov") {
		for _, dp := range defaultUSCGDistrictPages {
			subpageLinks = append(subpageLinks, dp.URL)
		}
	}

	seenURLs := make(map[string]bool)
	var allDocs []lnmDocument

	addPDF := func(rawURL string) {
		if seenURLs[rawURL] {
			return
		}
		seenURLs[rawURL] = true
		doc := parseLNMDocument(rawURL)
		allDocs = append(allDocs, doc)
	}

	for _, u := range pdfLinks {
		addPDF(u)
	}

	for _, subURL := range subpageLinks {
		subBody, err := fetchHTMLPage(ctx, httpClient, subURL)
		if err != nil {
			slog.Warn("Failed fetching LNM district subpage", "url", subURL, "error", err)
			continue
		}
		subPDFs, _ := extractLNMLinks(subURL, subBody)
		for _, u := range subPDFs {
			addPDF(u)
		}
	}

	return selectLatestLNMDocuments(allDocs), nil
}

// selectLatestLNMDocuments keeps the most recent weekly edition PDF per district (to avoid
// re-importing every historical week of the year on each weekly sync) while retaining any
// active special notices or non-weekly PDFs if no weekly edition was matched for a district.
func selectLatestLNMDocuments(docs []lnmDocument) []lnmDocument {
	latestWeekly := make(map[string]lnmDocument)
	var otherDocs []lnmDocument

	for _, d := range docs {
		if d.Year > 0 && d.Week > 0 {
			cur, ok := latestWeekly[d.District]
			if !ok || d.Year > cur.Year || (d.Year == cur.Year && d.Week > cur.Week) {
				latestWeekly[d.District] = d
			}
		} else {
			// Skip generic administrative forms
			lowerName := strings.ToLower(d.Filename)
			if strings.Contains(lowerName, "information_form") || strings.Contains(lowerName, "userguide") {
				continue
			}
			otherDocs = append(otherDocs, d)
		}
	}

	if len(latestWeekly) == 0 {
		return otherDocs
	}

	var result []lnmDocument
	for _, dp := range defaultUSCGDistrictPages {
		if doc, ok := latestWeekly[dp.District]; ok {
			result = append(result, doc)
			delete(latestWeekly, dp.District)
		}
	}
	for _, doc := range latestWeekly {
		result = append(result, doc)
	}
	return result
}

func fetchHTMLPage(ctx context.Context, httpClient *http.Client, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", err
	}
	setBrowserHeaders(req, "https://www.navcen.uscg.gov/local-notices-to-mariners-by-cg-district")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, pageURL)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func extractLNMLinks(baseURL, htmlBody string) (pdfURLs []string, subpageURLs []string) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, nil
	}
	seenPDF := make(map[string]bool)
	seenSub := make(map[string]bool)

	// Unescape HTML entities like &amp; in xlink:href="/local-notices-to-mariners?district=1+0&amp;subdistrict=n"
	cleanedBody := strings.ReplaceAll(htmlBody, "&amp;", "&")
	matches := hrefAttrRe.FindAllStringSubmatch(cleanedBody, -1)
	for _, m := range matches {
		raw := strings.TrimSpace(m[1])
		if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(strings.ToLower(raw), "javascript:") || strings.HasPrefix(strings.ToLower(raw), "mailto:") {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil {
			continue
		}
		resolved := base.ResolveReference(parsed)
		fullURL := resolved.String()
		lowerPath := strings.ToLower(resolved.Path)
		lowerFull := strings.ToLower(fullURL)

		if strings.HasSuffix(lowerPath, ".pdf") && (strings.Contains(lowerFull, "/lnm") || strings.Contains(lowerFull, "lnm")) {
			if !seenPDF[fullURL] {
				seenPDF[fullURL] = true
				pdfURLs = append(pdfURLs, fullURL)
			}
			continue
		}

		// Follow LNM district subpages on either navcen.uscg.gov or dco.uscg.mil
		isDistrictSubpage := (strings.Contains(lowerPath, "/local-notices-to-mariners") && strings.Contains(strings.ToLower(resolved.RawQuery), "district=")) ||
			strings.Contains(lowerPath, "/local-notice-to-mariners-lnms/")
		if isDistrictSubpage && strings.TrimRight(fullURL, "/") != strings.TrimRight(baseURL, "/") {
			if !seenSub[fullURL] {
				seenSub[fullURL] = true
				subpageURLs = append(subpageURLs, fullURL)
			}
		}
	}
	return pdfURLs, subpageURLs
}

func parseLNMDocument(rawURL string) lnmDocument {
	district := "general"
	u, err := url.Parse(rawURL)
	var urlPath string
	if err == nil {
		urlPath = u.Path
	} else {
		urlPath = rawURL
	}

	base := path.Base(urlPath)
	if base == "" || base == "." || base == "/" {
		base = "lnm.pdf"
	}
	if !strings.HasSuffix(strings.ToLower(base), ".pdf") {
		base += ".pdf"
	}
	base = safeFilenameRe.ReplaceAllString(base, "_")

	var year, week int
	if m := lnmFilenameRe.FindStringSubmatch(base); len(m) == 5 {
		distNum := strings.TrimLeft(m[1], "0")
		if distNum == "" {
			distNum = "0"
		}
		sub := strings.ToLower(m[2])
		district = "d" + distNum + sub
		week, _ = strconv.Atoi(m[3])
		year, _ = strconv.Atoi(m[4])
	} else if m := districtPathRe.FindString(strings.ToLower(urlPath)); m != "" {
		cleaned := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(m), "district", "d"), " ", "")
		cleaned = strings.ReplaceAll(cleaned, "-", "")
		cleaned = strings.ReplaceAll(cleaned, "_", "")
		if strings.HasPrefix(cleaned, "d0") && len(cleaned) > 2 {
			cleaned = "d" + strings.TrimPrefix(cleaned, "d0")
		}
		district = cleaned
	}

	return lnmDocument{
		District: district,
		Filename: base,
		URL:      rawURL,
		Year:     year,
		Week:     week,
	}
}

func syncUSCGLNM(ctx context.Context, client *storage.Client, bucketName, indexURL string) ([]string, error) {
	httpClient := &http.Client{Timeout: 5 * time.Minute}
	docs, err := discoverLNMDocuments(ctx, httpClient, indexURL)
	if err != nil {
		slog.Warn("Primary USCG LNM index discovery failed, attempting direct NAVCEN district pages", "error", err)
		for _, dp := range defaultUSCGDistrictPages {
			subDocs, subErr := discoverLNMDocuments(ctx, httpClient, dp.URL)
			if subErr != nil {
				slog.Warn("Failed fetching fallback NAVCEN district page", "district", dp.District, "url", dp.URL, "error", subErr)
				continue
			}
			docs = append(docs, subDocs...)
		}
		docs = selectLatestLNMDocuments(docs)
	}

	var syncedURIs []string
	seenDest := make(map[string]bool)
	syncedDistricts := make(map[string]bool)
	for _, doc := range docs {
		destObj := fmt.Sprintf("uscg-lnm/latest/%s/%s", doc.District, doc.Filename)
		if seenDest[destObj] {
			continue
		}
		slog.Info("Downloading USCG LNM publication", "district", doc.District, "week", doc.Week, "year", doc.Year, "url", doc.URL, "dest", destObj)
		if err := streamDownloadToGCS(ctx, httpClient, client, doc.URL, bucketName, destObj); err != nil {
			slog.Warn("Failed syncing USCG LNM publication", "district", doc.District, "url", doc.URL, "error", err)
			continue
		}
		seenDest[destObj] = true
		syncedDistricts[doc.District] = true
		syncedURIs = append(syncedURIs, fmt.Sprintf("gs://%s/%s", bucketName, destObj))
	}

	// If HTML index scraping was blocked by WAF (403) or missed districts, directly probe
	// the deterministic NAVCEN weekly PDF URLs:
	// https://navcen.uscg.gov/sites/default/files/pdf/lnms/lnm{DD}{WW}{YYYY}.pdf
	// for each missing district across the current and recent ISO weeks.
	directURIs := syncDirectWeeklyLNMPDFs(ctx, httpClient, client, bucketName, syncedDistricts, time.Now().UTC())
	syncedURIs = append(syncedURIs, directURIs...)

	if len(syncedURIs) == 0 {
		slog.Info("No weekly LNM PDFs reachable from current network; synthesizing LNM text from live USCG NAVCEN MSI GeoJSON feeds...")
		mdURIs, mdErr := syncUSCGLNMGeoJSONFeeds(ctx, httpClient, client, bucketName)
		if mdErr != nil {
			return nil, fmt.Errorf("syncing USCG LNM GeoJSON feeds fallback: %w", mdErr)
		}
		syncedURIs = append(syncedURIs, mdURIs...)
	} else {
		// Also sync the live structured GeoJSON feeds alongside the weekly PDFs so both
		// the full weekly district PDFs and any mid-week discrepancy updates are indexed.
		if mdURIs, mdErr := syncUSCGLNMGeoJSONFeeds(ctx, httpClient, client, bucketName); mdErr == nil {
			syncedURIs = append(syncedURIs, mdURIs...)
		}
	}
	return syncedURIs, nil
}

// candidateLNMWeekYears returns up to maxWeeks recent (year, week) pairs in reverse
// chronological order starting from the ISO week of now.
func candidateLNMWeekYears(now time.Time, maxWeeks int) [][2]int {
	var out [][2]int
	seen := make(map[[2]int]bool)
	for i := 0; i < maxWeeks; i++ {
		t := now.AddDate(0, 0, -7*i)
		y, w := t.ISOWeek()
		key := [2]int{y, w}
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

// syncDirectWeeklyLNMPDFs directly downloads the latest available weekly LNM PDF for each
// USCG district using the deterministic NAVCEN static path
// https://navcen.uscg.gov/sites/default/files/pdf/lnms/lnm{prefix}{WW}{YYYY}.pdf,
// bypassing Drupal HTML page WAF blocks.
func syncDirectWeeklyLNMPDFs(ctx context.Context, httpClient *http.Client, gcsClient *storage.Client, bucketName string, skipDistricts map[string]bool, now time.Time) []string {
	candidates := candidateLNMWeekYears(now, 8)
	var syncedURIs []string

	for _, dp := range defaultUSCGDistrictPages {
		if skipDistricts[dp.District] {
			continue
		}
		var prefixes []string
		prefixes = append(prefixes, dp.Prefix)
		// Heartland District 8 sometimes publishes a combined 08 file or subdistrict 08g/08r files.
		if dp.District == "d8g" {
			prefixes = append(prefixes, "08")
		}

		downloaded := false
		for _, wy := range candidates {
			year, week := wy[0], wy[1]
			for _, pfx := range prefixes {
				filename := fmt.Sprintf("lnm%s%02d%04d.pdf", pfx, week, year)
				destObj := fmt.Sprintf("uscg-lnm/latest/%s/%s", dp.District, filename)
				var lastErr error
				for _, host := range []string{"https://navcen.uscg.gov", "https://www.navcen.uscg.gov"} {
					pdfURL := fmt.Sprintf("%s/sites/default/files/pdf/lnms/%s", host, filename)
					slog.Info("Trying direct NAVCEN weekly LNM PDF", "district", dp.District, "week", week, "year", year, "url", pdfURL)
					if err := streamDownloadToGCS(ctx, httpClient, gcsClient, pdfURL, bucketName, destObj); err != nil {
						lastErr = err
						continue
					}
					lastErr = nil
					break
				}
				if lastErr != nil {
					slog.Warn("Direct NAVCEN weekly LNM PDF failed", "district", dp.District, "week", week, "year", year, "file", filename, "error", lastErr)
					continue
				}
				slog.Info("Synced direct NAVCEN weekly LNM PDF", "district", dp.District, "week", week, "year", year, "dest", destObj)
				syncedURIs = append(syncedURIs, fmt.Sprintf("gs://%s/%s", bucketName, destObj))
				downloaded = true
				break
			}
			if downloaded {
				break
			}
		}

		// If Cloud Run's egress IP was blocked by NAVCEN WAF (403), check if a pre-staged
		// weekly PDF already exists in GCS under gs://<bucket>/uscg-lnm/latest/<district>/
		// so Vertex AI RAG still imports it.
		if !downloaded {
			prefix := fmt.Sprintf("uscg-lnm/latest/%s/", dp.District)
			it := gcsClient.Bucket(bucketName).Objects(ctx, &storage.Query{Prefix: prefix})
			for {
				attrs, err := it.Next()
				if err != nil {
					break
				}
				if strings.HasSuffix(strings.ToLower(attrs.Name), ".pdf") {
					uri := fmt.Sprintf("gs://%s/%s", bucketName, attrs.Name)
					slog.Info("Found existing USCG LNM PDF in GCS for RAG import", "district", dp.District, "uri", uri)
					syncedURIs = append(syncedURIs, uri)
					break
				}
			}
		}
	}
	return syncedURIs
}

var uscgLNMGeoJSONCategories = []struct {
	Prefix string
	Title  string
}{
	{Prefix: "discFedAid_", Title: "Discrepant Federal Aids to Navigation"},
	{Prefix: "discPriAid_", Title: "Discrepant Private Aids to Navigation"},
	{Prefix: "tmpChange_", Title: "Temporary Changes to Aids to Navigation"},
	{Prefix: "bridge_", Title: "Bridge Notices and Restrictions"},
	{Prefix: "bridgePoly_", Title: "Bridge Construction and Safety Polygons"},
	{Prefix: "hazNav_", Title: "Hazards to Navigation"},
	{Prefix: "hazNavLine_", Title: "Hazards to Navigation Lines"},
	{Prefix: "hazNavPoly_", Title: "Hazards to Navigation Areas"},
	{Prefix: "marCon_", Title: "Marine Construction and Dredging"},
	{Prefix: "marConLine_", Title: "Marine Construction and Dredging Lines"},
	{Prefix: "marConPoly_", Title: "Marine Construction and Dredging Areas"},
	{Prefix: "safeZone_", Title: "Safety and Security Zones"},
	{Prefix: "safeZoneLine_", Title: "Safety and Security Zone Lines"},
	{Prefix: "safeZonePoly_", Title: "Safety and Security Zone Areas"},
	{Prefix: "marEvent_", Title: "Marine Events and Regattas"},
	{Prefix: "marEventLine_", Title: "Marine Event Lines"},
	{Prefix: "marEventPoly_", Title: "Marine Event Areas"},
	{Prefix: "msib_", Title: "Marine Safety Information Bulletins (MSIB)"},
	{Prefix: "msibPoly_", Title: "Marine Safety Information Bulletin Areas"},
	{Prefix: "navalAct_", Title: "Naval Activity"},
	{Prefix: "navalActPoly_", Title: "Naval Activity Areas"},
	{Prefix: "change_", Title: "Permanent Aid to Navigation Changes"},
	{Prefix: "changeAdvNot_", Title: "Advance Notice of Aid Changes"},
	{Prefix: "changeProp_", Title: "Proposed Aid to Navigation Changes"},
	{Prefix: "misc_", Title: "Miscellaneous Local Notices to Mariners"},
	{Prefix: "miscPoly_", Title: "Miscellaneous LNM Areas"},
}

type geoJSONFeatureCollection struct {
	Features []struct {
		Properties map[string]any `json:"properties"`
		Geometry   struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		} `json:"geometry"`
	} `json:"features"`
}

const maxRagMarkdownBytes = 1500000 // 1.5 MB per file (well under Vertex AI RAG's 2.5 MB limit for .md/.txt)

func syncUSCGLNMGeoJSONFeeds(ctx context.Context, httpClient *http.Client, gcsClient *storage.Client, bucketName string) ([]string, error) {
	indexBody, err := fetchHTMLPage(ctx, httpClient, "https://www.navcen.uscg.gov/sites/default/files/msi/fileIndexNew.json")
	if err != nil {
		return nil, fmt.Errorf("fetching fileIndexNew.json: %w", err)
	}

	var fileIndex map[string]struct {
		Counter   int   `json:"counter"`
		Timestamp int64 `json:"timestamp"`
	}
	if err := json.Unmarshal([]byte(indexBody), &fileIndex); err != nil {
		return nil, fmt.Errorf("decoding fileIndexNew.json: %w", err)
	}

	type partBuffer struct {
		part int
		sb   *strings.Builder
	}
	byDistrict := make(map[string][]*partBuffer)
	getBuilder := func(district string) *strings.Builder {
		if district == "" {
			district = "general"
		}
		parts := byDistrict[district]
		if len(parts) == 0 || parts[len(parts)-1].sb.Len() >= maxRagMarkdownBytes {
			partNum := len(parts) + 1
			sb := &strings.Builder{}
			sb.WriteString(fmt.Sprintf("# USCG Local Notice to Mariners (LNM) — District %s (Part %d)\n", strings.ToUpper(district), partNum))
			sb.WriteString(fmt.Sprintf("Synced: %s\n\n", time.Now().UTC().Format(time.RFC3339)))
			pb := &partBuffer{part: partNum, sb: sb}
			byDistrict[district] = append(parts, pb)
			return sb
		}
		return parts[len(parts)-1].sb
	}

	var totalFeatures int
	for _, cat := range uscgLNMGeoJSONCategories {
		meta, ok := fileIndex[cat.Prefix]
		if !ok || meta.Counter <= 0 {
			continue
		}
		for i := 1; i <= meta.Counter; i++ {
			feedURL := fmt.Sprintf("https://www.navcen.uscg.gov/sites/default/files/msi/%s%d.geojson", cat.Prefix, i)
			raw, err := fetchHTMLPage(ctx, httpClient, feedURL)
			if err != nil {
				slog.Warn("Failed fetching USCG MSI GeoJSON feed", "url", feedURL, "error", err)
				continue
			}
			var fc geoJSONFeatureCollection
			if err := json.Unmarshal([]byte(raw), &fc); err != nil {
				slog.Warn("Failed decoding USCG MSI GeoJSON feed", "url", feedURL, "error", err)
				continue
			}
			for _, feat := range fc.Features {
				if len(feat.Properties) == 0 {
					continue
				}
				dist := extractGeoJSONDistrict(feat.Properties)
				b := getBuilder(dist)
				b.WriteString(fmt.Sprintf("## %s\n", cat.Title))
				for k, v := range feat.Properties {
					if v == nil {
						continue
					}
					valStr := strings.TrimSpace(fmt.Sprintf("%v", v))
					if valStr == "" || valStr == "<nil>" {
						continue
					}
					b.WriteString(fmt.Sprintf("- **%s**: %s\n", k, valStr))
				}
				if len(feat.Geometry.Coordinates) > 0 && len(feat.Geometry.Coordinates) < 256 {
					b.WriteString(fmt.Sprintf("- **coordinates**: %s (%s)\n", string(feat.Geometry.Coordinates), feat.Geometry.Type))
				}
				b.WriteString("\n")
				totalFeatures++
			}
		}
	}

	if totalFeatures == 0 {
		return nil, fmt.Errorf("no LNM features extracted from USCG MSI GeoJSON feeds")
	}

	var syncedURIs []string
	for dist, parts := range byDistrict {
		for _, pb := range parts {
			destObj := fmt.Sprintf("uscg-lnm/latest/%s/uscg_lnm_%s_part%d.txt", dist, dist, pb.part)
			wc := gcsClient.Bucket(bucketName).Object(destObj).NewWriter(ctx)
			wc.ContentType = "text/plain"
			if _, err := io.WriteString(wc, pb.sb.String()); err != nil {
				wc.Close()
				slog.Warn("Failed writing LNM text to GCS", "district", dist, "part", pb.part, "error", err)
				continue
			}
			if err := wc.Close(); err != nil {
				slog.Warn("Failed closing LNM text GCS writer", "district", dist, "part", pb.part, "error", err)
				continue
			}
			slog.Info("Uploaded USCG LNM district text to GCS", "district", dist, "part", pb.part, "bytes", pb.sb.Len(), "dest", destObj)
			syncedURIs = append(syncedURIs, fmt.Sprintf("gs://%s/%s", bucketName, destObj))
		}
	}
	return syncedURIs, nil
}

func extractGeoJSONDistrict(props map[string]any) string {
	for k, v := range props {
		if v == nil {
			continue
		}
		lk := strings.ToLower(k)
		if lk == "district" || lk == "cg_district" || lk == "cgd" || lk == "dist" || strings.Contains(lk, "district") {
			s := strings.TrimSpace(fmt.Sprintf("%v", v))
			s = strings.TrimLeft(s, "0")
			if s != "" && s != "<nil>" {
				if !strings.HasPrefix(strings.ToLower(s), "d") {
					return "d" + strings.ToLower(s)
				}
				return strings.ToLower(s)
			}
		}
	}
	return "general"
}
