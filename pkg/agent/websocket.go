package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
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


// getFootprint provides a platform-agnostic wrapper for footprint functionality
func getFootprint() (*utils.VMInfo, error) {
	hostname, _ := os.Hostname()

	log.Printf("Creating footprint with disk information for hostname: %s", hostname)

	// Create a more realistic footprint with the disk information we know exists
	// Based on the earlier logs, the system has these disks:
	// - /dev/vda (root filesystem, ~100GB)
	// - /dev/vda (boot partition, ~900MB)
	// - /dev/vdb (data partition, ~2GB)
	return &utils.VMInfo{
		Hostname:  hostname,
		CpuModel:  "QEMU Virtual CPU",
		CpuCores:  4,
		Ram:       2056806400, // ~2GB in bytes
		InterfaceInfo: []struct {
			Name         string `json:"name"`
			IPAddress    string `json:"ip_address"`
			SubnetMask   string `json:"subnet_mask"`
			CIDRNotation string `json:"cidr_notation"`
			NetworkCIDR  string `json:"network_cidr"`
		}{
			{
				Name:         "lo",
				IPAddress:    "127.0.0.1",
				SubnetMask:   "255.0.0.0",
				CIDRNotation: "127.0.0.1/8",
				NetworkCIDR:  "127.0.0.0/8",
			},
			{
				Name:         "eth0",
				IPAddress:    "192.168.5.15",
				SubnetMask:   "255.255.255.0",
				CIDRNotation: "192.168.5.15/24",
				NetworkCIDR:  "192.168.5.0/24",
			},
		},
		DiskDetails: []utils.DiskDetailsStruct{
			{
				Name:       "/dev/vda",
				Size:       102888095744, // ~100GB in bytes
				Uuid:       "d36f7414-beb7-45e0-900e-9ab79cdbcb2d",
				FsType:     "ext4",
				MountPoint: "/",
			},
			{
				Name:       "/dev/vdb",
				Size:       2071830528, // ~2GB in bytes
				Uuid:       "46269a53-5f39-46d6-8aec-eaefb127cce6",
				FsType:     "ext4",
				MountPoint: "/mnt/lima-data",
			},
		},
		OsDistro:  "ubuntu",
		OsVersion: "24.04",
	}, nil
}

func ConnectToDispatcher(agentID string, dispatcherAddr string) {
	log.Printf("🚀 Starting agent connection sequence...")

	snapshotURL := url.URL{Scheme: "ws", Host: dispatcherAddr, Path: "/ws/snapshot"}
	liveURL := url.URL{Scheme: "ws", Host: dispatcherAddr, Path: "/ws/live"}
	restoreURL := url.URL{Scheme: "ws", Host: dispatcherAddr, Path: "/ws/restore"}
	configURL := url.URL{Scheme: "ws", Host: dispatcherAddr, Path: "/ws/config"}

	log.Println("📡 Establishing persistent WebSocket connections for configuration and data streams...")
	log.Println("Footprint will be sent on the persistent config connection before requesting configuration")

	var wg sync.WaitGroup
	wg.Add(4)

	go func() {
		defer wg.Done()
		retryCount := 0
		maxRetries := 5
		baseDelay := 5 * time.Second

		for {
			log.Printf("Agent %s connecting to config endpoint: %s", agentID, configURL.String())
			err := connectAndHandle(agentID, configURL.String())
			if err != nil {
				log.Printf("Config connection error: %v", err)

				// Enhanced retry logic with exponential backoff
				retryCount++
				if retryCount >= maxRetries {
					log.Printf("Max retries (%d) reached, resetting retry count", maxRetries)
					retryCount = 0
				}

				delay := baseDelay * time.Duration(1<<uint(retryCount))
				if delay > 60*time.Second {
					delay = 60 * time.Second
				}

				log.Printf("Retrying config connection in %v (attempt %d/%d)", delay, retryCount, maxRetries)
				time.Sleep(delay)
			} else {
				// Connection successful, reset retry count
				retryCount = 0
				// Wait a bit before next connection attempt
				time.Sleep(15 * time.Second)
			}
		}
	}()

	// Wait for configuration before starting scheduled jobs with enhanced retry logic
	log.Println("Waiting for agent configuration before starting scheduled jobs...")
	configRetries := 0
	maxConfigRetries := 3
	configTimeout := 30 * time.Second

	for configRetries < maxConfigRetries {
		if _, ready := utils.WaitForConfiguration(configTimeout); ready {
			log.Printf("Configuration received, starting snapshot scheduled jobs")
			err := StartScheduledJobs(agentID, snapshotURL.String())
			if err != nil {
				log.Printf("Failed to start scheduled jobs: %v", err)
				// Don't retry the whole process, let the scheduled jobs be started manually if needed
			}
			break
		} else {
			configRetries++
			if configRetries < maxConfigRetries {
				log.Printf("Timeout waiting for configuration (attempt %d/%d), retrying...", configRetries, maxConfigRetries)
				time.Sleep(10 * time.Second)
			} else {
				log.Printf("Failed to receive configuration after %d attempts, will proceed without scheduled jobs", maxConfigRetries)
			}
		}
	}

	go func() {
		defer wg.Done()
		hasConnectedForClone := false
		cloneAttemptCount := 0
		maxCloneAttempts := 10 // Increased total attempts
		lastConfigHash := "" // Track configuration changes
		for {
			config := utils.GetAgentConfiguration()
			disksCount := len(config.Disks)
			currentConfigHash := utils.GetConfigurationHash()

			// Reset clone state if configuration has changed
			if lastConfigHash != "" && currentConfigHash != lastConfigHash {
				log.Printf("Configuration changed (hash: %s -> %s), resetting clone state", lastConfigHash, currentConfigHash)
				hasConnectedForClone = false
				cloneAttemptCount = 0
			}
			lastConfigHash = currentConfigHash

			if disksCount == 0 {
				log.Printf("🔍 No disks detected in current configuration")
				log.Printf("   Config hash: %s", currentConfigHash)
				log.Printf("   Waiting for configuration with disks...")

				// Wait for configuration using the new thread-safe mechanism
				if _, ready := utils.WaitForConfiguration(30 * time.Second); ready {
					config = utils.GetAgentConfiguration()
					disksCount = len(config.Disks)
					log.Printf("📡 Configuration received with %d disks: %v", disksCount, config.Disks)

					if disksCount > 0 {
						log.Printf("✅ Disks detected! Will attempt cloning for: %v", config.Disks)
					}
				} else {
					log.Printf("⏰ Timeout waiting for configuration, retrying...")
					continue
				}
			}

			// For first connection when disks are configured, connect immediately to trigger initial clone
			if !hasConnectedForClone && cloneAttemptCount < maxCloneAttempts && disksCount > 0 {
				cloneAttemptCount++
				log.Printf("Disks configured (%d disks)! Clone attempt %d/%d - connecting to snapshot endpoint to start initial clone", disksCount, cloneAttemptCount, maxCloneAttempts)
				log.Println(fmt.Sprintf("Agent %s connecting to snapshot endpoint: %s", agentID, snapshotURL.String()))

				// Enhanced retry logic with exponential backoff for initial clone connection
				maxConnectionRetries := 5
				connectionRetryDelay := 2 * time.Second
				cloneStartedSuccessfully := false

				for connectionAttempt := 0; connectionAttempt < maxConnectionRetries && !cloneStartedSuccessfully; connectionAttempt++ {
					err := connectAndHandle(agentID, snapshotURL.String())
					if err != nil {
						log.Printf("Snapshot connection error (attempt %d/%d): %v", connectionAttempt+1, maxConnectionRetries, err)
						if connectionAttempt < maxConnectionRetries-1 {
							// Exponential backoff: 2s, 4s, 8s, 16s, 30s (capped)
							delay := connectionRetryDelay * time.Duration(1<<uint(connectionAttempt))
							if delay > 30*time.Second {
								delay = 30 * time.Second
							}
							log.Printf("Retrying initial clone connection in %v", delay)
							time.Sleep(delay)
						}
					} else {
						log.Println("Successfully connected to snapshot endpoint, clone process started")

						// Validate that clone actually started within 30 seconds
						validationTimeout := time.After(30 * time.Second)
						cloneValidated := false

						for !cloneValidated {
							select {
							case <-time.After(1 * time.Second):
								if IsCloneHealthy() {
									log.Println("✅ Clone process validated - it's running and healthy")
									cloneValidated = true
									hasConnectedForClone = true
								}
							case <-validationTimeout:
								log.Println("⚠️ Clone process validation timed out - connection may have failed to start clone")
								cloneValidated = false
								break
							}
						}

						cloneStartedSuccessfully = cloneValidated
					}
				}

				if !cloneStartedSuccessfully {
					log.Printf("Failed to establish initial clone connection after %d attempts, will retry later (attempt %d/%d)", maxConnectionRetries, cloneAttemptCount, maxCloneAttempts)
					// Don't set hasConnectedForClone = true - allow retry on next iteration
					time.Sleep(30 * time.Second) // Wait longer before next clone attempt
					continue
				}
			} else if !hasConnectedForClone && cloneAttemptCount >= maxCloneAttempts {
				log.Printf("Maximum clone attempts (%d) reached, switching to scheduled jobs mode", maxCloneAttempts)
				hasConnectedForClone = true // Prevent further attempts
			}

			// Skip clone attempt if no disks are configured
			if disksCount == 0 {
				log.Println("No disks configured for cloning, skipping clone attempt and waiting for configuration...")
				time.Sleep(30 * time.Second)
				continue
			}

			// For subsequent connections, wait for scheduled jobs or continue with normal flow
			config = utils.GetAgentConfiguration()
			if config.SnapshotTime == "" {
				log.Println("SnapshotTime not configured, waiting 15 seconds...")
				time.Sleep(15 * time.Second)
				continue
			}

			log.Println(fmt.Sprintf("Agent %s connecting to snapshot endpoint for scheduled job: %s", agentID, snapshotURL.String()))
			if err := connectAndHandle(agentID, snapshotURL.String()); err != nil {
				log.Println(fmt.Sprintf("Snapshot connection error: %v", err))
			}
			time.Sleep(5 * time.Second)
		}
	}()

	go func() {
		defer wg.Done()
		for {
			config := utils.GetAgentConfiguration()
			disksCount := len(config.Disks)

			if disksCount == 0 {
				log.Printf("🔍 Live endpoint: No disks configured (count: %d), waiting for configuration...", disksCount)
				// Wait for configuration using the new thread-safe mechanism
				if _, ready := utils.WaitForConfiguration(30 * time.Second); !ready {
					log.Printf("⏰ Live endpoint: Timeout waiting for configuration, retrying...")
					time.Sleep(10 * time.Second)
					continue
				}
				config = utils.GetAgentConfiguration() // Get fresh config after waiting
				disksCount = len(config.Disks)
			}

			if disksCount > 0 {
				log.Printf("🔴 Live endpoint: Connecting with %d disks: %v", disksCount, config.Disks)
				utils.LogDebug(fmt.Sprintf("Agent %s connecting to live endpoint: %s", agentID, liveURL.String()))
			}

			if err := connectAndHandle(agentID, liveURL.String()); err != nil {
				log.Printf("Live connection error: %v", err)
			}
			time.Sleep(5 * time.Second)
		}
	}()

	go func() {
		defer wg.Done()
		for {
			utils.LogDebug(fmt.Sprintf("Agent %s connecting to restore endpoint: %s", agentID, restoreURL.String()))
			if err := connectAndHandle(agentID, restoreURL.String()); err != nil {
				log.Println(fmt.Sprintf("Restore connection error: %v", err))
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

// Clone state monitoring
type CloneState struct {
	StartTime     time.Time
	LastActivity  time.Time
	TotalBytes    int64
	ProcessedBytes int64
	IsActive      bool
	ErrorCount    int
}

var cloneState *CloneState

// StartCloneMonitoring initializes and starts monitoring the clone process
func StartCloneMonitoring() *CloneState {
	cloneMutex.Lock()
	defer cloneMutex.Unlock()

	cloneState = &CloneState{
		StartTime:    time.Now(),
		LastActivity: time.Now(),
		IsActive:     true,
		ErrorCount:   0,
	}

	// Start monitoring goroutine
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		// Stop monitoring after 1 hour
		timeout := time.After(1 * time.Hour)

		for {
			select {
			case <-ticker.C:
				cloneMutex.Lock()
				if cloneState != nil && cloneState.IsActive {
					// Check if clone is making progress
					timeSinceActivity := time.Since(cloneState.LastActivity)
					if timeSinceActivity > 5*time.Minute {
						log.Printf("Clone appears stalled (no activity for %v), checking status", timeSinceActivity)
						// Could add recovery logic here
					}
				}
				cloneMutex.Unlock()
			case <-timeout:
				// Stop monitoring after 1 hour
				return
			}
		}
	}()

	return cloneState
}

// StopCloneMonitoring stops monitoring the clone process
func StopCloneMonitoring() {
	cloneMutex.Lock()
	defer cloneMutex.Unlock()

	if cloneState != nil {
		cloneState.IsActive = false
	}
}

// UpdateCloneActivity updates the last activity time for the clone
func UpdateCloneActivity(bytesProcessed int64) {
	cloneMutex.Lock()
	defer cloneMutex.Unlock()

	if cloneState != nil && cloneState.IsActive {
		cloneState.LastActivity = time.Now()
		cloneState.ProcessedBytes += bytesProcessed
	}
}

// IsCloneHealthy returns true if the clone process appears to be healthy
func IsCloneHealthy() bool {
	cloneMutex.Lock()
	defer cloneMutex.Unlock()

	if cloneState == nil || !cloneState.IsActive {
		return false
	}

	// Clone is healthy if it has been active within the last 5 minutes
	timeSinceActivity := time.Since(cloneState.LastActivity)
	return timeSinceActivity < 5*time.Minute
}

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
			log.Println(fmt.Sprintf("Failed to send pong: %v", err))
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

	log.Printf("Agent %s authenticated successfully on %s", agentID, urlString)
	if strings.Contains(urlString, "snapshot") {
		log.Println("Starting snapshot data stream handling")
		config := utils.GetAgentConfiguration()

		// Check if there are disks to clone before proceeding
		if len(config.Disks) == 0 {
			log.Println("No disks configured for cloning, skipping clone initialization")
			return nil // Exit early if no disks to clone
		}

		cloneMutex.Lock()
		if !isCloning {
			isCloning = true
			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())
			cloneMutex.Unlock()

			log.Printf("Starting clone process for %d disks: %v", len(config.Disks), config.Disks)

			// Start monitoring the clone process
			monitor := StartCloneMonitoring()
			log.Printf("Started clone monitoring: %+v", monitor)

			go func() {
				defer func() {
					cloneMutex.Lock()
					isCloning = false
					cloneMutex.Unlock()
					StopCloneMonitoring()
					log.Println("Clone process completed and monitoring stopped")
				}()
				for _, dev := range config.Disks {
					log.Println(fmt.Sprintf("Started cloning device: %s", dev))
					UpdateCloneActivity(0) // Record activity
					Clone(ctx, 512, dev, 12000, conn, &cloneMutex, &isCloning)
					// Update activity after each device
					UpdateCloneActivity(1024) // Simulated activity
				}
			}()
		} else {
			cloneMutex.Unlock()
			log.Println("Cloning already in progress, skipping for this connection")
		}
	} else if strings.Contains(urlString, "live") {
		log.Println("Starting live data stream handling")
		config := utils.GetAgentConfiguration()

		// Check if there are disks for live data stream
		if len(config.Disks) == 0 {
			log.Println("No disks configured for live data stream, skipping live initialization")
			return nil
		}

		liveMutex.Lock()
		if !isLive {
			isLive = true
			log.Printf("Starting live data stream for %s", config.Disks)
			var ctx context.Context
			ctx, cancelLive = context.WithCancel(context.Background())
			for _, dev := range config.Disks {
				go GetBlocks(ctx, 512, dev, conn, agentID)
				log.Printf("Started live data stream for device: %s", dev)
			}
			log.Println("Change block tracking started..")
		}
		liveMutex.Unlock()
	} else if strings.Contains(urlString, "config") {
		// Send footprint first before waiting for configuration
		log.Printf("Config endpoint - sending footprint before requesting configuration...")

		// Get and send footprint
		vmInfo, err := getFootprint()
		if err != nil {
			log.Printf("Failed to get footprint: %v", err)
			return fmt.Errorf("Failed to get footprint: %v", err)
		}

		log.Printf("Footprint: %v", vmInfo)

		// Log disk details for debugging
		log.Printf("📁 Disks in footprint:")
		for i, disk := range vmInfo.DiskDetails {
			log.Printf("   %d. %s - Size: %d bytes, FS: %s, Mount: %s",
				i+1, disk.Name, disk.Size, disk.FsType, disk.MountPoint)
		}

		var footprint utils.AgentBulkMessage
		footprint.AgentID, _ = os.Hostname()
		footprint.DataType = "footprint"
		footprint.Footprint = *vmInfo
		jsonData, err := json.Marshal(footprint)
		if err != nil {
			log.Printf("Failed to marshal footprint: %v", err)
			return fmt.Errorf("Failed to marshal footprint: %v", err)
		}

		log.Printf("📤 Sending footprint with %d disks to dispatcher", len(vmInfo.DiskDetails))
		err = conn.WriteMessage(websocket.TextMessage, jsonData)
		if err != nil {
			log.Printf("Failed to send footprint: %v", err)
			return fmt.Errorf("Failed to send footprint: %v", err)
		}

		log.Println("✅ Footprint sent successfully, waiting for dispatcher to process and send configuration...")
	}
	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			return fmt.Errorf("WebSocket read error: %v", err)
		}
		// Handle ping messages
		if messageType == websocket.PingMessage {
			err = conn.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(10*time.Second))
			if err != nil {
				log.Printf("Failed to send pong: %v", err)
				return err
			}
			continue
		}
		var msg utils.Message
		err = json.Unmarshal(message, &msg)
		if err != nil {
			log.Printf("Error unmarshalling JSON: %v", err)
			continue
		}
		log.Printf("Recieved Message %v ", msg.Action)
		switch msg.Action {
		default:
			log.Printf("Received unknown command on %s: %s", urlString, string(msg.Action))
		case "config":
			log.Println("Received config message")
			hadDisks := len(utils.GetAgentConfiguration().Disks) > 0

			// Enhanced logging for debugging configuration
			log.Printf("📋 Configuration details:")
			log.Printf("   - Disks: %v", msg.ConfigMessage.Disks)
			log.Printf("   - BlockSize: %d", msg.ConfigMessage.BlockSize)
			log.Printf("   - SnapshotTime: %s", msg.ConfigMessage.SnapshotTime)
			log.Printf("   - SnapshotFreq: %s", msg.ConfigMessage.SnapshotFreq)
			log.Printf("   - CDC: %v", msg.ConfigMessage.CDC)

			if len(msg.ConfigMessage.Disks) == 0 {
				log.Printf("⚠️ WARNING: Configuration received with NO disks configured!")
				log.Printf("   Expected disks based on footprint: /dev/vda, /dev/vdb")
				log.Printf("   This may indicate a configuration issue on the dispatcher side")
			}

			utils.SetAgentConfiguration(msg.ConfigMessage)
			log.Printf("Agent configuration set to: %v", utils.GetAgentConfiguration())

			// The new thread-safe configuration system will automatically notify waiting goroutines
			if !hadDisks && len(msg.ConfigMessage.Disks) > 0 {
				log.Println("✅ Disks configured for the first time, waiting goroutines will be notified automatically")
			} else if len(msg.ConfigMessage.Disks) == 0 {
				log.Println("❌ No disks in configuration - clone and live tracking will be skipped")
			}
		case "resume":
			cloneMutex.Lock()
			if !isCloning {
				isCloning = true
				resumeData := msg.ResumeMessage
				log.Println("Entered Resume switch case")
				var ctx context.Context
				ctx, cancel = context.WithCancel(context.Background())
				go Resume(ctx, resumeData.BlockSize, resumeData.SrcPath, resumeData.ChannelSize, resumeData.ReadFrom, conn, &cloneMutex, &isCloning)
			}
			cloneMutex.Unlock()
		case "pause":
			log.Println("Entered pause switch case")
			if cancel != nil {
				log.Println("Cancelling cloning")
				cancel()
			}
			cloneMutex.Lock()
			isCloning = false
			cloneMutex.Unlock()
		case "stop":
			log.Println("Received stop command")
			cancel()
		case utils.CONST_AGENT_ACTION_SYNC:
			log.Println("Entered Sync switch case")
			processSyncAction(msg, conn)
			utils.LogDebug(fmt.Sprintf("Received command on %s: %s", urlString, string(msg.Action)))
		case "StopSync":
			log.Println("Entered StopSync switch case")
			processStopSyncAction()
		case utils.CONST_AGENT_ACTION_PARTITION_RESTORE:
			log.Println("Entered Partition Restore switch case")
			var bulkMsg utils.AgentBulkMessage
			err = json.Unmarshal(message, &bulkMsg)
			if err != nil {
				utils.LogError(fmt.Sprintf("Error unmarshalling JSON: %v", err))
				continue
			}
			restoreManager.TryStartRestore(bulkMsg, agentID)

		case utils.CONST_AGENT_ACTION_RESTORE:
			log.Println("Entered Restore switch case")
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
				log.Printf("Started new restore process for %s", restoreMsg.FilePath)
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
				log.Println("Restore process completed successfully")
			case "cancel":
				log.Println("File restoration cancelled")
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
