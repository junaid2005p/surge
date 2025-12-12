package tui

import (
	"surge/internal/downloader"
	"surge/internal/messages"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	DefaultPollInterval   = 150 * time.Millisecond
	DeltaPercentThreshold = 0.003 // 0.3% change threshold
	SpeedSmoothingAlpha   = 0.3   // EMA smoothing factor
)

type ProgressReporter struct {
	state        *downloader.ProgressState
	pollInterval time.Duration
	lastSpeed    float64
	lastPercent  float64
	lastReportAt time.Time
}

func NewProgressReporter(state *downloader.ProgressState) *ProgressReporter {
	return &ProgressReporter{
		state:        state,
		pollInterval: DefaultPollInterval,
		lastSpeed:    0,
		lastPercent:  0,
	}
}

// PollCmd returns a tea.Cmd that polls the progress state after the interval
func (r *ProgressReporter) PollCmd() tea.Cmd {
	return tea.Tick(r.pollInterval, func(t time.Time) tea.Msg {
		// Check if download is done
		if r.state.Done.Load() {
			elapsed := time.Since(r.state.StartTime)
			return messages.DownloadCompleteMsg{
				DownloadID: r.state.ID,
				Elapsed:    elapsed,
				Total:      r.state.TotalSize,
			}
		}

		// Check for errors
		if err := r.state.GetError(); err != nil {
			return messages.DownloadErrorMsg{
				DownloadID: r.state.ID,
				Err:        err,
			}
		}

		// Get current progress
		downloaded, total, elapsed, connections := r.state.GetProgress()

		// Calculate current percent
		var currentPercent float64
		if total > 0 {
			currentPercent = float64(downloaded) / float64(total)
		}

		// Delta filtering: skip if change is too small (unless first update)
		delta := currentPercent - r.lastPercent
		if r.lastPercent > 0 && delta < DeltaPercentThreshold && delta >= 0 {
			// Still need to continue polling, return a tick message
			return progressTickMsg{downloadID: r.state.ID}
		}
		r.lastPercent = currentPercent

		// Calculate speed with EMA smoothing
		var instantSpeed float64
		if elapsed.Seconds() > 0 {
			instantSpeed = float64(downloaded) / elapsed.Seconds()
		}

		if r.lastSpeed == 0 {
			r.lastSpeed = instantSpeed
		} else {
			r.lastSpeed = SpeedSmoothingAlpha*instantSpeed + (1-SpeedSmoothingAlpha)*r.lastSpeed
		}

		return messages.ProgressMsg{
			DownloadID:        r.state.ID,
			Downloaded:        downloaded,
			Total:             total,
			Speed:             r.lastSpeed,
			ActiveConnections: int(connections),
		}
	})
}

// progressTickMsg is an internal message to continue polling without UI update
type progressTickMsg struct {
	downloadID int
}
