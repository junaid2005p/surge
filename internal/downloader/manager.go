package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"surge/internal/messages"
	"surge/internal/utils"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

var probeClient = &http.Client{Timeout: ProbeTimeout}

var ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) " +
	"Chrome/120.0.0.0 Safari/537.36"

// ProbeResult contains all metadata from server probe
type ProbeResult struct {
	FileSize      int64
	SupportsRange bool
	Filename      string
	ContentType   string
}

// probeServer sends GET with Range: bytes=0-0 to determine server capabilities
func probeServer(ctx context.Context, rawurl string, filenameHint string) (*ProbeResult, error) {
	utils.Debug("Probing server: %s", rawurl)

	var resp *http.Response
	var err error

	// Retry logic for probe request
	for i := 0; i < 3; i++ {
		if i > 0 {
			time.Sleep(1 * time.Second)
			utils.Debug("Retrying probe... attempt %d", i+1)
		}

		probeCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
		defer cancel()

		req, reqErr := http.NewRequestWithContext(probeCtx, http.MethodGet, rawurl, nil)
		if reqErr != nil {
			err = fmt.Errorf("failed to create probe request: %w", reqErr)
			break // Fatal error, don't retry
		}

		req.Header.Set("Range", "bytes=0-0")
		req.Header.Set("User-Agent", ua)

		resp, err = probeClient.Do(req)
		if err == nil {
			break // Success
		}
	}

	if err != nil {
		return nil, fmt.Errorf("probe request failed after retries: %w", err)
	}

	defer func() {
		io.Copy(io.Discard, resp.Body) // Drain any remaining data
		resp.Body.Close()
	}()

	utils.Debug("Probe response status: %d", resp.StatusCode)

	result := &ProbeResult{}

	// Determine range support and file size based on status code
	switch resp.StatusCode {
	case http.StatusPartialContent: // 206
		result.SupportsRange = true
		// Parse Content-Range: bytes 0-0/TOTAL
		contentRange := resp.Header.Get("Content-Range")
		utils.Debug("Content-Range header: %s", contentRange)
		if contentRange != "" {
			// Format: "bytes 0-0/12345" or "bytes 0-0/*"
			if idx := strings.LastIndex(contentRange, "/"); idx != -1 {
				sizeStr := contentRange[idx+1:]
				if sizeStr != "*" {
					result.FileSize, _ = strconv.ParseInt(sizeStr, 10, 64)
				}
			}
		}
		utils.Debug("Range supported, file size: %d", result.FileSize)

	case http.StatusOK: // 200 - server ignores Range header
		result.SupportsRange = false
		contentLength := resp.Header.Get("Content-Length")
		if contentLength != "" {
			result.FileSize, _ = strconv.ParseInt(contentLength, 10, 64)
		}
		utils.Debug("Range NOT supported (got 200), file size: %d", result.FileSize)

	default:
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Determine filename using strengthened logic
	name, _, err := utils.DetermineFilename(rawurl, resp, false)
	if err != nil {
		utils.Debug("Error determining filename: %v", err)
		name = "download.bin"
	}

	if filenameHint != "" {
		result.Filename = filenameHint
	} else {
		result.Filename = name
	}

	result.ContentType = resp.Header.Get("Content-Type")

	utils.Debug("Probe complete - filename: %s, size: %d, range: %v",
		result.Filename, result.FileSize, result.SupportsRange)

	return result, nil
}

// TUIDownload is the main entry point for TUI downloads
func TUIDownload(ctx context.Context, cfg DownloadConfig) error {

	// Start download timer
	start := time.Now()
	defer func() {
		utils.Debug("Download %s completed in %v", cfg.URL, time.Since(start))
	}()

	// Probe server once to get all metadata
	probe, err := probeServer(ctx, cfg.URL, cfg.Filename)
	if err != nil {
		utils.Debug("Probe failed: %v", err)
		return err
	}

	// Construct proper output path
	destPath := cfg.OutputPath
	if info, err := os.Stat(cfg.OutputPath); err == nil && info.IsDir() {
		destPath = filepath.Join(cfg.OutputPath, probe.Filename)
	}
	utils.Debug("Destination path: %s", destPath)

	// Send download started message
	if cfg.ProgressCh != nil {
		cfg.ProgressCh <- messages.DownloadStartedMsg{
			DownloadID: cfg.ID,
			URL:        cfg.URL,
			Filename:   probe.Filename,
			Total:      probe.FileSize,
			DestPath:   destPath,
		}
	}

	// Update shared state
	if cfg.State != nil {
		cfg.State.SetTotalSize(probe.FileSize)
	}

	// Choose downloader based on probe results
	if probe.SupportsRange && probe.FileSize > 0 {
		utils.Debug("Using concurrent downloader")
		d := NewConcurrentDownloader(cfg.ID, cfg.ProgressCh, cfg.State)
		return d.Download(ctx, cfg.URL, destPath, probe.FileSize, cfg.Verbose)
	}

	// Fallback to single-threaded downloader
	utils.Debug("Using single-threaded downloader")
	d := NewSingleDownloader(cfg.ID, cfg.ProgressCh, cfg.State)
	return d.Download(ctx, cfg.URL, destPath, probe.FileSize, probe.Filename, cfg.Verbose)
}

// Download is the CLI entry point (non-TUI) - convenience wrapper
func Download(ctx context.Context, url, outPath string, verbose bool, md5sum, sha256sum string, progressCh chan<- tea.Msg, id int) error {
	cfg := DownloadConfig{
		URL:        url,
		OutputPath: outPath,
		ID:         id,
		Verbose:    verbose,
		MD5Sum:     md5sum,
		SHA256Sum:  sha256sum,
		ProgressCh: progressCh,
		State:      nil,
	}
	return TUIDownload(ctx, cfg)
}
