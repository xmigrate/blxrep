package dispatcher

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xmigrate/blxrep/service"
	"github.com/xmigrate/blxrep/utils"
)

const (
	ACTION_STATUS_STARTED         = "Started"
	ACTION_STATUS_COMPLETED       = "Completed"
	ACTION_STATUS_FAILED          = "Failed"
	ACTION_TYPE_RESTORE           = "Restore"
	ACTION_TYPE_PARTITION_RESTORE = "PartitionRestore"
	MAX_ACTIONS_TO_PROCESS        = 1000
)

var restorePartitionMutex sync.Mutex
var isPartitionRestore bool

func StaledActions() error {
	actions, err := service.GetAllActionsWithStatus(MAX_ACTIONS_TO_PROCESS, utils.CONST_ACTION_STATUS_IN_PROGRESS)
	if err != nil {
		return fmt.Errorf("error fetching in progress actions: %v", err)
	}
	for _, action := range actions {
		if action.TimeUpdated == 0 {
			action.TimeUpdated = time.Now().Unix()
			action.UpdateBackend = true
			err = service.InsertOrUpdateAction(action)
			if err != nil {
				return fmt.Errorf("error updating action time updated field: %v", err)
			}
		}
		if action.TimeUpdated < time.Now().Unix()-600 {
			action.ActionStatus = string(utils.CONST_ACTION_STATUS_FAILED)
			action.TimeFinished = time.Now().Unix()
			action.UpdateBackend = true
			err = service.InsertOrUpdateAction(action)
			if err != nil {
				return fmt.Errorf("error updating action status to failed: %v", err)
			}
		}
	}
	return nil
}

func StaledActionsJob() error {
	utils.LogDebug(fmt.Sprintf("Staled actions job started to run at %s", time.Now().Format(time.RFC3339)))

	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		checkInterval := 5 * time.Minute
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()

		// Run initial cleanup for all agents
		if err := StaledActions(); err != nil {
			utils.LogError(fmt.Sprintf("Initial cleanup for all actions failed: %v", err))
		}

		// Then run periodic cleanup for all agents
		for {
			select {
			case <-ctx.Done():
				utils.LogDebug("Staled actions jobs stopped")
				return
			case <-ticker.C:
				if err := StaledActions(); err != nil {
					utils.LogError(fmt.Sprintf("Periodic cleanup for all actions failed: %v", err))
				}
			}
		}
	}()

	return nil
}

func CheckActions(agentID string) error {
	// Get pending actions
	actions, err := service.GetAllActionsWithStatus(MAX_ACTIONS_TO_PROCESS, utils.CONST_ACTION_STATUS_WAITING)
	if err != nil {
		return fmt.Errorf("error fetching pending actions: %v", err)
	}
	utils.LogDebug(fmt.Sprintf("Found %d pending actions for agent %s", len(actions), agentID))
	for _, action := range actions {
		// Check if the action is for this agent and is a restore action
		if action.AgentId == agentID && action.ActionType == ACTION_TYPE_RESTORE {
			// Update the action status to started
			action.ActionStatus = ACTION_STATUS_STARTED
			action.TimeStarted = time.Now().Unix()
			action.ActionProgress = 1

			err = service.InsertOrUpdateAction(action)
			if err != nil {
				return fmt.Errorf("error updating action status to started: %v", err)
			}

			// Log the action start
			utils.LogDebug(fmt.Sprintf("Starting restore action %s for agent %s", action.Id, agentID))

			// Perform the restore operation
			if action.Action == ACTION_TYPE_RESTORE {
				err := RestoreFiles(agentID, action.SourceFilePath, action.TargetFilePath, action)
				if err != nil {
					utils.LogError(fmt.Sprintf("Error during restore for action %s: %v", action.Id, err))

					// Update action status to failed
					action.ActionStatus = ACTION_STATUS_FAILED
					action.TimeFinished = time.Now().Unix()
					err = service.InsertOrUpdateAction(action)
					if err != nil {
						utils.LogError(fmt.Sprintf("Error updating action status to failed: %v", err))
					}
					continue
				}

				// Update action status to completed
				action.ActionStatus = ACTION_STATUS_COMPLETED
				action.TimeFinished = time.Now().Unix()
				action.ActionProgress = 100
				err = service.InsertOrUpdateAction(action)
				if err != nil {
					utils.LogError(fmt.Sprintf("Error updating action status to completed: %v", err))
				}

				// Log the action completion
				utils.LogDebug(fmt.Sprintf("Completed restore files action %s for agent %s", action.Id, agentID))
			} else if action.Action == ACTION_TYPE_PARTITION_RESTORE {
				utils.LogDebug(fmt.Sprintf("Starting restore partition action %s for agent %s", action.Id, agentID))
				restorePartitionMutex.Lock()
				var ctx context.Context
				ctx, _ = context.WithCancel(context.Background())
				err := RestorePartition(agentID, action.SourceFilePath, action.TargetFilePath, 512, 12000, ctx, &restorePartitionMutex, &isPartitionRestore, action)
				if err != nil {
					utils.LogError(fmt.Sprintf("Error during restore for action %s: %v", action.Id, err))

					// Update action status to failed
					action.ActionStatus = ACTION_STATUS_FAILED
					action.TimeFinished = time.Now().Unix()
					err = service.InsertOrUpdateAction(action)
				}
				restorePartitionMutex.Unlock()
				action.ActionStatus = ACTION_STATUS_COMPLETED
				action.TimeFinished = time.Now().Unix()
				err = service.InsertOrUpdateAction(action)
				if err != nil {
					utils.LogError(fmt.Sprintf("Error updating action status to completed: %v", err))
				}

				// Log the action completion
				utils.LogDebug(fmt.Sprintf("Completed restore partition action %s for agent %s", action.Id, agentID))
			}
		} else if action.AgentId == agentID && action.Action == string(utils.CONST_AGENT_ACTION_PAUSE) {
			err := SendPauseAction(action.SnapshotId, action.Id, agentID)
			if err != nil {
				utils.LogError(fmt.Sprintf("Error pausing action %s for agent %s: %v", action.Id, agentID, err))
			}
		} else if action.AgentId == agentID && action.Action == string(utils.CONST_AGENT_ACTION_RESUME) {
			err := SendResumeAction(action.SnapshotId, action.Id, agentID)
			if err != nil {
				utils.LogError(fmt.Sprintf("Error resuming action %s for agent %s: %v", action.Id, agentID, err))
			}
		}
	}
	return nil
}

func SendResumeAction(cloneActionID string, resumeActionId string, agentID string) error {
	resumeAction, err := service.GetAction(resumeActionId)
	if err != nil {
		return fmt.Errorf("error fetching Resume action by ID: %v", err)
	}

	cloneAction, err := service.GetAction(cloneActionID)
	if err != nil {
		return fmt.Errorf("error fetching Clone action by ID: %v", err)
	}

	filename := utils.AppConfiguration.DataDir + "/snapshot/" + agentID + ".log"
	readFrom, err := utils.ReadLastLine(filename)
	if err != nil {
		return fmt.Errorf("error reading last line from file: %v", err)
	}
	blockNum, err := strconv.ParseInt(readFrom, 10, 64)
	if err != nil {
		return fmt.Errorf("error converting readFrom to int: %v", err)
	}
	agentData := agents[agentID]
	conn := agentData.SnapConn
	msg := utils.Message{
		Action: "resume",
		ResumeMessage: utils.ResumeData{
			BlockSize:   int(utils.CONST_BLOCK_SIZE),
			ChannelSize: int(utils.CONST_CHANNEL_SIZE),
			ReadFrom:    blockNum,
		},
	}

	err = conn.WriteJSON(msg)
	if err != nil {
		return fmt.Errorf("error writing resume message to agent: %v", err)
	}
	c := agentData.SnapshotChannel
	go ResumeBackupToFile(&c, int64(utils.CONST_BLOCK_SIZE), blockNum, agentID, cloneAction.Id, &agentData.Progress)
	resumeAction.ActionStatus = string(utils.CONST_ACTION_STATUS_TYPE(utils.CONST_ACTION_STATUS_COMPLETED))
	resumeAction.TimeFinished = time.Now().Unix()
	resumeAction.ActionProgress = 100
	err = service.InsertOrUpdateAction(resumeAction)
	if err != nil {
		return fmt.Errorf("error updating action status to completed: %v", err)
	}
	cloneAction.ActionStatus = string(utils.CONST_ACTION_STATUS_TYPE(utils.CONST_ACTION_STATUS_IN_PROGRESS))
	err = service.InsertOrUpdateAction(cloneAction)
	if err != nil {
		return fmt.Errorf("error updating action status to completed: %v", err)
	}
	return nil
}

func SendPauseAction(cloneActionID string, pauseActionId string, agentID string) error {
	pauseAction, err := service.GetAction(pauseActionId)
	if err != nil {
		return fmt.Errorf("error fetching Pause action by ID: %v", err)
	}

	cloneAction, err := service.GetAction(cloneActionID)
	if err != nil {
		return fmt.Errorf("error fetching Clone action by ID: %v", err)
	}
	conn := agents[agentID].RestoreConn
	msg := utils.Message{
		Action: "pause",
	}

	err = conn.WriteJSON(msg)
	if err != nil {
		return fmt.Errorf("error writing pause message to agent: %v", err)
	}
	cloneAction.ActionStatus = string(utils.CONST_ACTION_STATUS_TYPE(utils.CONST_ACTION_STATUS_PAUSED))
	err = service.InsertOrUpdateAction(cloneAction)
	if err != nil {
		return fmt.Errorf("error updating action status to paused: %v", err)
	}
	pauseAction.ActionStatus = string(utils.CONST_ACTION_STATUS_TYPE(utils.CONST_ACTION_STATUS_COMPLETED))
	pauseAction.TimeFinished = time.Now().Unix()
	pauseAction.ActionProgress = 100
	err = service.InsertOrUpdateAction(pauseAction)
	if err != nil {
		return fmt.Errorf("error updating action status to completed: %v", err)
	}
	return nil
}

func PauseAction(actionID string, agentID string) error {
	utils.LogDebug(fmt.Sprintf("PauseAction called with actionID: '%s', agentID: '%s'", actionID, agentID))
	// Check if actionID or agentID is empty
	if actionID == "" || agentID == "" {
		return fmt.Errorf("actionID or agentID is empty")
	}
	startTime := time.Now().Unix()
	pauseActionID := strings.Join([]string{agentID, fmt.Sprintf("%d", startTime)}, "_")
	action, err := service.GetAction(pauseActionID)
	if err != nil {
		errMsg := fmt.Sprintf("error fetching action by ID: %v", err)
		utils.LogDebug(errMsg)
		action.Id = pauseActionID
		action.ActionStatus = string(utils.CONST_ACTION_STATUS_WAITING)
		action.TimeStarted = startTime
		action.SnapshotId = actionID
		action.AgentId = agentID
		action.ActionType = string(utils.CONST_AGENT_ACTION_CLONE)
		action.Action = string(utils.CONST_AGENT_ACTION_PAUSE)
		err = service.InsertOrUpdateAction(action)
		if err != nil {
			utils.LogError(fmt.Sprintf("Error updating action status to paused: %v", err))
		}
		utils.LogDebug(fmt.Sprintf("Action %s paused for agent %s created", actionID, agentID))
	}
	return nil
}

func ResumeAction(actionID string, agentID string) error {
	utils.LogDebug(fmt.Sprintf("ResumeAction called with actionID: '%s', agentID: '%s'", actionID, agentID))
	// Check if actionID or agentID is empty
	if actionID == "" || agentID == "" {
		return fmt.Errorf("actionID or agentID is empty")
	}
	startTime := time.Now().Unix()
	resumeActionID := strings.Join([]string{agentID, fmt.Sprintf("%d", startTime)}, "_")
	action, err := service.GetAction(resumeActionID)
	if err != nil {
		errMsg := fmt.Sprintf("error fetching action by ID: %v", err)
		utils.LogDebug(errMsg)
		action.Id = resumeActionID
		action.ActionStatus = string(utils.CONST_ACTION_STATUS_WAITING)
		action.TimeStarted = startTime
		action.SnapshotId = actionID
		action.AgentId = agentID
		action.ActionType = string(utils.CONST_AGENT_ACTION_CLONE)
		action.Action = string(utils.CONST_AGENT_ACTION_RESUME)
		err = service.InsertOrUpdateAction(action)
		if err != nil {
			utils.LogError(fmt.Sprintf("Error updating action status to resumed: %v", err))
		}
		utils.LogDebug(fmt.Sprintf("Action %s resumed for agent %s created", actionID, agentID))
	}
	return nil
}
