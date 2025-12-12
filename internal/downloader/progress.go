package downloader

import (
	"sync/atomic"
	"time"
)

type ProgressState struct {
	ID            int
	Downloaded    atomic.Int64
	TotalSize     int64
	StartTime     time.Time
	ActiveWorkers atomic.Int32
	Done          atomic.Bool
	Error         atomic.Pointer[error]
}

func NewProgressState(id int, totalSize int64) *ProgressState {
	return &ProgressState{
		ID:        id,
		TotalSize: totalSize,
		StartTime: time.Now(),
	}
}

func (ps *ProgressState) SetTotalSize(size int64) {
	ps.TotalSize = size
	ps.StartTime = time.Now() // Reset start time when we know the real size
}

func (ps *ProgressState) SetError(err error) {
	ps.Error.Store(&err)
}

func (ps *ProgressState) GetError() error {
	if e := ps.Error.Load(); e != nil {
		return *e
	}
	return nil
}

func (ps *ProgressState) GetProgress() (downloaded int64, total int64, elapsed time.Duration, connections int32) {
	downloaded = ps.Downloaded.Load()
	total = ps.TotalSize
	elapsed = time.Since(ps.StartTime)
	connections = ps.ActiveWorkers.Load()
	return
}
