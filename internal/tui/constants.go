package tui

import "time"

const (
	// Timeouts and Intervals
	TickInterval = 200 * time.Millisecond
	// Input Dimensions
	InputWidth = 50

	// Layout Offsets and Padding
	HeaderWidthOffset      = 2
	ProgressBarWidthOffset = 4
	DefaultPaddingX        = 1
	DefaultPaddingY        = 0
	PopupPaddingY          = 1
	PopupPaddingX          = 2

	// Units
	Megabyte = 1024.0 * 1024.0

	// Channel Buffers - reduced since only events use channel now
	ProgressChannelBuffer = 16
)
