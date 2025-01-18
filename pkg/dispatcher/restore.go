package dispatcher

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xmigrate/blxrep/service"
	"github.com/xmigrate/blxrep/utils"

	"golang.org/x/sys/unix"
)

const chunkSize = 1 * 1024 * 1024 // 1MB chunks

const (
	maxRetries = 5
	retryDelay = 2 * time.Second
)

func RestoreFiles(agentID string, sourcePath string, destPath string, action utils.Action) error {
	conn := agents[agentID].RestoreConn
	agent, exists := agents[agentID]
	if !exists {
		utils.LogError("Agent with ID " + agentID + " does not exist in the agents map")
		return fmt.Errorf("agent with ID " + agentID + " not found")
	}

	utils.LogDebug("Agent details for ID " + agentID + ":")
	utils.LogDebug("  Agent: " + fmt.Sprintf("%+v", agent))

	// Create a buffer to store the compressed data
	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gzipWriter)

	// Compress the file or directory
	err := compressPath(sourcePath, tarWriter)
	if err != nil {
		return fmt.Errorf("error compressing data: %v", err)
	}

	// Close the tar and gzip writers
	if err := tarWriter.Close(); err != nil {
		return fmt.Errorf("error closing tar writer: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return fmt.Errorf("error closing gzip writer: %v", err)
	}

	// Get the compressed data
	compressedData := buf.Bytes()
	totalSize := len(compressedData)
	totalChunks := (totalSize + chunkSize - 1) / chunkSize

	// Send start message
	startMsg := utils.Message{
		Action: utils.CONST_AGENT_ACTION_RESTORE,
		RestoreMessage: utils.RestoreData{
			Type:        "start",
			TotalChunks: totalChunks,
			TotalSize:   int64(totalSize),
			FilePath:    destPath,
		},
	}
	if err := conn.WriteJSON(startMsg); err != nil {
		return fmt.Errorf("error sending start message: %v", err)
	}
	lastReportedProgress := 0
	// Send the data in chunks
	for i := 0; i < totalSize; i += chunkSize {
		end := i + chunkSize
		if end > totalSize {
			end = totalSize
		}
		chunk := compressedData[i:end]

		// Encode the chunk in base64
		encodedChunk := base64.StdEncoding.EncodeToString(chunk)

		// Send the chunk
		chunkMsg := utils.Message{
			Action: utils.CONST_AGENT_ACTION_RESTORE,
			RestoreMessage: utils.RestoreData{
				FilePath:   destPath,
				Type:       "chunk",
				ChunkIndex: i / chunkSize,
				Data:       encodedChunk,
			},
		}

		// Retry loop for sending the chunk
		for retry := 0; retry < maxRetries; retry++ {
			conn := agents[agentID].RestoreConn
			err := conn.WriteJSON(chunkMsg)
			if err == nil {
				// Chunk sent successfully
				utils.LogDebug("Sent chunk " + fmt.Sprintf("%d", i/chunkSize+1) + " of " + fmt.Sprintf("%d", totalChunks))
				break
			}

			if retry == maxRetries-1 {
				// Last retry attempt failed
				return fmt.Errorf("error sending chunk %d after %d attempts: %v", i/chunkSize+1, maxRetries, err)
			}

			utils.LogError("Error sending chunk " + fmt.Sprintf("%d", i/chunkSize+1) + " (attempt " + fmt.Sprintf("%d", retry+1) + " of " + fmt.Sprintf("%d", maxRetries) + "): " + err.Error() + ". Retrying...")
			time.Sleep(retryDelay)
		}

		// Log progress
		progress := int(float64(i) / float64(totalSize) * 100)
		utils.LogDebug("Sent chunk " + fmt.Sprintf("%d", i/chunkSize+1) + " of " + fmt.Sprintf("%d", totalChunks) + " (" + fmt.Sprintf("%d", progress) + "%)")
		if progress >= lastReportedProgress+5 || progress == 100 {
			action.ActionProgress = progress
			service.InsertOrUpdateAction(action)
			lastReportedProgress = progress
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Send complete message
	completeMsg := utils.Message{
		Action: utils.CONST_AGENT_ACTION_RESTORE,
		RestoreMessage: utils.RestoreData{
			Type:     "complete",
			FilePath: destPath,
		},
	}
	action.ActionProgress = 100
	action.ActionStatus = "Completed"
	service.InsertOrUpdateAction(action)
	if err := conn.WriteJSON(completeMsg); err != nil {
		return fmt.Errorf("error sending complete message: %v", err)
	}

	utils.LogDebug("File transfer completed: " + destPath)
	return nil
}

func compressPath(sourcePath string, tarWriter *tar.Writer) error {
	return filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, path)
		if err != nil {
			return fmt.Errorf("error creating tar header: %v", err)
		}

		// Create relative path
		relPath, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return fmt.Errorf("error creating relative path: %v", err)
		}

		// Set the header name to the relative path, preserving directory structure
		header.Name = filepath.ToSlash(relPath)

		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("error writing tar header: %v", err)
		}

		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("error opening file: %v", err)
			}
			defer file.Close()

			if _, err := io.Copy(tarWriter, file); err != nil {
				return fmt.Errorf("error writing file to tar: %v", err)
			}
		}

		return nil
	})
}

func getBlockDeviceSize(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	size, err := unix.IoctlGetInt(int(file.Fd()), unix.BLKGETSIZE64)
	if err != nil {
		return 0, err
	}

	return int64(size), nil
}

func RestorePartition(agentID string, sourcePath string, destPath string, blockSize int, channelSize int, ctx context.Context, restorePartition *sync.Mutex, isPartitionRestore *bool, action utils.Action) error {
	websock := agents[agentID].RestoreConn
	src, err := os.Open(sourcePath)
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to open source disk: %v", err))
	}
	defer src.Close()
	bufReader := bufio.NewReaderSize(src, blockSize*8000)

	// Allocate a buffer for one block.
	buf := make([]byte, blockSize)

	var blocks []utils.AgentDataBlock
	var blockCount uint64
	var batchSize int
	var totalDataSent int64
	var lastReportedProgress int

	totalSize, err := getBlockDeviceSize(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to get block device size: %v", err)
	}
	if totalSize == 0 {
		return fmt.Errorf("block device is empty or size couldn't be determined")
	}
	utils.LogDebug(fmt.Sprintf("Restoration started for %s", sourcePath))
	for {
		select {
		case <-ctx.Done():
			// Handle context cancellation and exit the goroutine
			utils.LogDebug("Restoration was paused/cancelled and goroutine is exiting.")
			if len(blocks) > 0 {
				utils.StreamData(blocks, websock, false, destPath, utils.CONST_AGENT_ACTION_PARTITION_RESTORE, time.Now().Unix())
				totalDataSent += int64(len(blocks) * blockSize)
			}
			restorePartition.Lock()
			*isPartitionRestore = false
			restorePartition.Unlock()
			action.ActionProgress = int(float64(totalDataSent) / float64(totalSize) * 100)
			action.ActionStatus = "Paused"
			service.InsertOrUpdateAction(action)
			return nil
		default:
			// Read data in larger chunks to reduce syscall overhead
			n, err := bufReader.Read(buf)
			if n > 0 {
				for i := 0; i < n; i += blockSize {
					end := i + blockSize
					if end > n {
						end = n
					}
					blockData := utils.AgentDataBlock{
						BlockNumber: blockCount,
						BlockData:   append([]byte(nil), buf[i:end]...),
					}
					blocks = append(blocks, blockData)
					blockCount++
					batchSize += end - i

					if batchSize >= channelSize {
						utils.StreamData(blocks, websock, false, destPath, utils.CONST_AGENT_ACTION_PARTITION_RESTORE, time.Now().Unix())
						totalDataSent += int64(batchSize)
						// Update action progress
						progress := float64(totalDataSent) / float64(totalSize) * 100
						utils.LogDebug(fmt.Sprintf("Batch sent. Total data sent so far: %d bytes percentage: %.2f%%", totalDataSent, progress))

						if int(progress) >= lastReportedProgress+2 || int(progress) == 100 {
							action.ActionProgress = int(progress)
							service.InsertOrUpdateAction(action)
							lastReportedProgress = int(progress)
						}

						blocks = nil
						batchSize = 0
						time.Sleep(100 * time.Millisecond)
					}
				}

			}
			if err != nil {
				if err == io.EOF {
					if len(blocks) > 0 {
						utils.StreamData(blocks, websock, false, sourcePath, utils.CONST_AGENT_ACTION_PARTITION_RESTORE, time.Now().Unix())
						totalDataSent += int64(len(blocks) * blockSize)
					}
					utils.LogDebug(fmt.Sprintf("Restore completed. Total data sent: %d bytes", totalDataSent))
					restorePartition.Lock()
					*isPartitionRestore = false
					restorePartition.Unlock()
					action.ActionProgress = 100
					action.ActionStatus = "Completed"
					service.InsertOrUpdateAction(action)
					return nil
				}
				action.ActionStatus = "Failed"
				service.InsertOrUpdateAction(action)
				log.Fatalf("Failed to read block: %v", err)
			}
		}
	}
}
