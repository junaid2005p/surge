package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"surge/internal/downloader"
	"surge/internal/messages"
	"surge/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// Shared channel for download events (start/complete/error)
var eventCh chan tea.Msg
var program *tea.Program

// initTUI sets up the shared event channel and BubbleTea program
func initTUI() {
	eventCh = make(chan tea.Msg, DefaultProgressChannelBuffer)
	program = tea.NewProgram(tui.InitialRootModel(), tea.WithAltScreen())

	// Pump events to TUI
	go func() {
		for msg := range eventCh {
			program.Send(msg)
		}
	}()
}

// runTUI starts the TUI and blocks until quit
func runTUI() error {
	_, err := program.Run()
	return err
}

// sendToServer sends a download request to a running surge server
func sendToServer(url, outPath string, port int) error {
	reqBody := DownloadRequest{
		URL:  url,
		Path: outPath,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	serverURL := fmt.Sprintf("http://127.0.0.1:%d/download", port)
	resp, err := http.Post(serverURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send request to server: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned error: %s - %s", resp.Status, string(body))
	}

	fmt.Printf("Download queued on server: %s\n", string(body))
	return nil
}

var getCmd = &cobra.Command{
	Use:   "get [url]",
	Short: "get downloads a file from a URL",
	Long: `get downloads a file from a URL and saves it to the local filesystem.
If --port is specified, the download request is sent to a running surge server instead.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		url := args[0]
		outPath, _ := cmd.Flags().GetString("path")
		verbose, _ := cmd.Flags().GetBool("verbose")
		// concurrent, _ := cmd.Flags().GetInt("concurrent") Have to implement this later
		md5sum, _ := cmd.Flags().GetString("md5")
		sha256sum, _ := cmd.Flags().GetString("sha256")
		port, _ := cmd.Flags().GetInt("port")

		if outPath == "" {
			outPath = "."
		}

		// If port is specified, send to server instead of downloading locally
		if port > 0 {
			if err := sendToServer(url, outPath, port); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// Local download
		initTUI()
		ctx := context.Background()
		go func() {
			defer close(eventCh)
			if err := downloader.Download(ctx, url, outPath, verbose, md5sum, sha256sum, eventCh, 1); err != nil {
				program.Send(messages.DownloadErrorMsg{DownloadID: 1, Err: err})
			}
		}()

		if err := runTUI(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	getCmd.Flags().StringP("path", "o", "", "the path to the download folder")
	getCmd.Flags().IntP("concurrent", "c", DefaultConcurrentConnections, "number of concurrent connections (1 = single thread)")
	getCmd.Flags().BoolP("verbose", "v", false, "enable verbose output")
	getCmd.Flags().String("md5", "", "MD5 checksum for verification")
	getCmd.Flags().String("sha256", "", "SHA256 checksum for verification")
	getCmd.Flags().IntP("port", "p", 0, "port of running surge server to send download to (0 = run locally)")
}
