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
	"os"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"golang.org/x/oauth2/google"
)

type Config struct {
	ProjectID          string
	Region             string
	GCSBucket          string
	CoastPilotCorpusID string
	NGACorpusID        string
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

	slog.Info("Starting nautical data sync job", "project", cfg.ProjectID, "region", cfg.Region, "bucket", cfg.GCSBucket)

	storageClient, err := storage.NewClient(ctx)
	if err != nil {
		slog.Error("Failed to initialize Cloud Storage client", "error", err)
		os.Exit(1)
	}
	defer storageClient.Close()

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
	return fmt.Sprintf("projects/%s/locations/%s/ragCorpora/%s", projectID, region, corpusID), nil
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

func waitForRagOperation(ctx context.Context, client *http.Client, region, opName string) error {
	opURL := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1beta1/%s", region, opName)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
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
			slog.Warn("Failed parsing operation status", "error", err)
			continue
		}
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
		region = os.Getenv("GOOGLE_CLOUD_LOCATION")
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

	return Config{
		ProjectID:          projectID,
		Region:             region,
		GCSBucket:          bucket,
		CoastPilotCorpusID: os.Getenv("COAST_PILOT_CORPUS_ID"),
		NGACorpusID:        os.Getenv("NGA_CORPUS_ID"),
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
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://msi.nga.mil/Publications/SDEnroute")

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
