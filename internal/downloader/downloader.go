package downloader

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"surge/internal/util"

	"github.com/h2non/filetype"
	"github.com/vfaronov/httpheader"
)

type Downloader struct {
	Client                   *http.Client //Every downloader has a http client over which the downloads happen
	bytesDownloadedPerSecond []int64
	mu                       sync.Mutex
}

func NewDownloader() *Downloader {
	client := http.Client{
		Timeout: 0,
	}
	return &Downloader{Client: &client}
}

func (d *Downloader) Download(ctx context.Context, rawurl, outPath string, concurrent int, verbose bool) error {
	if concurrent > 1 {
		return d.concurrentDownload(ctx, rawurl, outPath, concurrent, verbose)
	}
	return d.singleDownload(ctx, rawurl, outPath, verbose)
}

func (d *Downloader) singleDownload(ctx context.Context, rawurl, outPath string, verbose bool) error {
	parsed, err := url.Parse(rawurl) //Parses the URL into parts
	if err != nil {
		return err
	}

	if parsed.Scheme == "" {
		if verbose {
			fmt.Fprintln(os.Stderr, "Error: URL missing scheme (use http:// or https://)")
		}
		return errors.New("url missing scheme (use http:// or https://)")
	} //if the URL does not have a scheme, return an error

	if verbose {
		fmt.Fprintf(os.Stderr, "Initiating single download for URL: %s\n", rawurl)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil) //We use a context so that we can cancel the download whenever we want
	if err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "Error creating HTTP request: %v\n", err)
		}
		return err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "+
		"AppleWebKit/537.36 (KHTML, like Gecko) "+
		"Chrome/120.0.0.0 Safari/537.36") // We set a browser like header to avoid being blocked by some websites

	resp, err := d.Client.Do(req) //Exectes the HTTP request
	if err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "Error executing HTTP request: %v\n", err)
		}
		return err
	}
	defer resp.Body.Close() //Closes the response body when the function returns

	if verbose {
		fmt.Fprintf(os.Stderr, "Received HTTP response with status code: %d\n", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		if verbose {
			fmt.Fprintf(os.Stderr, "Error: Unexpected status code: %d\n", resp.StatusCode)
		}
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Determine the filename
	filename := filepath.Base(parsed.Path) // Start with filename from URL path

	// Try to extract filename from Content-Disposition header
	if _, name, err := httpheader.ContentDisposition(resp.Header); err == nil && name != "" {
		filename = filepath.Base(name)
		if verbose {
			fmt.Fprintf(os.Stderr, "Filename from Content-Disposition: %s\n", filename)
		}
	}

	// Read first up to 512 bytes for sniffing. handle short reads
	header := make([]byte, 512)
	n, rerr := io.ReadFull(resp.Body, header)
	if rerr != nil {
		if rerr == io.ErrUnexpectedEOF || rerr == io.EOF {
			// fewer than 512 bytes available but n contains what we did read
			header = header[:n]
		} else {
			// real error (connection closed, etc.)
			return fmt.Errorf("reading header: %w", rerr)
		}
	} else {
		header = header[:n]
	}

	body := io.MultiReader(bytes.NewReader(header), resp.Body)

	if verbose {
		mimeType := http.DetectContentType(header)
		fmt.Fprintln(os.Stderr, "Detected MIME:", mimeType)

		if kind, _ := filetype.Match(header); kind != filetype.Unknown {
			fmt.Fprintln(os.Stderr, "Magic Type:", kind.Extension, kind.MIME)
		}
	}

	// ZIP filename extraction (if applicable, override current filename)
	if len(header) >= 4 && bytes.HasPrefix(header, []byte{0x50, 0x4B, 0x03, 0x04}) && len(header) >= 30 {
		nameLen := int(binary.LittleEndian.Uint16(header[26:28]))
		start := 30
		end := start + nameLen
		if end <= len(header) {
			zipName := string(header[start:end])
			if zipName != "" {
				filename = filepath.Base(zipName) // Override filename with ZIP internal name
				if verbose {
					fmt.Fprintln(os.Stderr, "ZIP internal filename:", zipName)
				}
			}
		}
	}

	// MIME type extension (if filename has no extension and MIME type is detectable)
	if filepath.Ext(filename) == "" { // Only add extension if one isn't already present
		if kind, _ := filetype.Match(header); kind != filetype.Unknown {
			if kind.Extension != "" {
				filename = filename + "." + kind.Extension
				if verbose {
					fmt.Fprintf(os.Stderr, "Added extension from magic type: %s\n", kind.Extension)
				}
			}
		}
	}

	// Final fallback: If filename is still empty or invalid
	if filename == "" || filename == "." || filename == "/" {
		filename = "download.bin"
		if verbose {
			fmt.Fprintln(os.Stderr, "Falling back to default filename: download.bin")
		}
	}

	outDir := filepath.Dir(outPath)
	if verbose {
		fmt.Fprintf(os.Stderr, "Creating temporary file in directory: %s with pattern: %s.surge\n", outDir, filename)
	}
	tmpFile, err := os.CreateTemp(outDir, filename+".surge") //Tries to create a temporary file
	if err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "Error creating temporary file: %v\n", err)
		}
		return err
	} // Returns error if it fails to create temp file
	tmpPath := tmpFile.Name()
	if verbose {
		fmt.Fprintf(os.Stderr, "Temporary file created: %s\n", tmpPath)
	}

	defer func() {
		tmpFile.Close()
		// if download failed, remove temp file
		if err != nil {
			os.Remove(tmpPath)
		}
	}() //Waits until the function returns and closes the temp file and removes it if there was an error

	start := time.Now()
	if verbose {
		fmt.Fprintln(os.Stderr, "Starting file copy from response body to temporary file...")
	}
	written, err := io.Copy(tmpFile, body) // IMPORTANT: copy from 'body', not resp.Body
	if err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "Error during file copy: %v\n", err)
		}
		return fmt.Errorf("copy failed: %w", err)
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "Finished copying %d bytes to temporary file.\n", written)
	}

	elapsed := time.Since(start)
	speed := float64(written) / 1024.0 / elapsed.Seconds() // KiB/s
	fmt.Fprintf(os.Stderr, "\nDownloaded %s in %s (%s/s)\n",
		outPath,
		elapsed.Round(time.Second),
		util.ConvertBytesToHumanReadable(int64(speed*1024)),
	)

	// // sync file to disk
	// if err := tmpFile.Sync(); err != nil {
	// 	return err
	// }
	// if err := tmpFile.Close(); err != nil {
	// 	return err
	// }

	// atomically move temp to dest
	destPath := outPath
	if info, err := os.Stat(outPath); err == nil && info.IsDir() {
		// When outPath is a directory we must have a valid filename.
		// The filename variable was determined earlier.
		destPath = filepath.Join(outPath, filename)
		if verbose {
			fmt.Fprintf(os.Stderr, "Destination path updated to: %s (outPath was a directory)\n", destPath)
		}
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "Attempting to rename temporary file %s to destination %s\n", tmpPath, destPath)
	}
	if renameErr := os.Rename(tmpPath, destPath); renameErr != nil { //If renaming fails, we do a manual copy
		if verbose {
			fmt.Fprintf(os.Stderr, "Rename failed: %v. Attempting manual copy.\n", renameErr)
		}
		if in, rerr := os.Open(tmpPath); rerr == nil { // Opens temp file for reading
			defer in.Close()                                   //Waits until function returns to close temp file
			if out, werr := os.Create(destPath); werr == nil { //Creates destination file
				defer out.Close() //Waits until function returns to close destination file
				if verbose {
					fmt.Fprintf(os.Stderr, "Manually copying from %s to %s\n", tmpPath, destPath)
				}
				if _, cerr := io.Copy(out, in); cerr != nil { //Tries to copy from temp to destination
					if verbose {
						fmt.Fprintf(os.Stderr, "Error during manual copy: %v\n", cerr)
					}
					return cerr // return the real copy error
				}
				if verbose {
					fmt.Fprintln(os.Stderr, "Manual copy successful.")
				}
			} else {
				if verbose {
					fmt.Fprintf(os.Stderr, "Error creating destination file for manual copy: %v\n", werr)
				}
				return werr // handle file creation error
			}
		} else {
			if verbose {
				fmt.Fprintf(os.Stderr, "Error opening temporary file for manual copy: %v\n", rerr)
			}
			return rerr // handle file open error
		}
		os.Remove(tmpPath) //only remove after successful copy
		if verbose {
			fmt.Fprintf(os.Stderr, "Removed temporary file: %s\n", tmpPath)
		}
		return fmt.Errorf("rename failed: %v", renameErr) //If everything fails we say renaming the file failed
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "Successfully renamed %s to %s\n", tmpPath, destPath)
	}

	return nil
}

func (d *Downloader) concurrentDownload(ctx context.Context, rawurl, outPath string, concurrent int, verbose bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return err
	}

	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.Header.Get("Accept-Ranges") != "bytes" {
		fmt.Println("Server does not support concurrent download, falling back to single thread")
		return d.singleDownload(ctx, rawurl, outPath, verbose)
	}

	totalSize, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	if err != nil {
		return err
	}

	chunkSize := totalSize / int64(concurrent)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var written int64

	startTime := time.Now()

	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		start := int64(i) * chunkSize
		end := start + chunkSize - 1
		if i == concurrent-1 {
			end = totalSize - 1
		}

		go func(i int, start, end int64) {
			defer wg.Done()
			err := d.downloadChunk(ctx, rawurl, outPath, i, start, end, &mu, &written, totalSize, startTime, verbose)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\nError downloading chunk %d: %v\n", i, err)
			}
		}(i, start, end)
	}

	wg.Wait()

	fmt.Print("Downloaded all parts! Merging...\n")

	// Merge files
	destFile, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer destFile.Close()

	for i := 0; i < concurrent; i++ {
		partFileName := fmt.Sprintf("%s.part%d", outPath, i)
		partFile, err := os.Open(partFileName)
		if err != nil {
			return err
		}
		_, err = io.Copy(destFile, partFile)
		if err != nil {
			partFile.Close()
			return err
		}
		partFile.Close()
		os.Remove(partFileName)
	}

	elapsed := time.Since(startTime)
	speed := float64(totalSize) / 1024.0 / elapsed.Seconds() // KiB/s
	fmt.Fprintf(os.Stderr, "\nDownloaded %s in %s (%s/s)\n", outPath, elapsed.Round(time.Second), util.ConvertBytesToHumanReadable(int64(speed*1024)))
	return nil
}

func (d *Downloader) downloadChunk(ctx context.Context, rawurl, outPath string, index int, start, end int64, mu *sync.Mutex, written *int64, totalSize int64, startTime time.Time, verbose bool) error {

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	partFileName := fmt.Sprintf("%s.part%d", outPath, index)
	partFile, err := os.Create(partFileName)
	if err != nil {
		return err
	}
	defer partFile.Close()

	buf := make([]byte, 32*1024)
	for {

		n, err := resp.Body.Read(buf)

		if n > 0 {
			_, wErr := partFile.Write(buf[:n])
			if wErr != nil {
				return wErr
			}
			mu.Lock()
			*written += int64(n)
			d.printProgress(*written, totalSize, startTime, verbose)
			mu.Unlock()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *Downloader) printProgress(written, total int64, start time.Time, verbose bool) {
	elapsed := time.Since(start).Seconds()
	speed := float64(written) / 1024.0 / elapsed // KiB/s

	d.mu.Lock()
	d.bytesDownloadedPerSecond = append(d.bytesDownloadedPerSecond, int64(speed))
	if len(d.bytesDownloadedPerSecond) > 30 {
		d.bytesDownloadedPerSecond = d.bytesDownloadedPerSecond[1:]
	}

	var avgSpeed float64
	var totalSpeed int64
	for _, s := range d.bytesDownloadedPerSecond {
		totalSpeed += s
	}
	if len(d.bytesDownloadedPerSecond) > 0 {
		avgSpeed = float64(totalSpeed) / float64(len(d.bytesDownloadedPerSecond))
	}
	d.mu.Unlock()

	eta := "N/A"
	if total > 0 && avgSpeed > 0 {
		remainingBytes := total - written
		remainingSeconds := float64(remainingBytes) / (avgSpeed * 1024)
		eta = time.Duration(remainingSeconds * float64(time.Second)).Round(time.Second).String()
	}

	if total > 0 {
		pct := float64(written) / float64(total) * 100.0
		fmt.Fprintf(os.Stderr, "\r%.2f%% %s/%s (%.1f KiB/s) ETA: %s", pct, util.ConvertBytesToHumanReadable(written), util.ConvertBytesToHumanReadable(total), speed, eta)
	} else {
		fmt.Fprintf(os.Stderr, "\r%s (%.1f KiB/s)", util.ConvertBytesToHumanReadable(written), speed)
	}
}
