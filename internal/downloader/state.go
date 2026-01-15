package downloader

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"surge/internal/config"
)

// masterListMu protects concurrent access to the master list file
var masterListMu sync.Mutex

// URLHash returns a short hash of the URL for master list keying
// This is used for tracking completed downloads by URL
func URLHash(url string) string {
	h := sha256.Sum256([]byte(url))
	return hex.EncodeToString(h[:8]) // 16 chars
}

// StateHash returns a unique hash for state file naming using URL and destination path
// This ensures multiple downloads of the same URL get separate state files
func StateHash(url string, destPath string) string {
	h := sha256.Sum256([]byte(url + "|" + destPath))
	return hex.EncodeToString(h[:8]) // 16 chars
}

// DownloadState represents persisted download state for resume
type DownloadState struct {
	ID         string `json:"id"`       // Unique ID of the download
	URLHash    string `json:"url_hash"` // Hash of URL only (for master list compatibility)
	URL        string `json:"url"`
	DestPath   string `json:"dest_path"`
	TotalSize  int64  `json:"total_size"`
	Downloaded int64  `json:"downloaded"`
	Tasks      []Task `json:"tasks"` // Remaining tasks
	Filename   string `json:"filename"`
	CreatedAt  int64  `json:"created_at"` // Unix timestamp
	PausedAt   int64  `json:"paused_at"`  // Unix timestamp
}

// getStatePath returns the path to the state file using URL+DestPath hash
// This ensures multiple downloads of the same URL get separate state files
func getStatePath(url string, destPath string) string {
	return filepath.Join(config.GetStateDir(), StateHash(url, destPath)+".json")
}

// getSurgeDir returns the global surge state directory
func getSurgeDir() string {
	return config.GetStateDir()
}

// SaveState saves download state to global surge state directory
// Uses URL+destPath for unique state file naming
func SaveState(url string, destPath string, state *DownloadState) error {
	statePath := getStatePath(url, destPath)

	// Create state directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(statePath), 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	// Set hashes and timestamps
	state.URLHash = URLHash(url)
	state.PausedAt = time.Now().Unix()
	if state.CreatedAt == 0 {
		state.CreatedAt = time.Now().Unix()
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(statePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	// Also update master list (uses StateHash for unique identification)
	entry := DownloadEntry{
		ID:       state.ID,
		URLHash:  state.URLHash,
		URL:      state.URL,
		DestPath: state.DestPath,
		Filename: state.Filename,
		Status:   "paused",
	}
	_ = AddToMasterList(entry)

	return nil
}

// LoadState loads download state from global surge state directory
// Uses URL+destPath for unique state file lookup
func LoadState(url string, destPath string) (*DownloadState, error) {
	statePath := getStatePath(url, destPath)

	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state DownloadState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return &state, nil
}

// DeleteState removes the state file after successful completion
// Uses URL+destPath for unique state file identification
func DeleteState(id string, url string, destPath string) error {
	statePath := getStatePath(url, destPath)

	if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete state file: %w", err)
	}

	// Remove from master list using ID
	_ = RemoveFromMasterList(id)

	return nil
}

// DeleteStateByURL removes state file by URL and destPath (for TUI delete)
// This replaces DeleteStateByDir since we now use a global directory
func DeleteStateByURL(id string, url string, destPath string) error {
	return DeleteState(id, url, destPath)
}

// ================== Master List Functions ==================

// MasterList holds all tracked downloads
type MasterList struct {
	Downloads []DownloadEntry `json:"downloads"`
}

// DownloadEntry represents a download in the master list
type DownloadEntry struct {
	ID          string `json:"id"`       // Unique ID of the download
	URLHash     string `json:"url_hash"` // Hash of URL only (backward compatibility)
	URL         string `json:"url"`
	DestPath    string `json:"dest_path"`
	Filename    string `json:"filename"`
	Status      string `json:"status"`       // "paused", "completed", "error"
	TotalSize   int64  `json:"total_size"`   // File size in bytes
	CompletedAt int64  `json:"completed_at"` // Unix timestamp when completed
	TimeTaken   int64  `json:"time_taken"`   // Duration in milliseconds (for completed)
}

func getMasterListPath() string {
	return filepath.Join(getSurgeDir(), "downloads.json")
}

// LoadMasterList loads the master downloads list from global state directory
func LoadMasterList() (*MasterList, error) {
	path := getMasterListPath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &MasterList{Downloads: []DownloadEntry{}}, nil
		}
		return nil, fmt.Errorf("failed to read master list: %w", err)
	}

	var list MasterList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("failed to unmarshal master list: %w", err)
	}

	return &list, nil
}

// SaveMasterList saves the master downloads list to global state directory
func SaveMasterList(list *MasterList) error {
	surgeDir := getSurgeDir()
	path := getMasterListPath()

	if err := os.MkdirAll(surgeDir, 0755); err != nil {
		return fmt.Errorf("failed to create surge directory: %w", err)
	}

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal master list: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write master list: %w", err)
	}

	return nil
}

// AddToMasterList adds or updates a download entry in the master list
func AddToMasterList(entry DownloadEntry) error {
	masterListMu.Lock()
	defer masterListMu.Unlock()

	list, err := LoadMasterList()
	if err != nil {
		list = &MasterList{Downloads: []DownloadEntry{}}
	}

	// Update existing or append new
	found := false
	for i, e := range list.Downloads {
		// Match by ID if available
		if entry.ID != "" && e.ID == entry.ID {
			list.Downloads[i] = entry
			found = true
			break
		} else if entry.ID == "" && e.URLHash == entry.URLHash {
			// Legacy fallback (should ideally not happen for new entries)
			list.Downloads[i] = entry
			found = true
			break
		}
	}
	if !found {
		list.Downloads = append(list.Downloads, entry)
	}

	return SaveMasterList(list)
}

// RemoveFromMasterList removes a download entry from the master list
// Uses stateHash for unique identification (falls back to URLHash for legacy entries)
func RemoveFromMasterList(id string) error {
	masterListMu.Lock()
	defer masterListMu.Unlock()

	list, err := LoadMasterList()
	if err != nil {
		return nil // Nothing to remove
	}

	// Filter out the entry
	newDownloads := make([]DownloadEntry, 0, len(list.Downloads))
	for _, e := range list.Downloads {
		if e.ID == id {
			continue // Skip this entry (remove it)
		}
		newDownloads = append(newDownloads, e)
	}
	list.Downloads = newDownloads

	return SaveMasterList(list)
}

// LoadPausedDownloads returns all paused downloads from the master list
func LoadPausedDownloads() ([]DownloadEntry, error) {
	list, err := LoadMasterList()
	if err != nil {
		return nil, err
	}

	var paused []DownloadEntry
	for _, e := range list.Downloads {
		if e.Status == "paused" {
			paused = append(paused, e)
		}
	}

	return paused, nil
}

// LoadCompletedDownloads returns all completed downloads from the master list
func LoadCompletedDownloads() ([]DownloadEntry, error) {
	list, err := LoadMasterList()
	if err != nil {
		return nil, err
	}

	var completed []DownloadEntry
	for _, e := range list.Downloads {
		if e.Status == "completed" {
			completed = append(completed, e)
		}
	}

	return completed, nil
}
