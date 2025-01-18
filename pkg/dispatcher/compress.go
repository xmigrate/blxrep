package dispatcher

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xmigrate/blxrep/service"
	"github.com/xmigrate/blxrep/utils"

	"github.com/klauspost/compress/zstd"
)

// ProgressInfo holds information about compression/decompression progress
type ProgressInfo struct {
	Percentage     float64
	ProcessedBytes int64
	TotalBytes     int64
	Speed          float64 // MB/s
}

func CompressFileWithProgress(inputPath string, progressChan chan ProgressInfo) error {
	inputFile, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("failed to open input file: %v", err)
	}
	defer inputFile.Close()

	// Get file size for progress reporting
	fileInfo, err := inputFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to get file info: %v", err)
	}
	totalSize := fileInfo.Size()

	outputFile, err := os.Create(inputPath + ".zst")
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer outputFile.Close()

	encoder, err := zstd.NewWriter(outputFile,
		zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return fmt.Errorf("failed to create encoder: %v", err)
	}
	defer encoder.Close()

	// Create a buffered reader for progress reporting
	buf := make([]byte, 1024*1024) // 1MB buffer
	var processed int64
	startTime := time.Now()

	for {
		n, err := inputFile.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read error: %v", err)
		}

		_, err = encoder.Write(buf[:n])
		if err != nil {
			return fmt.Errorf("write error: %v", err)
		}

		processed += int64(n)
		elapsed := time.Since(startTime).Seconds()
		speed := float64(processed) / elapsed / (1024 * 1024) // MB/s

		if progressChan != nil {
			progressChan <- ProgressInfo{
				Percentage:     float64(processed) / float64(totalSize) * 100,
				ProcessedBytes: processed,
				TotalBytes:     totalSize,
				Speed:          speed,
			}
		}
	}

	return nil
}

func DecompressFileWithProgress(inputPath string, progressChan chan ProgressInfo) error {
	if !strings.HasSuffix(inputPath, ".zst") {
		return fmt.Errorf("input file must have .zst extension")
	}

	inputFile, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("failed to open input file: %v", err)
	}
	defer inputFile.Close()

	// Get compressed file size
	fileInfo, err := inputFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to get file info: %v", err)
	}
	compressedSize := fileInfo.Size()

	outputPath := strings.TrimSuffix(inputPath, ".zst")
	outputFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer outputFile.Close()

	decoder, err := zstd.NewReader(inputFile)
	if err != nil {
		return fmt.Errorf("failed to create decoder: %v", err)
	}
	defer decoder.Close()

	buf := make([]byte, 1024*1024) // 1MB buffer
	var processed int64
	startTime := time.Now()

	for {
		n, err := decoder.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read error: %v", err)
		}

		_, err = outputFile.Write(buf[:n])
		if err != nil {
			return fmt.Errorf("write error: %v", err)
		}

		processed += int64(n)
		elapsed := time.Since(startTime).Seconds()
		speed := float64(processed) / elapsed / (1024 * 1024) // MB/s

		if progressChan != nil {
			progressChan <- ProgressInfo{
				Percentage:     float64(processed) / float64(compressedSize) * 100,
				ProcessedBytes: processed,
				TotalBytes:     compressedSize,
				Speed:          speed,
			}
		}
	}

	return nil
}

func CompressJob() error {
	utils.LogDebug(fmt.Sprintf("Compress jobs started to run at %s", time.Now().Format(time.RFC3339)))
	for {
		if utils.AppConfiguration.ArchiveInterval == "" {
			utils.LogError("Archive interval is not set, waiting for config to be updated")
			time.Sleep(3 * time.Second)
			continue
		}
		utils.LogDebug(fmt.Sprintf("Archive interval is set to %s", utils.AppConfiguration.ArchiveInterval))
		break
	}
	interval, err := utils.ParseDuration(utils.AppConfiguration.ArchiveInterval)
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to parse archive interval: %v", err))
		return err
	}
	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Create archive directory if it doesn't exist
		archiveDir := filepath.Join(utils.AppConfiguration.DataDir, "archive")
		if err := os.MkdirAll(archiveDir, 0755); err != nil {
			utils.LogError(fmt.Sprintf("Failed to create archive directory: %v", err))
			return
		}

		// Function to process files that need compression
		processFiles := func() error {
			// Get all files in the snapshot directory
			snapshotDir := filepath.Join(utils.AppConfiguration.DataDir, "snapshot")
			files, err := os.ReadDir(snapshotDir)
			if err != nil {
				return fmt.Errorf("failed to read snapshot directory: %v", err)
			}

			for _, file := range files {
				if file.IsDir() {
					continue
				}

				// Skip already compressed files, log files, and non-img files
				if !strings.HasSuffix(file.Name(), ".img") ||
					strings.HasSuffix(file.Name(), ".zst") {
					continue
				}

				filePath := filepath.Join(snapshotDir, file.Name())
				fileInfo, err := file.Info()
				if err != nil {
					utils.LogError(fmt.Sprintf("Failed to get file info for %s: %v", filePath, err))
					continue
				}

				// Check if file is older than archive interval
				if time.Since(fileInfo.ModTime()) > interval {
					utils.LogDebug(fmt.Sprintf("Processing file for archival: %s", filePath))

					// Create progress channel
					progressChan := make(chan ProgressInfo)

					// Start progress monitoring goroutine
					actionId := strings.TrimSuffix(file.Name(), ".img")
					archiveId := utils.AppConfiguration.DataDir + "/archive/" + file.Name() + ".zst"
					go UpdateCompressProgress(progressChan, actionId, archiveId)

					// Compress the file
					if err := CompressFileWithProgress(filePath, progressChan); err != nil {
						utils.LogError(fmt.Sprintf("Failed to compress %s: %v", filePath, err))
						close(progressChan)
						continue
					}
					close(progressChan)

					// Move compressed file to archive directory
					compressedPath := filePath + ".zst"
					archivePath := filepath.Join(archiveDir, file.Name()+".zst")

					if err := os.Rename(compressedPath, archivePath); err != nil {
						utils.LogError(fmt.Sprintf("Failed to move compressed file to archive: %v", err))
						continue
					}

					// Move corresponding log file if it exists
					logPath := strings.TrimSuffix(filePath, ".img") + ".log"
					archiveLogPath := filepath.Join(archiveDir, strings.TrimSuffix(file.Name(), ".img")+".log")

					// Check if log file exists before trying to move it
					if _, err := os.Stat(logPath); err == nil {
						if err := os.Rename(logPath, archiveLogPath); err != nil {
							utils.LogError(fmt.Sprintf("Failed to move log file to archive: %v", err))
						}
					} else {
						utils.LogDebug(fmt.Sprintf("No log file found for %s", file.Name()))
					}

					// Remove original file after successful compression and move
					if err := os.Remove(filePath); err != nil {
						utils.LogError(fmt.Sprintf("Failed to remove original file: %v", err))
					} else {
						utils.LogDebug(fmt.Sprintf("Successfully compressed and archived %s", file.Name()))
					}
				}
			}
			return nil
		}

		// Run initial compression for all agents
		if err := processFiles(); err != nil {
			utils.LogError(fmt.Sprintf("Initial compression for all agents failed: %v", err))
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Then run periodic compression
		for {
			select {
			case <-ctx.Done():
				utils.LogDebug("Compress jobs stopped")
				return
			case <-ticker.C:
				if err := processFiles(); err != nil {
					utils.LogError(fmt.Sprintf("Periodic compression failed: %v", err))
				}
			}
		}
	}()

	return nil
}

func UpdateCompressProgress(progressChan chan ProgressInfo, actionId string, archiveId string) {
	lastUpdate := time.Now()
	minUpdateInterval := 2 * time.Second // Minimum time between updates

	agentId := strings.Split(actionId, "_")[0]
	diskName := strings.Split(actionId, "_")[1]
	diskName = strings.Replace(diskName, "-", "/", -1)
	action, err := service.GetAction(actionId)
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to get action for agent %s: %v creating new action", actionId, err))
		action.Id = actionId
		action.ActionType = string(utils.CONST_SCHEDULED_ACTION)
		action.Action = string(utils.CONST_COMPRESS_ACTION)
		action.ActionStatus = string(utils.CONST_ACTION_STATUS_IN_PROGRESS)
		action.AgentId = agentId
		action.TimeStarted = time.Now().Unix()
		action.Disk = make(map[string]utils.DiskSnapshot)
		action.Disk[diskName] = utils.DiskSnapshot{
			SnapshotId: archiveId,
			Name:       diskName,
			Progress:   1,
			Status:     string(utils.CONST_ACTION_STATUS_IN_PROGRESS),
		}
		action.UpdateBackend = true
		action.ActionProgress = 1
		if err := service.InsertOrUpdateAction(action); err != nil {
			utils.LogError(fmt.Sprintf("Failed to update action for agent %s: %v", agentId, err))
			return
		}
	}
	for progress := range progressChan {
		// utils.LogDebug(fmt.Sprintf("Compressing: %.2f%% complete (%.2f MB/s)",
		// 	progress.Percentage,
		// 	progress.Speed))
		if progress.Percentage == 100 || time.Since(lastUpdate) > minUpdateInterval {
			action.ActionProgress = int(progress.Percentage)
			action.UpdateBackend = true
			action.ActionStatus = string(utils.CONST_ACTION_STATUS_COMPLETED)
			action.TimeFinished = time.Now().Unix()
		} else {
			action.ActionProgress = int(progress.Percentage)
			if _, exists := action.Disk[diskName]; !exists {
				action.Disk[diskName] = utils.DiskSnapshot{
					Name:     diskName,
					Progress: int(progress.Percentage),
					Status:   string(utils.CONST_ACTION_STATUS_IN_PROGRESS),
				}
			}
			action.UpdateBackend = true
			action.TimeUpdated = time.Now().Unix()
		}
		if err := service.InsertOrUpdateAction(action); err != nil {
			utils.LogError(fmt.Sprintf("Failed to update action for agent %s: %v", agentId, err))
			return
		}
	}
}
