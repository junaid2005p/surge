package messages

import (
	"time"
)

// ProgressMsg represents a progress update from the downloader
type ProgressMsg struct {
	DownloadID        int
	Downloaded        int64
	Total             int64
	Speed             float64 // bytes per second
	ActiveConnections int
}

// DownloadCompleteMsg signals that the download finished successfully
type DownloadCompleteMsg struct {
	DownloadID int
	Filename   string
	Elapsed    time.Duration
	Total      int64
}

// DownloadErrorMsg signals that an error occurred
type DownloadErrorMsg struct {
	DownloadID int
	Err        error
}

// DownloadStartedMsg is sent when a download actually starts (after metadata fetch)
type DownloadStartedMsg struct {
	DownloadID int
	URL        string
	Filename   string
	Total      int64
	DestPath   string // Full path to the destination file
}

type DownloadPausedMsg struct {
	DownloadID int
	Downloaded int64
}

type DownloadResumedMsg struct {
	DownloadId int
}
