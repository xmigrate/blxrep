package dispatcher

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/xmigrate/blxrep/service"
	"github.com/xmigrate/blxrep/utils"
)

func WriteFullBackupToFile(c *chan utils.AgentBulkMessage, agentID string, progress *int) {
	var fileHandlers map[string]*os.File = make(map[string]*os.File)
	var logHandlers map[string]*os.File = make(map[string]*os.File)
	defer func() {
		for _, f := range fileHandlers {
			f.Close()
		}
		for _, f := range logHandlers {
			f.Close()
		}
	}()
	fileTimestamp := time.Now().Format("200601021504")
	lastProgressUpdate := time.Now()
	timeoutDuration := 3 * time.Second
	bytesWrittenMap := make(map[string]int64)
	var originalStartTime int64
	var totalBlocks uint64
	var filename string
	for {
		select {
		case msg, ok := <-*c:
			if !ok {
				return
			}

			if originalStartTime == 0 {
				originalStartTime = msg.StartTime
				totalBlocks = msg.TotalBlocks
			}

			safePath := strings.ReplaceAll(msg.SrcPath, "/", "-")
			filename = filepath.Join(utils.AppConfiguration.DataDir, "snapshot",
				fmt.Sprintf("%s_%s_%s.img", agentID, safePath, fileTimestamp))

			f, exists := fileHandlers[filename]
			if !exists {
				if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
					utils.LogError(fmt.Sprintf("Failed to create directory: %v", err))
					return
				}
				var err error
				f, err = os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
				if err != nil {
					utils.LogError(fmt.Sprintf("Cannot open file %s: %v", filename, err))
					return
				}
				fileHandlers[filename] = f
				bytesWrittenMap[filename] = 0

				logFilename := strings.ReplaceAll(filename, ".img", ".log")
				logFile, err := os.OpenFile(logFilename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
				if err != nil {
					utils.LogError(fmt.Sprintf("Failed to open log file: %v", err))
					return
				}
				logHandlers[filename] = logFile
			}
			var lastBlockNumber uint64
			for _, block := range msg.Data {
				n, err := f.Write(block.BlockData)
				if err != nil {
					utils.LogError(fmt.Sprintf("Write failed: %v", err))
					return
				}
				bytesWrittenMap[filename] += int64(n)
				lastBlockNumber = block.BlockNumber
			}
			if logFile := logHandlers[filename]; logFile != nil {
				if _, err := logFile.Seek(0, 0); err != nil {
					utils.LogError(fmt.Sprintf("Failed to seek in log file: %v", err))
					return
				}
				if _, err := fmt.Fprintf(logFile, "%d", lastBlockNumber); err != nil {
					utils.LogError(fmt.Sprintf("Failed to write to log file: %v", err))
					return
				}
			}
			if time.Since(lastProgressUpdate) >= 400*time.Millisecond {
				actionId := strings.Join([]string{agentID, fmt.Sprintf("%d", msg.StartTime)}, "_")
				go UpdateProgress(msg, progress, actionId, agentID, filename)
				lastProgressUpdate = time.Now()
			}

		case <-time.After(timeoutDuration):
			utils.LogDebug(fmt.Sprintf("No data for %v. Exiting WriteFullBackupToFile for agent %s", timeoutDuration, agentID))

			// Calculate total bytes written across all files
			var totalBytesWritten int64
			for _, bytes := range bytesWrittenMap {
				totalBytesWritten += bytes
			}

			utils.LogDebug(fmt.Sprintf("Total bytes written for agent %s: %d", agentID, totalBytesWritten))

			finalMsg := utils.AgentBulkMessage{
				TotalBlocks: totalBlocks,
				Data:        []utils.AgentDataBlock{{BlockNumber: uint64(totalBytesWritten / int64(utils.CONST_BLOCK_SIZE))}},
				StartTime:   originalStartTime,
			}
			UpdateProgress(finalMsg, progress, fmt.Sprintf("%s_%d", agentID, originalStartTime), agentID, filename)
			return
		}
	}
}

func ResumeBackupToFile(c *chan utils.AgentBulkMessage, blockSize int64, readFrom int64, agentId string, actionId string, progress *int) {
	action, err := service.GetAction(actionId)
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to get action for agent %s: %v", agentId, err))
		return
	}
	// Assuming there is only one disk in the resume action at a time
	var key string
	for k := range action.Disk {
		key = k
	}
	filename := action.Disk[key].SnapshotId
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to get action for agent %s: %v", agentId, err))
		return
	}
	defer file.Close()
	setCurrentBlockNum := int64(0)
	f, err := os.OpenFile(filename, os.O_RDWR, 0644)
	if err != nil {
		utils.LogError(fmt.Sprintf("cannot open file %s dropping message from agent %s", filename, agentId))
		return
	}
	defer f.Close()
	logFilename := strings.Replace(filename, ".img", ".log", -1)
	lf, err := os.OpenFile(logFilename, os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		utils.LogError(fmt.Sprintf("cannot open file %s dropping message from agent %s", logFilename, agentId))
		return
	}
	defer lf.Close()
	for {
		_, err := f.Seek(blockSize, io.SeekCurrent)
		if err != nil && err != io.EOF {
			utils.LogError(fmt.Sprintf("Error reading from snapshot: %v", err))
			return
		}

		if setCurrentBlockNum == readFrom {
			utils.LogDebug(fmt.Sprintf("Seeked in resume to %d", setCurrentBlockNum))
			break
		}
		setCurrentBlockNum++
	}
	utils.LogDebug(fmt.Sprintf("Current block: %d", setCurrentBlockNum))

	timeoutDuration := 3 * time.Second
	lastBlock := int64(0)
	lastProgressUpdate := time.Now()
	progressUpdateInterval := 100 * time.Millisecond
	var originalStartTime int64
	var totalBlocks uint64
	for {
		select {
		case msg, ok := <-*c:
			if !ok {
				utils.LogDebug(fmt.Sprintf("Channel closed, exiting ResumeBackupToFile for agent %s", agentId))
				return
			}
			if originalStartTime == 0 {
				originalStartTime = msg.StartTime
				totalBlocks = msg.TotalBlocks
			}
			for _, blockData := range msg.Data {

				if _, err := f.Write(blockData.BlockData); err != nil {
					utils.LogError(fmt.Sprintf("Failed to write to file: %v", err))
					return
				}

				if _, err := lf.Seek(0, 0); err != nil { // Seek to start of file
					utils.LogError(fmt.Sprintf("Failed to seek in log file for agent %s: %v", agentId, err))
					return
				}
				if _, err := fmt.Fprintf(lf, "%d", blockData.BlockNumber); err != nil {
					utils.LogError(fmt.Sprintf("Failed to write to log file for agent %s: %v", agentId, err))
					return
				}
				lastBlock = int64(blockData.BlockNumber)
			}
			if time.Since(lastProgressUpdate) >= progressUpdateInterval {
				go UpdateProgress(msg, progress, actionId, agentId, filename)
				lastProgressUpdate = time.Now()
			}
		case <-time.After(timeoutDuration):
			// No data in the channel for the duration of timeoutDuration
			utils.LogDebug(fmt.Sprintf("No data for %v. Exiting ResumeBackupToFile for agent %s, last block: %d, total blocks: %d", timeoutDuration, agentId, lastBlock, totalBlocks))
			startTime, err := strconv.ParseInt(strings.Split(actionId, "_")[1], 10, 64)
			if err != nil {
				utils.LogError(fmt.Sprintf("Failed to parse start time for agent %s: %v", agentId, err))
				return
			}
			finalMsg := utils.AgentBulkMessage{
				TotalBlocks: totalBlocks,
				Data:        []utils.AgentDataBlock{{BlockNumber: uint64(totalBlocks)}},
				StartTime:   startTime,
			}
			UpdateProgress(finalMsg, progress, actionId, agentId, filename)

			return
		}
	}
}

func UpdateProgress(msg utils.AgentBulkMessage, progress *int, actionId string, agentId string, snapshotId string) {
	action, err := service.GetAction(actionId)
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to get action for agent %s: %v creating new action", actionId, err))
		action.Id = actionId
		action.ActionType = string(utils.CONST_SCHEDULED_ACTION)
		action.Action = string(utils.CONST_AGENT_ACTION_CLONE)
		action.ActionStatus = string(utils.CONST_ACTION_STATUS_IN_PROGRESS)
		action.AgentId = agentId
		action.TimeStarted = msg.StartTime
		action.Disk = make(map[string]utils.DiskSnapshot)
		if _, exists := action.Disk[msg.SrcPath]; !exists {
			action.Disk[msg.SrcPath] = utils.DiskSnapshot{
				Name:       msg.SrcPath,
				SnapshotId: snapshotId,
				Progress:   60,
				Status:     string(utils.CONST_ACTION_STATUS_IN_PROGRESS),
			}
		}
		action.UpdateBackend = true
		if err := service.InsertOrUpdateAction(action); err != nil {
			utils.LogError(fmt.Sprintf("Failed to update action for agent %s: %v", agentId, err))
			return
		}
	}
	currentBlock := msg.Data[len(msg.Data)-1].BlockNumber

	clonePercentage := int(float64(currentBlock) / float64(msg.TotalBlocks) * 100)
	if clonePercentage > 100 {
		clonePercentage = 100
	}
	if clonePercentage == 100 {
		action.TimeFinished = time.Now().Unix()
		// TODO: This should be changed to overall progress
		action.ActionProgress = clonePercentage
		action.ActionStatus = string(utils.CONST_ACTION_STATUS_COMPLETED)
		base := filepath.Base(snapshotId)
		diskName := strings.ReplaceAll(strings.Split(base, "_")[1], "-", "/")
		log.Printf("UpdateProgress diskName: %s", diskName)
		if _, exists := action.Disk[diskName]; !exists {
			action.Disk[diskName] = utils.DiskSnapshot{
				Name:       diskName,
				SnapshotId: snapshotId,
				Progress:   100,
				Status:     string(utils.CONST_ACTION_STATUS_COMPLETED),
			}
		}
		action.UpdateBackend = true
		if err := service.InsertOrUpdateAction(action); err != nil {
			utils.LogError(fmt.Sprintf("failed to update action for clone percentage: %s", err.Error()))
		}
		utils.LogDebug("replication completed successfully")
	}

	if *progress != clonePercentage {
		*progress = clonePercentage
		action.ActionProgress = clonePercentage
		if _, exists := action.Disk[msg.SrcPath]; exists {
			action.Disk[msg.SrcPath] = utils.DiskSnapshot{
				Name:       msg.SrcPath,
				Progress:   clonePercentage,
				Status:     string(utils.CONST_ACTION_STATUS_IN_PROGRESS),
				SnapshotId: snapshotId,
			}
		}
		action.UpdateBackend = true
		action.TimeUpdated = time.Now().Unix()
		if err := service.InsertOrUpdateAction(action); err != nil {
			utils.LogError(fmt.Sprintf("failed to update action for clone percentage: %s", err.Error()))
		}
		utils.LogDebug(fmt.Sprintf("Progress: %d%%", clonePercentage))
	}
}
