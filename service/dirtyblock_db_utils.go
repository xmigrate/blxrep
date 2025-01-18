package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xmigrate/blxrep/storage"
	"github.com/xmigrate/blxrep/storage/boltdb"
	"github.com/xmigrate/blxrep/utils"
)

// getDirtyBlockDBInstance returns a new instance of the BoltDB service
func getDirtyBlockDBInstance() storage.Service {
	dbPath := utils.AppConfiguration.DataDir + "/xmdirtyblocks.db"
	utils.LogDebug(fmt.Sprintf("Getting DirtyBlock DB instance at: %s", dbPath))
	return boltdb.New(dbPath)
}

// DirtyBlock represents a block that needs to be retried
type DirtyBlock struct {
	BlockNumber int64     `json:"block_number"`
	TimeCreated time.Time `json:"time_created"`
	LastRetried time.Time `json:"last_retried"`
	RetryCount  int       `json:"retry_count"`
	AgentID     string    `json:"agent_id"`
	DiskPath    string    `json:"disk_path"`
}

// AddDirtyBlock adds a new dirty block to the database
func AddDirtyBlock(agentID, diskPath string, blockNum int64) error {
	db := getDirtyBlockDBInstance()
	if err := db.Open(); err != nil {
		utils.LogError(fmt.Sprintf("DirtyBlock DB: Failed to open DB for adding dirty block: %v", err))
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			utils.LogError("DirtyBlock DB: unable to close DB: " + err.Error())
		}
	}()

	block := DirtyBlock{
		BlockNumber: blockNum,
		TimeCreated: time.Now().UTC(),
		LastRetried: time.Now().UTC(),
		RetryCount:  1,
		AgentID:     agentID,
		DiskPath:    diskPath,
	}

	key := fmt.Sprintf("%s_%s_%d", agentID, diskPath, blockNum)
	blockData, err := json.Marshal(block)
	if err != nil {
		utils.LogError(fmt.Sprintf("DirtyBlock DB: Failed to marshal dirty block data: %v", err))
		return err
	}

	utils.LogDebug(fmt.Sprintf("DirtyBlock DB: Adding dirty block - Key: %s, Block: %+v", key, block))
	err = db.Insert(key, blockData)
	if err != nil {
		utils.LogError(fmt.Sprintf("DirtyBlock DB: Failed to insert dirty block into DB: %v", err))
		return err
	}

	utils.LogDebug(fmt.Sprintf("DirtyBlock DB: Successfully added dirty block - Key: %s", key))
	return nil
}

// GetDirtyBlocks retrieves all dirty blocks for a specific agent and disk path
func GetDirtyBlocks(agentID, diskPath string) ([]DirtyBlock, error) {
	db := getDirtyBlockDBInstance()
	if err := db.Open(); err != nil {
		utils.LogError(fmt.Sprintf("DirtyBlock DB: Failed to open DB for getting dirty blocks: %v", err))
		return nil, err
	}
	defer func() {
		if err := db.Close(); err != nil {
			utils.LogError("DirtyBlock DB: unable to close DB: " + err.Error())
		}
	}()

	utils.LogDebug(fmt.Sprintf("DirtyBlock DB: Getting dirty blocks for agent: %s, disk: %s", agentID, diskPath))

	allBlocks, err := db.SelectAll(-1)
	if err != nil {
		utils.LogError(fmt.Sprintf("DirtyBlock DB: Failed to select all blocks from DB: %v", err))
		return nil, err
	}

	utils.LogDebug(fmt.Sprintf("DirtyBlock DB: Found %d total blocks in DB", len(allBlocks)))

	var blocks []DirtyBlock
	prefix := fmt.Sprintf("%s_%s_", agentID, diskPath)

	for key, blockData := range allBlocks {
		// Unmarshal the key since BoltDB stores it as JSON
		var keyStr string
		if err := json.Unmarshal([]byte(key), &keyStr); err != nil {
			utils.LogError(fmt.Sprintf("DirtyBlock DB: Failed to unmarshal key: %v", err))
			continue
		}

		// Check if the key starts with our prefix
		if strings.HasPrefix(keyStr, prefix) {
			var block DirtyBlock
			if err := json.Unmarshal(blockData, &block); err != nil {
				utils.LogError(fmt.Sprintf("DirtyBlock DB: Failed to unmarshal block data: %v", err))
				continue
			}
			blocks = append(blocks, block)
		}
	}

	utils.LogDebug(fmt.Sprintf("DirtyBlock DB: Found %d dirty blocks for agent: %s, disk: %s", len(blocks), agentID, diskPath))
	return blocks, nil
}

// RemoveBlock removes a specific dirty block from the database
func RemoveBlock(agentID, diskPath string, blockNum int64) error {
	db := getDirtyBlockDBInstance()
	if err := db.Open(); err != nil {
		utils.LogError(fmt.Sprintf("DirtyBlock DB: Failed to open DB for removing dirty block: %v", err))
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			utils.LogError("DirtyBlock DB: unable to close DB: " + err.Error())
		}
	}()

	key := fmt.Sprintf("%s_%s_%d", agentID, diskPath, blockNum)
	err := db.Delete(key)
	if err != nil {
		if err.Error() == "key not found" {
			utils.LogDebug(fmt.Sprintf("DirtyBlock DB: Block already removed - Key: %s", key))
			return nil
		}
		utils.LogError(fmt.Sprintf("DirtyBlock DB: Failed to remove block: %v", err))
		return err
	}

	utils.LogDebug(fmt.Sprintf("DirtyBlock DB: Successfully removed block - Key: %s", key))
	return nil
}

// UpdateBlockRetry updates the retry count and last retried time for a specific block
func UpdateBlockRetry(agentID, diskPath string, blockNum int64) error {
	db := getDirtyBlockDBInstance()
	if err := db.Open(); err != nil {
		utils.LogError(fmt.Sprintf("DirtyBlock DB: Failed to open DB for updating retry count: %v", err))
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			utils.LogError("DirtyBlock DB: unable to close DB: " + err.Error())
		}
	}()

	key := fmt.Sprintf("%s_%s_%d", agentID, diskPath, blockNum)
	utils.LogDebug(fmt.Sprintf("DirtyBlock DB: Updating retry count for block - Key: %s", key))

	blockData, err := db.Get(key)
	if err != nil {
		if err.Error() == "key does not exists" {
			utils.LogDebug(fmt.Sprintf("DirtyBlock DB: Block not found for retry update - Key: %s", key))
			return nil // Not an error, block might have been removed
		}
		utils.LogError(fmt.Sprintf("DirtyBlock DB: Failed to get block for retry update: %v", err))
		return err
	}

	var block DirtyBlock
	if err := json.Unmarshal(blockData, &block); err != nil {
		utils.LogError(fmt.Sprintf("DirtyBlock DB: Failed to unmarshal block data for retry update: %v", err))
		return err
	}

	block.LastRetried = time.Now().UTC()
	block.RetryCount++

	utils.LogDebug(fmt.Sprintf("DirtyBlock DB: Updating block retry count - Key: %s, New Count: %d", key, block.RetryCount))

	updatedBlockData, err := json.Marshal(block)
	if err != nil {
		utils.LogError(fmt.Sprintf("DirtyBlock DB: Failed to marshal updated block data: %v", err))
		return err
	}

	if err := db.Insert(key, updatedBlockData); err != nil {
		utils.LogError(fmt.Sprintf("DirtyBlock DB: Failed to save updated retry count: %v", err))
		return err
	}

	utils.LogDebug(fmt.Sprintf("DirtyBlock DB: Successfully updated retry count - Key: %s", key))
	return nil
}

// IsBlockDirty checks if a specific block is marked as dirty
func IsBlockDirty(agentID, diskPath string, blockNum int64) (bool, error) {
	db := getDirtyBlockDBInstance()
	if err := db.Open(); err != nil {
		utils.LogError(fmt.Sprintf("DirtyBlock DB: Failed to open DB for checking dirty block: %v", err))
		return false, err
	}
	defer func() {
		if err := db.Close(); err != nil {
			utils.LogError("DirtyBlock DB: unable to close DB: " + err.Error())
		}
	}()

	key := fmt.Sprintf("%s_%s_%d", agentID, diskPath, blockNum)
	utils.LogDebug(fmt.Sprintf("DirtyBlock DB: Checking if block is dirty - Key: %s", key))

	_, err := db.Get(key)
	if err != nil {
		if strings.Contains(err.Error(), "does not exists") {
			// This is an expected case for most blocks
			return false, nil
		}
		utils.LogError(fmt.Sprintf("DirtyBlock DB: Error checking dirty block: %v", err))
		return false, err
	}

	utils.LogDebug(fmt.Sprintf("DirtyBlock DB: Block found in dirty list - Key: %s", key))
	return true, nil
}

// GetAllDirtyBlocks retrieves all dirty blocks from the database
func GetAllDirtyBlocks() ([]DirtyBlock, error) {
	db := getDirtyBlockDBInstance()
	if err := db.Open(); err != nil {
		utils.LogError(fmt.Sprintf("DirtyBlock DB: Failed to open DB for getting all dirty blocks: %v", err))
		return nil, err
	}
	defer func() {
		if err := db.Close(); err != nil {
			utils.LogError("DirtyBlock DB: unable to close DB: " + err.Error())
		}
	}()

	allBlocks, err := db.SelectAll(-1)
	if err != nil {
		return nil, err
	}

	var blocks []DirtyBlock
	for _, blockData := range allBlocks {
		var block DirtyBlock
		if err := json.Unmarshal(blockData, &block); err != nil {
			continue
		}
		blocks = append(blocks, block)
	}

	return blocks, nil
}
