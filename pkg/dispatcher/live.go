package dispatcher

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/xmigrate/blxrep/service"
	"github.com/xmigrate/blxrep/utils"

	"github.com/gorilla/websocket"
	"github.com/klauspost/compress/zstd"
)

const (
	BlockSize       = 512 // Fixed block size
	BlockHeaderSize = 8   // 8 bytes for block number
)

type BlockData struct {
	Number uint64
	Data   [BlockSize]byte
}

type BackupWriter struct {
	baseDir     string
	agentID     string
	srcPath     string
	currentFile *os.File
	encoder     *zstd.Encoder
	mu          sync.Mutex
	lastMinute  string
	buffer      []BlockData
}

func NewBackupWriter(baseDir, agentID, srcPath string) *BackupWriter {
	return &BackupWriter{
		baseDir: baseDir,
		agentID: agentID,
		srcPath: srcPath,
		buffer:  make([]BlockData, 0, 100), // Buffer 100 blocks before writing

	}
}

func (bw *BackupWriter) Write(block BlockData) error {
	bw.mu.Lock()
	defer bw.mu.Unlock()

	bw.buffer = append(bw.buffer, block)

	if len(bw.buffer) >= cap(bw.buffer) {
		if err := bw.flushBuffer(); err != nil {
			return fmt.Errorf("failed to flush buffer: %w", err)
		}

	}
	return nil
}

func (bw *BackupWriter) flushBuffer() error {
	currentMinute := time.Now().Format("200601021504")
	if currentMinute != bw.lastMinute {
		if err := bw.rotateFile(currentMinute); err != nil {
			return fmt.Errorf("failed to rotate file: %w", err)
		}
	}

	for _, block := range bw.buffer {
		header := make([]byte, BlockHeaderSize)
		binary.LittleEndian.PutUint64(header, block.Number)

		if _, err := bw.encoder.Write(header); err != nil {
			return fmt.Errorf("failed to write block header: %w", err)
		}

		if _, err := bw.encoder.Write(block.Data[:]); err != nil {
			return fmt.Errorf("failed to write block data: %w", err)
		}
	}

	bw.buffer = bw.buffer[:0] // Clear the buffer
	return nil
}

func (bw *BackupWriter) rotateFile(minute string) error {
	if bw.currentFile != nil {
		bw.encoder.Close()
		bw.currentFile.Close()
	}

	safePath := strings.ReplaceAll(bw.srcPath, "/", "-")
	filename := fmt.Sprintf("%s_%s_%s.bak", bw.agentID, safePath, minute)
	filepath := filepath.Join(bw.baseDir, filename)

	file, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	bw.currentFile = file
	bw.encoder, err = zstd.NewWriter(file)
	if err != nil {
		return fmt.Errorf("failed to create zstd encoder: %w", err)
	}

	bw.lastMinute = minute
	return nil
}

func (bw *BackupWriter) Close() error {
	bw.mu.Lock()
	defer bw.mu.Unlock()

	if err := bw.flushBuffer(); err != nil {
		return fmt.Errorf("failed to flush buffer on close: %w", err)
	}

	if bw.currentFile != nil {
		if err := bw.encoder.Close(); err != nil {
			return fmt.Errorf("failed to close encoder: %w", err)
		}
		if err := bw.currentFile.Close(); err != nil {
			return fmt.Errorf("failed to close file: %w", err)
		}
	}
	return nil
}

func ChangeSectorTracker(c *chan utils.AgentBulkMessage, agentID string) {
	utils.LogDebug("Starting change sector tracking process for agent: " + agentID)
	fileCreationInterval := 2 * time.Minute
	ticker := time.NewTicker(fileCreationInterval)
	defer ticker.Stop()

	fileHandlers := make(map[string]*os.File)
	defer func() {
		for _, f := range fileHandlers {
			f.Close()
		}
	}()

	sectorBufferMap := make(map[string][]utils.LiveSector)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	writeBufferToFile := func() {
		for srcPath, sectors := range sectorBufferMap {
			if len(sectors) == 0 {
				continue
			}

			safePath := strings.ReplaceAll(srcPath, "/", "-")
			timeStamp := time.Now().Format("20060102150405")
			filename := fmt.Sprintf("_%s_%s.cst", safePath, timeStamp)
			incremental_filepath := filepath.Join(utils.AppConfiguration.DataDir, "incremental", agentID+filename)

			f, exists := fileHandlers[incremental_filepath]
			if !exists {
				if err := os.MkdirAll(filepath.Dir(incremental_filepath), 0755); err != nil {
					utils.LogError("Failed to create directory: " + err.Error())
					continue
				}
				var err error
				f, err = os.OpenFile(incremental_filepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
				if err != nil {
					utils.LogError("Cannot open file " + filename + ": " + err.Error())
					continue
				}
				fileHandlers[incremental_filepath] = f
			}

			for _, sector := range sectors {
				_, err := f.WriteString(fmt.Sprintf("%d:%d\n", sector.StartSector, sector.EndSector))
				if err != nil {
					utils.LogError("Error writing to file " + filename + ": " + err.Error())
				}
			}

			utils.LogDebug("Wrote " + fmt.Sprintf("%d", len(sectors)) + " sectors to file " + filename)
		}
		// Clear all buffers after writing
		sectorBufferMap = make(map[string][]utils.LiveSector)
	}

	for {
		select {
		case sig := <-sigs:
			utils.LogDebug("Received signal: " + fmt.Sprintf("%v", sig))
			utils.LogDebug("Change sector tracking was terminated for agent: " + agentID)
			writeBufferToFile() // Write any remaining sectors before exiting
			os.Exit(0)

		case <-ticker.C:
			writeBufferToFile()

		case sector, ok := <-*c:
			if !ok {
				utils.LogDebug("Channel closed inside ChangeSectorTracker for agent: " + agentID)
				writeBufferToFile() // Write any remaining sectors before exiting
				return
			}
			utils.LogDebug("Received sector: " + fmt.Sprintf("%d", sector.StartSector) + " - " + fmt.Sprintf("%d", sector.EndSector) + " for path: " + sector.SrcPath)
			changedSector := utils.LiveSector{
				StartSector: sector.StartSector,
				EndSector:   sector.EndSector,
				SrcPath:     sector.SrcPath,
				AgentID:     sector.AgentID,
			}

			// Append to the appropriate buffer based on SrcPath
			if _, exists := sectorBufferMap[sector.SrcPath]; !exists {
				sectorBufferMap[sector.SrcPath] = make([]utils.LiveSector, 0)
			}
			sectorBufferMap[sector.SrcPath] = append(sectorBufferMap[sector.SrcPath], changedSector)
		}
	}
}

func readSectorsFromFile(filePath string) ([]utils.BlockPair, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close()

	var sectorPairs []utils.BlockPair
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid line format: %s", line)
		}

		start, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("error parsing start sector: %v", err)
		}

		end, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("error parsing end sector: %v", err)
		}

		sectorPairs = append(sectorPairs, utils.BlockPair{
			Start: start,
			End:   end,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %v", err)
	}

	return sectorPairs, nil
}

func sortAndDeduplicate(pairs []utils.BlockPair) []utils.BlockPair {
	if len(pairs) <= 1 {
		return pairs
	}

	// Sort the pairs
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Start != pairs[j].Start {
			return pairs[i].Start < pairs[j].Start
		}
		return pairs[i].End < pairs[j].End
	})

	// Deduplicate and merge overlapping or adjacent sectors
	result := make([]utils.BlockPair, 0, len(pairs))
	current := pairs[0]

	for i := 1; i < len(pairs); i++ {
		if pairs[i].Start <= current.End+1 {
			// Overlapping or adjacent sectors, merge them
			if pairs[i].End > current.End {
				current.End = pairs[i].End
			}
		} else {
			// Non-overlapping sector, add the current one to result and move to the next
			result = append(result, current)
			current = pairs[i]
		}
	}

	// Add the last sector pair
	result = append(result, current)

	return result
}

func SyncData(conn *websocket.Conn, agentID string, force bool) {
	age := time.Duration(utils.AppConfiguration.SyncFreq) * time.Minute
	filePattern := fmt.Sprintf("%s_*.cst", agentID)
	pattern := filepath.Join(utils.AppConfiguration.DataDir, "incremental", filePattern)
	utils.LogDebug("Looking for files matching pattern " + pattern)

	matches, err := filepath.Glob(pattern)
	if err != nil {
		utils.LogError("Error finding files for agent " + agentID + ": " + err.Error())
		return
	}

	// Compile regex to extract timestamp and srcPath
	re := regexp.MustCompile(fmt.Sprintf(`^%s_(.+)_(\d{14})\.cst$`, agentID))

	// Group files by srcPath
	filesBySrcPath := make(map[string][]string)
	now := time.Now()

	for _, file := range matches {
		filename := filepath.Base(file)
		match := re.FindStringSubmatch(filename)
		if len(match) < 3 {
			utils.LogError("Unexpected filename format: " + filename)
			continue
		}

		srcPath := strings.ReplaceAll(match[1], "-", "/") // Convert back to original path

		if !force {
			// Only check timestamp if not forced
			timestampStr := match[2]
			timestamp, err := time.Parse("20060102150405", timestampStr)
			if err != nil {
				utils.LogError("Error parsing timestamp from filename " + filename + ": " + err.Error())
				continue
			}

			if now.Sub(timestamp) <= age {
				continue // Skip files that aren't old enough
			}
		}

		filesBySrcPath[srcPath] = append(filesBySrcPath[srcPath], file)
	}

	if len(filesBySrcPath) == 0 {
		if force {
			utils.LogDebug("No .cst files found for agent " + agentID)
		} else {
			utils.LogDebug("No files old enough for agent " + agentID)
		}
		return
	}

	// Process each srcPath separately
	for srcPath, files := range filesBySrcPath {
		var sectorPairs []utils.BlockPair

		// Get dirty blocks from BoltDB for this disk
		dirtyBlocks, err := service.GetDirtyBlocks(agentID, srcPath)
		if err != nil {
			utils.LogError("Error getting dirty blocks from database: " + err.Error())
		} else if len(dirtyBlocks) > 0 {
			// Process dirty blocks
			for _, block := range dirtyBlocks {
				// Add block to sector pairs for retry
				sectorPairs = append(sectorPairs, utils.BlockPair{
					Start: uint64(block.BlockNumber),
					End:   uint64(block.BlockNumber) + 1,
				})

				// Update retry information in the database
				if err := service.UpdateBlockRetry(agentID, srcPath, block.BlockNumber); err != nil {
					utils.LogError(fmt.Sprintf("Error updating retry info for block %d: %s",
						block.BlockNumber, err.Error()))
				}

				utils.LogDebug(fmt.Sprintf("Added dirty block %d from disk %s for retry (attempt %d)",
					block.BlockNumber, srcPath, block.RetryCount+1))
			}
		}

		// Read sector pairs from files
		for _, file := range files {
			pairs, err := readSectorsFromFile(file)
			if err != nil {
				utils.LogError("Error reading sectors from " + file + ": " + err.Error())
				continue
			}
			sectorPairs = append(sectorPairs, pairs...)
		}

		// Sort and deduplicate sector pairs only if not forced
		if !force {
			sectorPairs = sortAndDeduplicate(sectorPairs)
		}

		// Send sector pairs in chunks
		const chunkSize = 40000
		for i := 0; i < len(sectorPairs); i += chunkSize {
			end := i + chunkSize
			if end > len(sectorPairs) {
				end = len(sectorPairs)
			}

			chunk := sectorPairs[i:end]

			actionMsg := utils.Message{
				Action: utils.CONST_AGENT_ACTION_SYNC,
				SyncMessage: utils.SyncData{
					BlockSize:   512,
					ChannelSize: 12000,
					Blocks:      chunk,
					SrcPath:     srcPath,
				},
			}

			if err := conn.WriteJSON(actionMsg); err != nil {
				utils.LogError("Failed to send message chunk " + fmt.Sprintf("%d", i/chunkSize+1) +
					" for srcPath " + srcPath + " to agent " + agentID + ": " + err.Error())
				continue
			}

			utils.LogDebug("Sent chunk " + fmt.Sprintf("%d", i/chunkSize+1) + "/" +
				fmt.Sprintf("%d", (len(sectorPairs)+chunkSize-1)/chunkSize) +
				" to agent " + agentID + " for srcPath " + srcPath +
				" with " + fmt.Sprintf("%d", len(chunk)) + " sector pairs")

			time.Sleep(100 * time.Millisecond)
		}
	}

	// Rename processed files
	for _, files := range filesBySrcPath {
		for _, file := range files {
			newName := file + ".done"
			if err := os.Rename(file, newName); err != nil {
				utils.LogError("Error renaming file " + file + " to " + newName + ": " + err.Error())
			} else {
				utils.LogDebug("Renamed file " + file + " to " + newName)
			}
		}
	}
}

func getLatestSnapshotFile(dataDir, agentID, diskPath string) (string, error) {
	// Convert disk path to match file naming (e.g., /dev/xvda -> -dev-xvda)
	safeDiskPath := strings.ReplaceAll(diskPath, "/", "-")

	// Get all files in snapshot directory
	pattern := filepath.Join(dataDir, "snapshot", fmt.Sprintf("%s_%s_*.img", agentID, safeDiskPath))
	files, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("failed to list snapshot files: %w", err)
	}

	if len(files) == 0 {
		return "", fmt.Errorf("no snapshot files found for agent %s and disk %s", agentID, diskPath)
	}

	// Sort files by name (timestamp is part of name)
	sort.Strings(files)

	// Return the last (latest) file
	return files[len(files)-1], nil
}

func WriteIncrementalBackupToFile(c *chan utils.AgentBulkMessage, agentID string, blockSize int) {
	backupWriters := make(map[string]*BackupWriter)
	// Map to store snapshot file handles
	snapshotFiles := make(map[string]*os.File)
	defer func() {
		for _, writer := range backupWriters {
			writer.Close()
		}
		for _, file := range snapshotFiles {
			file.Close()
		}
	}()
	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case msg, ok := <-*c:
			if !ok {
				utils.LogDebug("Channel closed for agent: " + agentID)
				return
			}
			timeout.Reset(60 * time.Second)
			writerKey := fmt.Sprintf("%s_%s", agentID, msg.SrcPath)

			writer, ok := backupWriters[writerKey]
			if !ok {
				// Create new writer for this agentID + srcPath combination
				safePath := strings.ReplaceAll(msg.SrcPath, "/", "-")
				writer = NewBackupWriter(filepath.Join(utils.AppConfiguration.DataDir, "incremental"),
					agentID, safePath)
				backupWriters[writerKey] = writer
				utils.LogDebug("Created new backup writer for agent: " + agentID + ", SrcPath: " + msg.SrcPath)
			}

			// Get or open snapshot file
			snapshotFile, ok := snapshotFiles[writerKey]
			if !ok {
				latestSnapshot, err := getLatestSnapshotFile(utils.AppConfiguration.DataDir, agentID, msg.SrcPath)
				if err != nil {
					utils.LogError("Failed to find latest snapshot: " + err.Error())
					continue
				}

				snapshotFile, err = os.OpenFile(latestSnapshot, os.O_RDONLY, 0644)
				if err != nil {
					utils.LogError("Failed to open snapshot file: " + err.Error())
					continue
				}
				snapshotFiles[writerKey] = snapshotFile
			}

			utils.LogDebug("Processing message for agent: " + agentID + ", SrcPath: " + msg.SrcPath)

			for _, blockData := range msg.Data {
				if len(blockData.BlockData) != 512 {
					utils.LogError("Warning: Received block data with unexpected size for block " + fmt.Sprintf("%d", blockData.BlockNumber) + ", agent " + agentID + ", SrcPath: " + msg.SrcPath)
					continue
				}

				// Read corresponding block from snapshot
				snapshotBlock := make([]byte, 512)
				offset := int64(blockData.BlockNumber) * 512
				_, err := snapshotFile.ReadAt(snapshotBlock, offset)
				if err != nil && err != io.EOF {
					utils.LogError("Failed to read snapshot block: " + err.Error())
					continue
				}
				// Check if block is in dirty list
				isDirty, err := service.IsBlockDirty(agentID, msg.SrcPath, int64(blockData.BlockNumber))
				if err != nil {
					utils.LogError("Failed to check if block is dirty: " + err.Error())
					continue
				} // Compare blocks
				if !bytes.Equal(snapshotBlock, blockData.BlockData) {
					var block BlockData
					block.Number = blockData.BlockNumber
					copy(block.Data[:], blockData.BlockData)

					err := writer.Write(block)
					if err != nil {
						utils.LogError("Failed to write block " + fmt.Sprintf("%d", blockData.BlockNumber))
					}
					// If block was in dirty list, remove it as it's now changed
					if isDirty {
						if err := service.RemoveBlock(agentID, msg.SrcPath, int64(blockData.BlockNumber)); err != nil {
							utils.LogError("Failed to remove block from dirty list: " + err.Error())
						}
					}
				} else {
					if isDirty {
						// Block is still unchanged, update retry information
						if err := service.UpdateBlockRetry(agentID, msg.SrcPath, int64(blockData.BlockNumber)); err != nil {
							utils.LogError("Failed to update dirty block retry info: " + err.Error())
						}
					} else {
						// New unchanged block, add to dirty list
						if err := service.AddDirtyBlock(agentID, msg.SrcPath, int64(blockData.BlockNumber)); err != nil {
							utils.LogError("Failed to log dirty block: " + err.Error())
						}
					}
				}
			}
			writer.flushBuffer()

			utils.LogDebug("Finished processing message for agent: " + agentID + ", SrcPath: " + msg.SrcPath)

		case <-timeout.C:
			utils.LogDebug("No incremental data received for 60 seconds for agent: " + agentID)
			timeout.Reset(60 * time.Second)
		}
	}
}

func processBackupFile(backupPath string, snapshotFile *os.File) error {
	backupFile, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("failed to open backup file: %w", err)
	}
	defer backupFile.Close()

	decoder, err := zstd.NewReader(backupFile)
	if err != nil {
		return fmt.Errorf("failed to create zstd decoder: %w", err)
	}
	defer decoder.Close()

	header := make([]byte, BlockHeaderSize)
	data := make([]byte, BlockSize)

	blockCount := 0
	for {
		_, err := io.ReadFull(decoder, header)
		if err == io.EOF {
			// Reached end of file normally
			break
		}
		if err != nil {
			if err == io.ErrUnexpectedEOF {
				utils.LogError("Warning: Incomplete header in backup file " + backupPath + " after " + fmt.Sprintf("%d", blockCount) + " blocks")
				break
			}
			utils.LogError("failed to read block header after " + fmt.Sprintf("%d", blockCount) + " blocks: " + err.Error())
			break
		}

		blockNumber := binary.LittleEndian.Uint64(header)

		n, err := io.ReadFull(decoder, data)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				utils.LogError("Warning: Incomplete block data in backup file " + backupPath + " for block " + fmt.Sprintf("%d", blockNumber))
				break
			}

			utils.LogError("failed to read block data for block " + fmt.Sprintf("%d", blockNumber) + ": " + err.Error() + " size is " + fmt.Sprintf("%d", n))
			continue
		}
		if n < BlockSize {
			utils.LogError("Warning: Incomplete block data in backup file " + backupPath + " for block " + fmt.Sprintf("%d", blockNumber) + " (read " + fmt.Sprintf("%d", n) + " bytes)")
		}

		// Write the block to the snapshot file
		offset := int64(blockNumber) * BlockSize
		_, err = snapshotFile.WriteAt(data[:n], offset)
		if err != nil {
			return fmt.Errorf("failed to write block %d to snapshot: %w", blockNumber, err)
		}

		blockCount++
	}

	utils.LogDebug("Processed " + fmt.Sprintf("%d", blockCount) + " blocks from backup file " + backupPath)
	return nil
}

func extractTimestamp(filename string) string {
	// Extract the timestamp from the filename (now in YYYYMMDDHHMM format)
	parts := strings.Split(filename, "_")
	lastPart := parts[len(parts)-1]

	// Remove .img or .bak extension if present
	timestamp := strings.TrimSuffix(strings.TrimSuffix(lastPart, ".img"), ".bak")

	// Validate timestamp format
	if len(timestamp) == 12 {
		if _, err := time.Parse("200601021504", timestamp); err == nil {
			return timestamp
		}
	}
	return ""
}

func ProcessIncrementalBackups(baseDir, agentID, snapshotPath string, checkpoint string) error {
	// Find all .bak files for the given agent and disk
	pattern := filepath.Join(baseDir, fmt.Sprintf("%s*.bak", agentID))
	files, err := filepath.Glob(pattern)

	if err != nil {
		return fmt.Errorf("failed to find backup files: %w", err)
	}

	// Sort files by timestamp
	sort.Slice(files, func(i, j int) bool {
		return extractTimestamp(files[i]) < extractTimestamp(files[j])
	})

	var selectedFiles []string
	checkpointTime, err := time.Parse("200601021504", checkpoint)
	if err != nil {
		return fmt.Errorf("invalid checkpoint format: %w", err)
	}

	for _, file := range files {
		fileTime, err := time.Parse("200601021504", extractTimestamp(file))
		if err != nil {
			utils.LogError("Warning: Unable to parse timestamp from file " + file + ": " + err.Error())
			continue
		}
		if fileTime.After(checkpointTime) {
			break
		}
		selectedFiles = append(selectedFiles, file)
	}

	if len(selectedFiles) == 0 {
		utils.LogDebug("No incremental changes found with given checkpoint: " + checkpoint)
		return nil
	} else {
		utils.LogDebug(fmt.Sprintf("Processing %d incremental backups for agent %s", len(selectedFiles), agentID))
	}

	// Open the snapshot file
	snapshotFile, err := os.OpenFile(snapshotPath, os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("failed to open snapshot file: %w", err)
	}
	defer snapshotFile.Close()

	// Process each .bak file
	for _, file := range selectedFiles {
		err := processBackupFile(file, snapshotFile)
		if err != nil {
			return fmt.Errorf("failed to process backup file %s: %w", file, err)
		}
	}

	return nil
}

func latestSnapshot(dataDir string, agent string) (string, error) {
	files, err := os.ReadDir(filepath.Join(dataDir, "snapshot"))
	if err != nil {
		return "", fmt.Errorf("error reading snapshot directory: %w", err)
	}
	var filteredFiles []os.DirEntry
	for _, file := range files {
		if strings.HasPrefix(file.Name(), agent) && strings.HasSuffix(file.Name(), ".img") {
			filteredFiles = append(filteredFiles, file)
		}
	}
	sort.Slice(filteredFiles, func(i, j int) bool {
		return filteredFiles[i].Name() < filteredFiles[j].Name()
	})
	return filteredFiles[len(filteredFiles)-1].Name(), nil
}

func processAndCheckCompletion(agentID string, checkpoint string, dataDir string) bool {
	snapshot, err := latestSnapshot(dataDir, agentID)
	if err != nil {
		return false
	}
	snapshot = filepath.Join(dataDir, "snapshot", snapshot)
	finalSnapshot := filepath.Join(dataDir, "final", fmt.Sprintf("%s-%s.img", agentID, checkpoint))
	err = utils.CopyFile(snapshot, finalSnapshot)
	if err != nil {
		utils.LogError("Error copying snapshot to final location: " + err.Error())
		return false
	}
	utils.LogDebug(fmt.Sprintf("Copying snapshot to final location: %s", finalSnapshot))

	err = ProcessIncrementalBackups(dataDir+"/incremental", agentID, finalSnapshot, checkpoint)
	if err != nil {
		utils.LogError("Failed to process incremental backups for agent " + agentID + ": " + err.Error())
		return false
	}

	pattern := filepath.Join(dataDir, "incremental", fmt.Sprintf("%s*.bak", agentID))
	files, err := filepath.Glob(pattern)
	if err != nil {
		utils.LogError("Error checking for remaining .bak files for agent " + agentID + ": " + err.Error())
		return false
	}

	if len(files) == 0 {
		utils.LogDebug("All incremental backups processed for agent: " + agentID)
		return true
	} else {
		utils.LogDebug("Remaining incremental backups for agent " + agentID + ": " + strings.Join(files, ", "))
		return false
	}
}

func TotalSyncData(conn *websocket.Conn, agentID string) {
	age := 15 * time.Minute
	filePattern := fmt.Sprintf("%s_*.cst.done", agentID)
	pattern := filepath.Join(utils.AppConfiguration.DataDir, "incremental", filePattern)
	utils.LogDebug("Looking for files matching pattern " + pattern)

	matches, err := filepath.Glob(pattern)
	if err != nil {
		utils.LogError("Error finding files for agent " + agentID + ": " + err.Error())
		return
	}

	// Updated regex to match current format
	re := regexp.MustCompile(fmt.Sprintf(`^%s_(.+)_(\d{14})\.cst\.done$`, agentID))

	// Group files by srcPath
	filesBySrcPath := make(map[string][]string)
	now := time.Now()

	for _, file := range matches {
		filename := filepath.Base(file)
		match := re.FindStringSubmatch(filename)
		if len(match) < 3 {
			utils.LogError("Unexpected filename format: " + filename)
			continue
		}

		srcPath := strings.ReplaceAll(match[1], "-", "/") // Convert back to original path
		timestampStr := match[2]
		timestamp, err := time.Parse("20060102150405", timestampStr)
		if err != nil {
			utils.LogError("Error parsing timestamp from filename " + filename + ": " + err.Error())
			continue
		}

		if now.Sub(timestamp) > age {
			filesBySrcPath[srcPath] = append(filesBySrcPath[srcPath], file)
		}
	}

	if len(filesBySrcPath) == 0 {
		utils.LogDebug("No files old enough for agent " + agentID)
		return
	}

	// Process each srcPath separately
	for srcPath, files := range filesBySrcPath {
		var sectorPairs []utils.BlockPair
		for _, file := range files {
			pairs, err := readSectorsFromFile(file)
			if err != nil {
				utils.LogError("Error reading sectors from " + file + ": " + err.Error())
				continue
			}
			sectorPairs = append(sectorPairs, pairs...)
		}

		// Sort and deduplicate sector pairs for this srcPath
		sectorPairs = sortAndDeduplicate(sectorPairs)

		const chunkSize = 40000
		for i := 0; i < len(sectorPairs); i += chunkSize {
			end := i + chunkSize
			if end > len(sectorPairs) {
				end = len(sectorPairs)
			}

			chunk := sectorPairs[i:end]

			actionMsg := utils.Message{
				Action: utils.CONST_AGENT_ACTION_SYNC,
				SyncMessage: utils.SyncData{
					BlockSize:   512,
					ChannelSize: 12000,
					Blocks:      chunk,
					SrcPath:     srcPath, // Include srcPath in message
				},
			}

			if err := conn.WriteJSON(actionMsg); err != nil {
				utils.LogError("Failed to send message chunk " + fmt.Sprintf("%d", i/chunkSize+1) + " for srcPath " + srcPath + " to agent " + agentID + ": " + err.Error())
				continue
			}

			utils.LogDebug("Sent chunk " + fmt.Sprintf("%d", i/chunkSize+1) + "/" + fmt.Sprintf("%d", (len(sectorPairs)+chunkSize-1)/chunkSize) + " to agent " + agentID + " for srcPath " + srcPath + " with " + fmt.Sprintf("%d", len(chunk)) + " sector pairs")

			time.Sleep(100 * time.Millisecond)
		}

		// Rename processed files for this srcPath
		for _, file := range files {
			newName := file + ".total.done"
			if err := os.Rename(file, newName); err != nil {
				utils.LogError("Error renaming file " + file + " to " + newName + ": " + err.Error())
			} else {
				utils.LogDebug("Renamed file " + file + " to " + newName)
			}
		}
	}
}
