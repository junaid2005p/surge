package tui

import (
	"fmt"
	"strings"
	"time"

	"surge/internal/utils"

	"github.com/charmbracelet/lipgloss"
)

// Define the Layout Ratios
const (
	ListWidthRatio = 0.6 // List takes 60% width
)

func (m RootModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	// === Handle Modal States First ===
	// These overlays sit on top of the dashboard or replace it

	if m.state == InputState {
		labelStyle := lipgloss.NewStyle().Width(10).Foreground(ColorLightGray)
		// Centered popup - compact layout
		hintStyle := lipgloss.NewStyle().MarginLeft(1).Foreground(ColorLightGray) // Secondary
		if m.focusedInput == 1 {
			hintStyle = lipgloss.NewStyle().MarginLeft(1).Foreground(ColorNeonPink) // Highlighted
		}
		pathLine := lipgloss.JoinHorizontal(lipgloss.Left,
			labelStyle.Render("Path:"),
			m.inputs[1].View(),
			hintStyle.Render("[Tab] Browse"),
		)

		popup := lipgloss.JoinVertical(lipgloss.Left,
			TitleStyle.Render("ADD DOWNLOAD"),
			"",
			lipgloss.JoinHorizontal(lipgloss.Left, labelStyle.Render("URL:"), m.inputs[0].View()),
			pathLine,
			lipgloss.JoinHorizontal(lipgloss.Left, labelStyle.Render("Filename:"), m.inputs[2].View()),
			"",
			lipgloss.NewStyle().Foreground(ColorLightGray).Render("[Enter] Start  [Esc] Cancel"),
		)

		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			PaneStyle.Width(60).Padding(1, 2).Render(popup),
		)
	}

	if m.state == FilePickerState {
		pickerContent := lipgloss.JoinVertical(lipgloss.Left,
			TitleStyle.Render("SELECT DIRECTORY"),
			"",
			lipgloss.NewStyle().Foreground(ColorLightGray).Render(m.filepicker.CurrentDirectory),
			"",
			m.filepicker.View(),
			"",
			lipgloss.NewStyle().Foreground(ColorLightGray).Render("[.] Select Here  [H] Downloads  [Enter] Open  [Esc] Cancel"),
		)

		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			PaneStyle.Width(60).Padding(1, 2).Render(pickerContent),
		)
	}

	if m.state == DuplicateWarningState {
		warningContent := lipgloss.JoinVertical(lipgloss.Center,
			lipgloss.NewStyle().Foreground(ColorNeonPink).Bold(true).Render("⚠ DUPLICATE DETECTED"),
			"",
			lipgloss.NewStyle().Foreground(ColorNeonPurple).Bold(true).Render(truncateString(m.duplicateInfo, 50)),
			"",
			lipgloss.NewStyle().Foreground(ColorLightGray).Render("[C] Continue  [F] Focus Existing  [X] Cancel"),
		)

		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(ColorNeonPink).
				Padding(1, 3).
				Render(warningContent),
		)
	}

	// === MAIN DASHBOARD LAYOUT ===

	availableHeight := m.height - 2 // Margin
	availableWidth := m.width - 4   // Margin

	// Top Row Height (Logo + Graph)
	topHeight := 9

	// Bottom Row Height (List + Details)
	bottomHeight := availableHeight - topHeight - 1
	if bottomHeight < 10 {
		bottomHeight = 10
	} // Min height

	// Column Widths
	leftWidth := int(float64(availableWidth) * ListWidthRatio)
	rightWidth := availableWidth - leftWidth - 2 // -2 for spacing

	// --- SECTION 1: HEADER & LOGO (Top Left) ---
	logoText := `
 ██████  ██    ██ ██████   ██████  ███████ 
██       ██    ██ ██   ██ ██       ██      
███████  ██    ██ ██████  ██   ███ █████   
     ██  ██    ██ ██   ██ ██    ██ ██      
███████   ██████  ██   ██  ██████  ███████`

	// Create the header stats
	active, queued, downloaded := m.CalculateStats()
	statsText := fmt.Sprintf("Active: %d  •  Queued: %d  •  Done: %d", active, queued, downloaded)

	headerContent := lipgloss.JoinVertical(lipgloss.Left,
		LogoStyle.Render(logoText),
		lipgloss.NewStyle().Foreground(ColorLightGray).Render(statsText),
	)

	// Use PaneStyle for consistent borders with the graph box
	headerBox := PaneStyle.
		Width(leftWidth).
		Height(topHeight).
		Render(headerContent)

	// --- SECTION 2: SPEED GRAPH (Top Right) ---
	// Calculate dimensions
	// We reserve space for the axis labels on the left (e.g., " 10.5 MB/s")
	axisWidth := 12
	graphContentWidth := rightWidth - 4 - axisWidth
	if graphContentWidth < 10 {
		graphContentWidth = 10
	}

	// Determine Max Speed for the Axis
	maxSpeed := 1.0 // Prevent divide by zero
	for _, v := range m.SpeedHistory {
		if v > maxSpeed {
			maxSpeed = v
		}
	}

	// Render the Sparkline
	graphVisual := renderSparkline(m.SpeedHistory, graphContentWidth, maxSpeed)

	// Create the Axis (Left side)
	// We use a vertical join to place Max at top and 0 at bottom
	// We use the same height as the graph visual
	axisStyle := lipgloss.NewStyle().Width(axisWidth).Foreground(ColorGray).Align(lipgloss.Right)

	axisTop := axisStyle.Render(fmt.Sprintf("%.1f MB/s ", maxSpeed))
	axisBottom := axisStyle.Render("0.0 MB/s ")

	// Create a filler for the space between Top and Bottom labels
	// We assume a fixed height for visual balance (or calculate based on topHeight)
	// topHeight is usually 9. Subtract borders (2) and padding (2) ~ 5 lines usable
	spacerHeight := topHeight - 6
	if spacerHeight < 0 {
		spacerHeight = 0
	}
	spacer := strings.Repeat("\n", spacerHeight)

	axisColumn := lipgloss.JoinVertical(lipgloss.Right,
		axisTop,
		spacer,
		axisBottom,
	)

	// Combine Axis and Graph
	// We wrap the graph in a style to align it with the axis text if needed
	fullGraphRow := lipgloss.JoinHorizontal(lipgloss.Top,
		axisColumn,
		lipgloss.NewStyle().MarginLeft(1).Render(graphVisual),
	)

	// Get current speed for the title/overlay
	currentSpeed := 0.0
	if len(m.SpeedHistory) > 0 {
		currentSpeed = m.SpeedHistory[len(m.SpeedHistory)-1]
	}
	currentSpeedStr := fmt.Sprintf("Current: %.2f MB/s", currentSpeed)

	// Final Assembly for the box
	speedContent := lipgloss.JoinVertical(lipgloss.Right,
		lipgloss.NewStyle().Foreground(ColorNeonPink).Bold(true).Render(currentSpeedStr),
		"", // Spacer
		fullGraphRow,
	)

	graphBox := renderBtopBox("Network Activity", speedContent, rightWidth, topHeight, ColorNeonCyan, false)

	// --- SECTION 3: DOWNLOAD LIST (Bottom Left) ---
	// Tab Bar
	tabBar := renderTabs(m.activeTab, active, queued, downloaded)

	// Render the bubbles list or centered empty message
	var listContent string
	if len(m.list.Items()) == 0 {
		// Center "No downloads" like the right pane
		listContentHeight := bottomHeight - 6 // Account for tab bar and borders
		listContent = lipgloss.Place(leftWidth-4, listContentHeight, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(ColorNeonCyan).Render("No downloads"))
	} else {
		listContent = m.list.View()
	}

	listInner := lipgloss.NewStyle().Padding(1, 2).Render(lipgloss.JoinVertical(lipgloss.Left,
		tabBar,
		listContent,
	))
	listBox := renderBtopBox("Downloads", listInner, leftWidth, bottomHeight, ColorNeonPink, true)

	// --- SECTION 4: DETAILS PANE (Bottom Right) ---
	var detailContent string
	if d := m.GetSelectedDownload(); d != nil {
		detailContent = renderFocusedDetails(d, rightWidth-4)
	} else {
		detailContent = lipgloss.Place(rightWidth-4, bottomHeight-4, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(ColorNeonCyan).Render("No Download Selected"))
	}

	detailBox := renderBtopBox("File Details", detailContent, rightWidth, bottomHeight, ColorGray, true)

	// --- ASSEMBLY ---

	// Top Row
	topRow := lipgloss.JoinHorizontal(lipgloss.Top, headerBox, graphBox)

	// Bottom Row
	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, listBox, detailBox)

	// Footer - show notification if active, otherwise show keybindings
	var footer string
	if m.notification != "" {
		footer = lipgloss.Place(m.width, 1, lipgloss.Center, lipgloss.Center,
			NotificationStyle.Render(m.notification))
	} else {
		footer = lipgloss.NewStyle().Foreground(ColorLightGray).Padding(0, 1).Render(" [Q/W/E] Tabs  [A] Add  [P] Pause  [X] Delete  [/] Filter  [Ctrl+Q] Quit")
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		topRow,
		bottomRow,
		footer,
	)
}

// Helper to render the detailed info pane
func renderFocusedDetails(d *DownloadModel, w int) string {
	pct := 0.0
	if d.Total > 0 {
		pct = float64(d.Downloaded) / float64(d.Total)
	}

	// Progress bar with margins
	progressWidth := w - 12
	if progressWidth < 20 {
		progressWidth = 20
	}
	d.progress.Width = progressWidth
	progView := d.progress.ViewAs(pct)
	// pctStr was previously used for explicit percentage display

	// Consistent content width for centering
	contentWidth := w - 6

	// Section divider
	divider := lipgloss.NewStyle().
		Foreground(ColorGray).
		Render(strings.Repeat("─", contentWidth))

	// File info section
	fileInfo := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("Filename:"), StatsValueStyle.Render(truncateString(d.Filename, contentWidth-14))),
		"",
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("Status:"), StatsValueStyle.Render(getDownloadStatus(d))),
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("Size:"), StatsValueStyle.Render(fmt.Sprintf("%s / %s", utils.ConvertBytesToHumanReadable(d.Downloaded), utils.ConvertBytesToHumanReadable(d.Total)))),
	)

	// Progress section with percentage aligned right
	progressLabel := lipgloss.NewStyle().
		Foreground(ColorNeonCyan).
		Bold(true).
		Render("PROGRESS")
	// progressPct was previously used for explicit percentage display
	progressHeader := lipgloss.JoinHorizontal(lipgloss.Top,
		progressLabel,
		lipgloss.NewStyle().Width(contentWidth-lipgloss.Width(progressLabel)).Render(""),
	)
	progressSection := lipgloss.JoinVertical(lipgloss.Left,
		progressHeader,
		"",
		lipgloss.NewStyle().MarginLeft(1).Render(progView),
	)

	// Stats section
	statsSection := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("Speed:"), StatsValueStyle.Render(fmt.Sprintf("%.2f MB/s", d.Speed/Megabyte))),
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("Conns:"), StatsValueStyle.Render(fmt.Sprintf("%d", d.Connections))),
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("Elapsed:"), StatsValueStyle.Render(d.Elapsed.Round(time.Second).String())),
	)

	// URL section
	urlSection := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("URL:"), lipgloss.NewStyle().Foreground(ColorLightGray).Render(truncateString(d.URL, contentWidth-14))),
	)

	// Combine all sections with dividers and spacing
	content := lipgloss.JoinVertical(lipgloss.Left,
		fileInfo,
		"",
		divider,
		"",
		progressSection,
		"",
		divider,
		"",
		statsSection,
		"",
		divider,
		"",
		urlSection,
	)

	// Wrap in a container with margins
	return lipgloss.NewStyle().
		Padding(1, 2).
		Render(content)
}

func getDownloadStatus(d *DownloadModel) string {
	style := lipgloss.NewStyle()

	switch {
	case d.err != nil:
		return style.Foreground(ColorStateError).Render("✖ Error")
	case d.done:
		return style.Foreground(ColorStateDone).Render("✔ Completed")
	case d.paused:
		return style.Foreground(ColorStatePaused).Render("⏸ Paused")
	case d.Speed == 0 && d.Downloaded == 0:
		return style.Foreground(ColorStatePaused).Render("o Queued")
	default:
		return style.Foreground(ColorStateDownloading).Render("⬇ Downloading")
	}
}

// Simple Sparkline Generator using Braille patterns
func renderSparkline(data []float64, w int, maxVal float64) string {
	// Braille characters
	// distinct levels: ' ', '⡀', '⣀', '⣄', '⣤', '⣦', '⣶', '⣷', '⣿'
	levels := []rune{' ', '⡀', '⣀', '⣄', '⣤', '⣦', '⣶', '⣷', '⣿'}

	var s strings.Builder

	// 1. Calculate how many points we can actually show
	// If we have less data than width, we show all data.
	// If we have more data than width, we slice the end.
	var visibleData []float64
	if len(data) > w {
		visibleData = data[len(data)-w:]
	} else {
		visibleData = data
	}

	// 2. Padding (The Fix for "Starting in Middle")
	// If we have fewer data points than width, fill the START with spaces
	// so the graph grows from the right side.
	padding := w - len(visibleData)
	if padding > 0 {
		s.WriteString(strings.Repeat(" ", padding))
	}

	// 3. Render the bars
	for _, val := range visibleData {
		// Normalize value between 0 and 1 based on maxVal
		normalized := val / maxVal

		// Map to level index
		levelIdx := int(normalized * float64(len(levels)-1))

		// Clamp index
		if levelIdx < 0 {
			levelIdx = 0
		}
		if levelIdx >= len(levels) {
			levelIdx = len(levels) - 1
		}

		// Edge case: If val is > 0 but calculated index is 0 (very small number),
		// bump it to 1 so we at least see a dot.
		if val > 0 && levelIdx == 0 {
			levelIdx = 1
		}

		s.WriteRune(levels[levelIdx])
	}

	return lipgloss.NewStyle().Foreground(ColorNeonPink).Render(s.String())
}

func (m RootModel) calcTotalSpeed() float64 {
	total := 0.0
	for _, d := range m.downloads {
		total += d.Speed
	}
	return total / Megabyte
}

func (m RootModel) CalculateStats() (active, queued, downloaded int) {
	for _, d := range m.downloads {
		if d.done {
			downloaded++
		} else if d.Speed > 0 {
			active++
		} else {
			queued++
		}
	}
	return
}

func truncateString(s string, i int) string {
	runes := []rune(s)
	if len(runes) > i {
		return string(runes[:i]) + "..."
	}
	return s
}

func renderTabs(activeTab, activeCount, queuedCount, doneCount int) string {
	tabs := []struct {
		Label string
		Count int
	}{
		{"Queued", queuedCount},
		{"Active", activeCount},
		{"Done", doneCount},
	}
	var rendered []string
	for i, t := range tabs {
		var style lipgloss.Style
		if i == activeTab {
			style = ActiveTabStyle
		} else {
			style = TabStyle
		}
		label := fmt.Sprintf("%s (%d)", t.Label, t.Count)
		rendered = append(rendered, style.Render(label))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

// renderBtopBox creates a btop-style box with title embedded in the top border
// titleRight: if true, title appears on the right side; if false, title appears on the left
// Example (left):  ╭─ TITLE ─────────────────────────────────╮
// Example (right): ╭─────────────────────────────────── TITLE ─╮
func renderBtopBox(title string, content string, width, height int, borderColor lipgloss.Color, titleRight bool) string {
	// Border characters
	const (
		topLeft     = "╭"
		topRight    = "╮"
		bottomLeft  = "╰"
		bottomRight = "╯"
		horizontal  = "─"
		vertical    = "│"
	)

	innerWidth := width - 2 // Account for left and right borders
	if innerWidth < 1 {
		innerWidth = 1
	}

	// Build top border with embedded title
	titleText := fmt.Sprintf(" %s ", title)
	titleLen := len(titleText)
	remainingWidth := innerWidth - titleLen - 1 // -1 for the dash after topLeft
	if remainingWidth < 0 {
		remainingWidth = 0
	}

	var topBorder string
	if titleRight {
		// Title on the right: ╭─────────────────────────────────── TITLE ─╮
		topBorder = lipgloss.NewStyle().Foreground(borderColor).Render(topLeft+strings.Repeat(horizontal, remainingWidth)) +
			lipgloss.NewStyle().Foreground(ColorNeonCyan).Bold(true).Render(titleText) +
			lipgloss.NewStyle().Foreground(borderColor).Render(horizontal+topRight)
	} else {
		// Title on the left: ╭─ TITLE ─────────────────────────────────╮
		topBorder = lipgloss.NewStyle().Foreground(borderColor).Render(topLeft+horizontal) +
			lipgloss.NewStyle().Foreground(ColorNeonCyan).Bold(true).Render(titleText) +
			lipgloss.NewStyle().Foreground(borderColor).Render(strings.Repeat(horizontal, remainingWidth)) +
			lipgloss.NewStyle().Foreground(borderColor).Render(topRight)
	}

	// Build bottom border: ╰───────────────────╯
	bottomBorder := lipgloss.NewStyle().Foreground(borderColor).Render(
		bottomLeft + strings.Repeat(horizontal, innerWidth) + bottomRight,
	)

	// Style for vertical borders
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	// Wrap content lines with vertical borders
	contentLines := strings.Split(content, "\n")
	innerHeight := height - 2 // Account for top and bottom borders

	var wrappedLines []string
	for i := 0; i < innerHeight; i++ {
		var line string
		if i < len(contentLines) {
			line = contentLines[i]
		} else {
			line = ""
		}
		// Pad or truncate line to fit innerWidth
		lineWidth := lipgloss.Width(line)
		if lineWidth < innerWidth {
			line = line + strings.Repeat(" ", innerWidth-lineWidth)
		} else if lineWidth > innerWidth {
			// Truncate (simplified - just take first innerWidth chars)
			runes := []rune(line)
			if len(runes) > innerWidth {
				line = string(runes[:innerWidth])
			}
		}
		wrappedLines = append(wrappedLines, borderStyle.Render(vertical)+line+borderStyle.Render(vertical))
	}

	// Combine all parts
	return lipgloss.JoinVertical(lipgloss.Left,
		topBorder,
		strings.Join(wrappedLines, "\n"),
		bottomBorder,
	)
}
