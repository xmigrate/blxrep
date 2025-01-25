package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xmigrate/blxrep/utils"
)

func processChunk(state *utils.RestoreState, data []byte, chunkIndex int) error {
	log.Printf("Received chunk %d, size: %d bytes", chunkIndex, len(data))

	if chunkIndex != state.ChunksReceived {
		return fmt.Errorf("received out-of-order chunk: expected %d, got %d", state.ChunksReceived, chunkIndex)
	}

	_, err := state.Buffer.Write(data)
	if err != nil {
		return fmt.Errorf("error writing to buffer: %v", err)
	}

	state.ChunksReceived++

	// If we've received all chunks, process the data
	if state.ChunksReceived == state.TotalChunks {
		return processCompleteData(state)
	}

	return nil
}

func processCompleteData(state *utils.RestoreState) error {
	utils.LogDebug(fmt.Sprintf("Processing complete data, total size: %d bytes", state.Buffer.Len()))

	gzipReader, err := gzip.NewReader(bytes.NewReader(state.Buffer.Bytes()))
	if err != nil {
		return fmt.Errorf("error creating gzip reader: %v", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return fmt.Errorf("error reading tar: %v", err)
		}

		err = extractEntry(state.FilePath, header, tarReader)
		if err != nil {
			return fmt.Errorf("error extracting entry: %v", err)
		}
	}

	log.Println("Restore process completed successfully")
	return nil
}

func extractEntry(basePath string, header *tar.Header, tarReader *tar.Reader) error {
	// Handle both cases: with and without "RESTORE" prefix
	relPath := header.Name
	if strings.HasPrefix(relPath, "RESTORE/") {
		relPath = strings.TrimPrefix(relPath, "RESTORE/")
	}

	relPath = filepath.FromSlash(relPath)
	target := filepath.Join(basePath, relPath)

	log.Printf("Extracting: %s, type: %c, size: %d bytes", target, header.Typeflag, header.Size)

	switch header.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, 0755)
	case tar.TypeReg:
		return extractRegularFile(target, header, tarReader)
	default:
		log.Printf("Unsupported file type: %c for %s", header.Typeflag, target)
		return nil // Skipping unsupported types
	}
}

func extractRegularFile(target string, header *tar.Header, tarReader *tar.Reader) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("error creating directory %s: %v", dir, err)
	}

	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
	if err != nil {
		return fmt.Errorf("error creating file %s: %v", target, err)
	}
	defer file.Close()

	_, err = io.Copy(file, tarReader)
	if err != nil {
		return fmt.Errorf("error writing file %s: %v", target, err)
	}

	log.Printf("File extracted: %s", target)
	return nil
}

func cleanupRestore(state *utils.RestoreState) {
	log.Println("Cleaning up restore process")
	if state.GzipReader != nil {
		state.GzipReader.Close()
	}
	// Consider removing partially extracted files here
}

const BlockSize = 512

func RestorePartition(c *chan utils.AgentBulkMessage, agentID string, progress *int, destPath string) {
	log.Printf("Starting RestorePartition process for agent %s, destPath: %s", agentID, destPath)

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Printf("Cannot open partition %s for agent %s: %v", destPath, agentID, err)
		return
	}
	defer f.Close()

	timeoutDuration := 20 * time.Second

	totalBytesWritten := int64(0)
	totalMessagesReceived := 0
	lastActivityTime := time.Now()
	for {
		select {
		case msg, ok := <-*c:
			if !ok {
				log.Printf("Channel closed, exiting RestorePartition for agent %s", agentID)
				return
			}
			lastActivityTime = time.Now()
			totalMessagesReceived++
			batchBytesWritten := int64(0)

			for _, blockData := range msg.Data {
				n, err := f.Write(blockData.BlockData)
				if err != nil {
					log.Printf("Failed to write to file for agent %s: %v", agentID, err)
					return
				}
				batchBytesWritten += int64(n)
				totalBytesWritten += int64(n)

			}
			log.Printf("Batch received for agent %s: Wrote %d bytes", agentID, batchBytesWritten)
			log.Printf("Total for agent %s: Messages received: %d, Bytes written: %d", agentID, totalMessagesReceived, totalBytesWritten)
			// TODO: Below code might not be needed, hence commenting it out. No point in updating progress for restore from agent in dispatcher boltdb.
			// actionId := strings.Join([]string{agentID, fmt.Sprintf("%d", msg.StartTime)}, "_")
			// dispatcher.UpdateProgress(msg, progress, actionId, agentID, destPath)

		case <-time.After(timeoutDuration):
			log.Printf("No data for %v. Exiting RestorePartition for agent %s", timeoutDuration, agentID)
			log.Printf("Final stats for agent %s: Total messages received: %d, Total bytes written: %d", agentID, totalMessagesReceived, totalBytesWritten)
			log.Printf("Time since last activity: %v", time.Since(lastActivityTime))
			return
		}
	}
}
