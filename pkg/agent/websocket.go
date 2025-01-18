package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/xmigrate/blxrep/utils"

	"github.com/gorilla/websocket"
)

type RestoreManager struct {
	isRunning bool
	mutex     sync.Mutex
	restoreCh chan utils.AgentBulkMessage
}

var restoreManager *RestoreManager

func init() {
	restoreManager = &RestoreManager{
		restoreCh: make(chan utils.AgentBulkMessage, 1), // Buffer size of 1
	}
}

func (rm *RestoreManager) TryStartRestore(msg utils.AgentBulkMessage, agentID string) {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	if !rm.isRunning {
		rm.isRunning = true
		rm.restoreCh = make(chan utils.AgentBulkMessage, 100) // Increased buffer size
		go rm.runRestore(agentID, msg.SrcPath)
	}

	// Always try to send the message, even if a restore is in progress
	select {
	case rm.restoreCh <- msg:
		utils.LogDebug(fmt.Sprintf("Queued restore message for agent %s, srcPath: %s", agentID, msg.SrcPath))
	default:
		utils.LogError(fmt.Sprintf("Restore channel full for agent %s", agentID))
	}
}

func (rm *RestoreManager) runRestore(agentID string, srcPath string) {
	defer func() {
		rm.mutex.Lock()
		rm.isRunning = false
		close(rm.restoreCh) // Close the channel when done
		rm.mutex.Unlock()
	}()

	utils.LogDebug(fmt.Sprintf("Starting restore process for agent %s, srcPath: %s", agentID, srcPath))
	RestorePartition(&rm.restoreCh, agentID, &restoreProgress, srcPath)
	utils.LogDebug(fmt.Sprintf("Completed restore process for agent %s, srcPath: %s", agentID, srcPath))
}

func ConnectToDispatcher(agentID string, dispatcherAddr string, device []string) {
	snapshotURL := url.URL{Scheme: "ws", Host: dispatcherAddr, Path: "/ws/snapshot"}
	liveURL := url.URL{Scheme: "ws", Host: dispatcherAddr, Path: "/ws/live"}
	restoreURL := url.URL{Scheme: "ws", Host: dispatcherAddr, Path: "/ws/restore"}
	configURL := url.URL{Scheme: "ws", Host: dispatcherAddr, Path: "/ws/config"}

	var wg sync.WaitGroup
	wg.Add(4)

	go func() {
		defer wg.Done()
		for {
			utils.LogDebug(fmt.Sprintf("Agent %s connecting to config endpoint: %s", agentID, configURL.String()))
			if err := connectAndHandle(agentID, configURL.String()); err != nil {
				utils.LogError(fmt.Sprintf("Config connection error: %v", err))
			}
			time.Sleep(15 * time.Second)
		}
	}()

	for {
		if utils.AgentConfiguration.SnapshotTime != "" {
			utils.LogDebug("Starting snapshot scheduled jobs")
			err := StartScheduledJobs(agentID, snapshotURL.String())
			if err != nil {
				utils.LogError(fmt.Sprintf("Failed to start scheduled jobs: %v", err))
			}
			break
		}
		time.Sleep(15 * time.Second)
	}

	go func() {
		defer wg.Done()
		for {
			if len(utils.AgentConfiguration.Disks) == 0 {
				utils.LogDebug("Agent configuration not yet set, waiting...")
				// Trying to update the config
				if err := connectAndHandle(agentID, configURL.String()); err != nil {
					utils.LogError(fmt.Sprintf("Config connection error: %v", err))
				}
				time.Sleep(15 * time.Second)
				continue
			}
			utils.LogDebug(fmt.Sprintf("Agent %s connecting to snapshot endpoint: %s", agentID, snapshotURL.String()))
			if err := connectAndHandle(agentID, snapshotURL.String()); err != nil {
				utils.LogError(fmt.Sprintf("Snapshot connection error: %v", err))
			}
			time.Sleep(5 * time.Second)
		}
	}()

	go func() {
		defer wg.Done()
		for {
			if len(utils.AgentConfiguration.Disks) == 0 {
				utils.LogDebug("Agent configuration not yet set, waiting...")
				time.Sleep(15 * time.Second)
				continue
			}
			utils.LogDebug(fmt.Sprintf("Agent %s connecting to live endpoint: %s", agentID, liveURL.String()))
			if err := connectAndHandle(agentID, liveURL.String()); err != nil {
				utils.LogError(fmt.Sprintf("Live connection error: %v", err))
			}
			time.Sleep(5 * time.Second)
		}
	}()

	go func() {
		defer wg.Done()
		for {
			utils.LogDebug(fmt.Sprintf("Agent %s connecting to restore endpoint: %s", agentID, restoreURL.String()))
			if err := connectAndHandle(agentID, restoreURL.String()); err != nil {
				utils.LogError(fmt.Sprintf("Restore connection error: %v", err))
			}
			time.Sleep(5 * time.Second)
		}
	}()

	wg.Wait()
}

var cloneMutex sync.Mutex
var liveMutex sync.Mutex
var syncMutex sync.Mutex
var restoreMutex sync.Mutex

var isCloning bool
var isLive bool
var isSyncing bool
var cancel context.CancelFunc
var cancelLive context.CancelFunc
var cancelSync context.CancelFunc
var currentRestoreState *utils.RestoreState
var restoreProgress int

func connectAndHandle(agentID string, urlString string) error {
	var restoreState *utils.RestoreState
	conn, _, err := websocket.DefaultDialer.Dial(urlString, nil)
	if err != nil {
		return fmt.Errorf("WebSocket connection error: %v", err)
	}
	defer conn.Close()
	// Set up ping handler
	conn.SetPingHandler(func(appData string) error {
		err := conn.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(10*time.Second))
		if err != nil {
			utils.LogError(fmt.Sprintf("Failed to send pong: %v", err))
			return err
		}
		return nil
	})
	conn.EnableWriteCompression(true)

	// Send agent ID as authentication
	err = conn.WriteMessage(websocket.TextMessage, []byte(agentID))
	if err != nil {
		return fmt.Errorf("WebSocket write error: %v", err)
	}

	// Wait for authentication confirmation
	_, message, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("WebSocket read error: %v", err)
	}

	if string(message) != "Authenticated" {
		return fmt.Errorf("Authentication failed: %s", string(message))
	}

	utils.LogDebug(fmt.Sprintf("Agent %s authenticated successfully on %s", agentID, urlString))
	if strings.Contains(urlString, "snapshot") {
		utils.LogDebug("Starting snapshot data stream handling")
		cloneMutex.Lock()
		if !isCloning {
			isCloning = true
			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())
			cloneMutex.Unlock()
			go func() {
				defer func() {
					cloneMutex.Lock()
					isCloning = false
					cloneMutex.Unlock()
				}()
				for _, dev := range utils.AgentConfiguration.Disks {
					utils.LogDebug(fmt.Sprintf("Started cloning device: %s", dev))
					Clone(ctx, 512, dev, 12000, conn, &cloneMutex, &isCloning)
				}
			}()
		} else {
			cloneMutex.Unlock()
			utils.LogDebug("Cloning already in progress, skipping for this connection")
		}
	} else if strings.Contains(urlString, "live") {
		utils.LogDebug("Starting live data stream handling")
		liveMutex.Lock()
		if !isLive {
			isLive = true
			utils.LogDebug(fmt.Sprintf("Starting live data stream for %s", utils.AgentConfiguration.Disks))
			var ctx context.Context
			ctx, cancelLive = context.WithCancel(context.Background())
			for _, dev := range utils.AgentConfiguration.Disks {
				go GetBlocks(ctx, 512, dev, conn, agentID)
				utils.LogDebug(fmt.Sprintf("Started live data stream for device: %s", dev))
			}
			utils.LogDebug("Change block tracking started..")
		}
		liveMutex.Unlock()
	} else if strings.Contains(urlString, "config") {
		vmInfo, err := Footprint()
		if err != nil {
			return err
		}
		utils.LogDebug(fmt.Sprintf("Footprint %v", vmInfo))
		var footprint utils.AgentBulkMessage
		footprint.AgentID, _ = os.Hostname()
		footprint.DataType = "footprint"
		footprint.Footprint = *vmInfo
		jsonData, err := json.Marshal(footprint)
		utils.LogDebug(fmt.Sprintf("Footprint sent to %s", urlString))

		if err != nil {
			return err
		}

		err = conn.WriteMessage(websocket.TextMessage, jsonData)
		if err != nil {
			return err
		}
	}
	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				utils.LogError(fmt.Sprintf("WebSocket read error: %v", err))
			}
			return fmt.Errorf("WebSocket read error: %v", err)
		}
		// Handle ping messages
		if messageType == websocket.PingMessage {
			err = conn.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(10*time.Second))
			if err != nil {
				utils.LogError(fmt.Sprintf("Failed to send pong: %v", err))
				return err
			}
			continue
		}
		var msg utils.Message
		err = json.Unmarshal(message, &msg)
		if err != nil {
			utils.LogError(fmt.Sprintf("Error unmarshalling JSON: %v", err))
			continue
		}
		utils.LogDebug(fmt.Sprintf("Recieved Message %v ", msg.Action))
		switch msg.Action {
		default:
			utils.LogDebug(fmt.Sprintf("Received unknown command on %s: %s", urlString, string(msg.Action)))
		case "config":
			utils.LogDebug("Received config message")
			utils.AgentConfiguration = msg.ConfigMessage
			utils.LogDebug(fmt.Sprintf("Agent configuration received: %v", msg.ConfigMessage))
			utils.LogDebug(fmt.Sprintf("Agent configuration set to: %v", utils.AgentConfiguration))
		case "resume":
			cloneMutex.Lock()
			if !isCloning {
				isCloning = true
				resumeData := msg.ResumeMessage
				utils.LogDebug("Entered Resume switch case")
				var ctx context.Context
				ctx, cancel = context.WithCancel(context.Background())
				go Resume(ctx, resumeData.BlockSize, resumeData.SrcPath, resumeData.ChannelSize, resumeData.ReadFrom, conn, &cloneMutex, &isCloning)
			}
			cloneMutex.Unlock()
		case "pause":
			utils.LogDebug("Entered pause switch case")
			if cancel != nil {
				utils.LogDebug("Cancelling cloning")
				cancel()
			}
			cloneMutex.Lock()
			isCloning = false
			cloneMutex.Unlock()
		case "stop":
			utils.LogDebug("Received stop command")
			cancel()
		case utils.CONST_AGENT_ACTION_SYNC:
			utils.LogDebug("Entered Sync switch case")
			processSyncAction(msg, conn)
			utils.LogDebug(fmt.Sprintf("Received command on %s: %s", urlString, string(msg.Action)))
		case "StopSync":
			utils.LogDebug("Entered StopSync switch case")
			processStopSyncAction()
		case utils.CONST_AGENT_ACTION_PARTITION_RESTORE:
			utils.LogDebug("Entered Partition Restore switch case")
			var bulkMsg utils.AgentBulkMessage
			err = json.Unmarshal(message, &bulkMsg)
			if err != nil {
				utils.LogError(fmt.Sprintf("Error unmarshalling JSON: %v", err))
				continue
			}
			restoreManager.TryStartRestore(bulkMsg, agentID)

		case utils.CONST_AGENT_ACTION_RESTORE:
			utils.LogDebug("Entered Restore switch case")
			restoreMsg := msg.RestoreMessage
			switch restoreMsg.Type {
			case "start":
				if currentRestoreState != nil {
					return fmt.Errorf("restore already in progress")
				}
				currentRestoreState = &utils.RestoreState{
					Buffer:      new(bytes.Buffer),
					TotalChunks: restoreMsg.TotalChunks,
					FilePath:    restoreMsg.FilePath,
				}
				utils.LogDebug(fmt.Sprintf("Started new restore process for %s", restoreMsg.FilePath))
			case "chunk":
				if currentRestoreState == nil {
					return fmt.Errorf("received chunk without start message")
				}
				decodedData, err := base64.StdEncoding.DecodeString(restoreMsg.Data)
				if err != nil {
					return fmt.Errorf("error decoding chunk data: %v", err)
				}
				err = processChunk(currentRestoreState, decodedData, restoreMsg.ChunkIndex)
				if err != nil {
					return fmt.Errorf("error processing chunk: %v", err)
				}
			case "complete":
				if currentRestoreState == nil {
					return fmt.Errorf("received complete message without start")
				}
				if currentRestoreState.ChunksReceived != currentRestoreState.TotalChunks {
					return fmt.Errorf("restore incomplete: expected %d chunks, received %d", currentRestoreState.TotalChunks, currentRestoreState.ChunksReceived)
				}
				err := processCompleteData(currentRestoreState)
				if err != nil {
					return fmt.Errorf("error processing complete data: %v", err)
				}
				currentRestoreState = nil // Reset the state after completion
				utils.LogDebug("Restore process completed successfully")
			case "cancel":
				utils.LogDebug("File restoration cancelled")
				if restoreState != nil {
					cleanupRestore(restoreState)
				}
				restoreState = nil

			default:
				return fmt.Errorf("unknown restore message type: %s", restoreMsg.Type)
			}
		}

	}
}
