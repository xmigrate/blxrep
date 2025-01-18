package dispatcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xmigrate/blxrep/service"
	"github.com/xmigrate/blxrep/utils"
)

func StartSnapshotCleanupJobs() error {
	utils.LogDebug(fmt.Sprintf("Cleanup jobs started to run at %s", time.Now().Format(time.RFC3339)))

	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		checkInterval := 5 * time.Minute
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()

		// Run initial cleanup for all agents
		if err := cleanupAllAgents(ctx); err != nil {
			utils.LogError(fmt.Sprintf("Initial cleanup for all agents failed: %v", err))
		}

		// Then run periodic cleanup for all agents
		for {
			select {
			case <-ctx.Done():
				utils.LogDebug("Cleanup jobs stopped")
				return
			case <-ticker.C:
				if err := cleanupAllAgents(ctx); err != nil {
					utils.LogError(fmt.Sprintf("Periodic cleanup for all agents failed: %v", err))
				}
			}
		}
	}()

	return nil
}

func cleanupAllAgents(ctx context.Context) error {
	agents, err := service.GetAllAgents(-1)
	if err != nil {
		return fmt.Errorf("failed to get agents: %w", err)
	}

	for _, agent := range agents {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := performCleanup(ctx, agent.AgentId, agent.SnapshotRetention); err != nil {
				utils.LogError(fmt.Sprintf("Cleanup failed for agent %s: %v", agent.AgentId, err))
				// Continue with other agents even if one fails
				continue
			}
			utils.LogDebug(fmt.Sprintf("Cleanup completed for agent %s", agent.AgentId))
		}
	}

	return nil
}

func performCleanup(ctx context.Context, agentID string, snapshotRetention int) error {
	// Get latest snapshot times per disk first
	latestSnapshotTimes, err := getLatestSnapshotTimes(ctx, agentID)
	if err != nil {
		return fmt.Errorf("error getting latest snapshot times: %w", err)
	}

	// Perform both cleanups
	if err := cleanupSnapshots(ctx, agentID, latestSnapshotTimes, snapshotRetention); err != nil {
		utils.LogError(fmt.Sprintf("Snapshot cleanup failed: %v", err))
	}

	if err := cleanupIncrementals(ctx, agentID, latestSnapshotTimes); err != nil {
		utils.LogError(fmt.Sprintf("Incremental cleanup failed: %v", err))
	}

	return nil
}

func getLatestSnapshotTimes(ctx context.Context, agentID string) (map[string]time.Time, error) {
	snapshotFolder := filepath.Join(utils.AppConfiguration.DataDir, "snapshot")
	files, err := os.ReadDir(snapshotFolder)
	if err != nil {
		return nil, fmt.Errorf("error reading snapshot directory: %w", err)
	}

	latestTimes := make(map[string]time.Time)

	for _, file := range files {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		fileName := file.Name()
		if !strings.HasPrefix(fileName, agentID) || !strings.HasSuffix(fileName, ".img") {
			continue
		}

		baseName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
		parts := strings.Split(baseName, "_")
		if len(parts) < 2 {
			continue
		}
		diskID := parts[len(parts)-2]

		fileInfo, err := file.Info()
		if err != nil {
			utils.LogError(fmt.Sprintf("Error getting file info for %s: %v", fileName, err))
			continue
		}

		if currentTime, exists := latestTimes[diskID]; !exists || fileInfo.ModTime().After(currentTime) {
			latestTimes[diskID] = fileInfo.ModTime()
		}
	}

	return latestTimes, nil
}

func cleanupSnapshots(ctx context.Context, agentID string, latestSnapshotTimes map[string]time.Time, snapshotRetention int) error {
	snapshotFolder := filepath.Join(utils.AppConfiguration.DataDir, "snapshot")

	files, err := os.ReadDir(snapshotFolder)
	if err != nil {
		return fmt.Errorf("error reading snapshot directory: %w", err)
	}

	currentTime := time.Now()
	cleanupCount := 0
	var totalSpaceFreed int64

	diskSnapshots := make(map[string][]utils.SnapshotInfo)
	mostRecentPerDisk := make(map[string]utils.SnapshotInfo)

	// First pass: collect all valid snapshot pairs and organize by disk
	for _, file := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if file.IsDir() {
			continue
		}

		fileName := file.Name()
		if !strings.HasPrefix(fileName, agentID) {
			continue
		}

		baseName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
		parts := strings.Split(baseName, "_")
		if len(parts) < 2 {
			utils.LogError(fmt.Sprintf("Invalid snapshot filename format: %s", fileName))
			continue
		}
		diskID := parts[len(parts)-2]

		// Skip if already processed
		alreadyProcessed := false
		for _, info := range diskSnapshots[diskID] {
			if info.BaseName == baseName {
				alreadyProcessed = true
				break
			}
		}
		if alreadyProcessed {
			continue
		}

		imgPath := filepath.Join(snapshotFolder, baseName+".img")
		logPath := filepath.Join(snapshotFolder, baseName+".log")

		imgInfo, imgErr := os.Stat(imgPath)
		logInfo, logErr := os.Stat(logPath)

		if imgErr != nil || logErr != nil {
			utils.LogError(fmt.Sprintf("Error accessing snapshot files for base %s: img error: %v, log error: %v",
				baseName, imgErr, logErr))
			continue
		}

		// Use the older of the two timestamps
		fileTime := imgInfo.ModTime()
		if logInfo.ModTime().Before(fileTime) {
			fileTime = logInfo.ModTime()
		}

		snapshotInfo := utils.SnapshotInfo{
			Timestamp: fileTime,
			BaseName:  baseName,
			ImgSize:   imgInfo.Size(),
			LogSize:   logInfo.Size(),
		}

		diskSnapshots[diskID] = append(diskSnapshots[diskID], snapshotInfo)

		// Update most recent snapshot for this disk
		currentMostRecent, exists := mostRecentPerDisk[diskID]
		if !exists || fileTime.After(currentMostRecent.Timestamp) {
			mostRecentPerDisk[diskID] = snapshotInfo
		}
	}

	// Second pass: delete old snapshots while preserving the most recent one per disk
	for diskID, snapshots := range diskSnapshots {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		hasRecentSnapshot := false
		for _, snapshot := range snapshots {
			if currentTime.Sub(snapshot.Timestamp) <= (time.Duration(snapshotRetention) * time.Hour) {
				hasRecentSnapshot = true
				break
			}
		}

		for _, snapshot := range snapshots {
			fileAge := currentTime.Sub(snapshot.Timestamp)

			// Skip most recent snapshot if no recent snapshots exist
			if !hasRecentSnapshot && snapshot.BaseName == mostRecentPerDisk[diskID].BaseName {
				utils.LogDebug(fmt.Sprintf("Preserving most recent snapshot %s for disk %s (age: %v)",
					snapshot.BaseName, diskID, fileAge))
				continue
			}

			// Delete if older than retention period
			if fileAge > (time.Duration(snapshotRetention) * time.Hour) {
				imgPath := filepath.Join(snapshotFolder, snapshot.BaseName+".img")
				logPath := filepath.Join(snapshotFolder, snapshot.BaseName+".log")

				imgErr := os.Remove(imgPath)
				logErr := os.Remove(logPath)

				if imgErr != nil || logErr != nil {
					utils.LogError(fmt.Sprintf("Error deleting snapshot files for base %s: img error: %v, log error: %v",
						snapshot.BaseName, imgErr, logErr))
					continue
				}

				cleanupCount += 2
				totalSpaceFreed += snapshot.ImgSize + snapshot.LogSize
				utils.LogDebug(fmt.Sprintf("Deleted snapshot files for base %s (age: %v, freed: %d bytes)",
					snapshot.BaseName, fileAge, snapshot.ImgSize+snapshot.LogSize))
			}
		}
	}

	utils.LogDebug(fmt.Sprintf("Snapshot cleanup completed for agent %s. Deleted %d files, freed %d bytes",
		agentID, cleanupCount, totalSpaceFreed))

	return nil
}

func cleanupIncrementals(ctx context.Context, agentID string, latestSnapshotTimes map[string]time.Time) error {
	incrementalFolder := filepath.Join(utils.AppConfiguration.DataDir, "incremental")
	files, err := os.ReadDir(incrementalFolder)
	if err != nil {
		return fmt.Errorf("error reading incremental directory: %w", err)
	}

	var totalSpaceFreed int64
	cleanupCount := 0

	for _, file := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		fileName := file.Name()
		if !strings.HasPrefix(fileName, agentID) {
			continue
		}

		// Parse the disk ID from the filename
		parts := strings.Split(fileName, "_")
		if len(parts) < 2 {
			utils.LogError(fmt.Sprintf("Invalid incremental filename format: %s", fileName))
			continue
		}
		diskID := parts[1] // Get the disk identifier part

		// Get the latest snapshot time for this disk
		latestSnapshotTime, exists := latestSnapshotTimes[diskID]
		if !exists {
			utils.LogDebug(fmt.Sprintf("No snapshot found for disk %s, skipping incremental cleanup", diskID))
			continue
		}

		fileInfo, err := file.Info()
		if err != nil {
			utils.LogError(fmt.Sprintf("Error getting file info for %s: %v", fileName, err))
			continue
		}

		// If the incremental file is older than the latest snapshot, delete it
		if fileInfo.ModTime().Before(latestSnapshotTime) {
			filePath := filepath.Join(incrementalFolder, fileName)
			fileSize := fileInfo.Size()

			if err := os.Remove(filePath); err != nil {
				utils.LogError(fmt.Sprintf("Error deleting incremental file %s: %v", fileName, err))
				continue
			}

			cleanupCount++
			totalSpaceFreed += fileSize
			utils.LogDebug(fmt.Sprintf("Deleted incremental file %s (created: %v, latest snapshot: %v)",
				fileName, fileInfo.ModTime(), latestSnapshotTime))
		}
	}

	if cleanupCount > 0 {
		utils.LogDebug(fmt.Sprintf("Incremental cleanup completed for agent %s. Deleted %d files, freed %d bytes",
			agentID, cleanupCount, totalSpaceFreed))
	}

	return nil
}
