package dispatcher

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xmigrate/blxrep/service"
	"github.com/xmigrate/blxrep/utils"

	"github.com/gorilla/websocket"
)

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			// For simplicity, we're allowing all origins. In production, we should restrict this.
			return true
		},
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	agentsMutex sync.Mutex
	agents      map[string]*AgentChannels
)

type AgentChannels struct {
	ID              string
	SnapConn        *websocket.Conn
	LiveConn        *websocket.Conn
	RestoreConn     *websocket.Conn
	SnapshotChannel chan utils.AgentBulkMessage
	LiveChannel     chan utils.AgentBulkMessage
	SyncChannel     chan utils.AgentBulkMessage
	Progress        int
}

func Start(dataDir string, targets []string, policyDir string) {
	utils.AppConfiguration.DataDir = dataDir
	utils.AppConfiguration.Targets = targets
	utils.AppConfiguration.Cdc = true
	utils.AppConfiguration.PolicyDir = policyDir
	utils.LogDebug("Dispatcher started")
	var err error

	logDir := filepath.Join(dataDir, "logs")
	err = utils.InitLogging(logDir)
	if err != nil {
		utils.LogError(fmt.Sprintf("Error initializing log directory: %v", err))
		return
	}
	defer utils.CloseLogFile()
	// Initialize the agents map
	agents = make(map[string]*AgentChannels)

	// Set up HTTP server with WebSocket endpoints
	http.HandleFunc("/ws/snapshot", handleSnapshotWebSocket)
	http.HandleFunc("/ws/live", handleLiveWebSocket)
	http.HandleFunc("/ws/restore", handleRestoreWebSocket)
	http.HandleFunc("/ws/config", handleConfigWebSocket)

	// Start the HTTP server
	go func() {
		utils.LogDebug("Starting WebSocket server on :8080")
		if err := http.ListenAndServe(":8080", nil); err != nil {
			utils.LogError(fmt.Sprintf("WebSocket server error: %v", err))
		}
	}()

	err = StartSnapshotCleanupJobs()
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to start snapshot cleanup job: %v", err))
	}

	err = StaledActionsJob()
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to start staled actions job: %v", err))
	}

	var wg sync.WaitGroup
	wg.Add(3)
	// Config scheduler
	go func() {
		defer wg.Done()
		for {
			utils.LogDebug(fmt.Sprintf("Config scheduler parsing policy from: %s", utils.AppConfiguration.PolicyDir))
			if err := ConfigScheduler(utils.AppConfiguration.PolicyDir); err != nil {
				utils.LogError(fmt.Sprintf("Config scheduler error: %v", err))
			}
			time.Sleep(10 * time.Second)
		}
	}()

	err = CompressJob()
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to start compress job: %v", err))
	}

	// Keep the main goroutine alive
	select {}
}

func handleSnapshotWebSocket(w http.ResponseWriter, r *http.Request) {
	utils.LogDebug(fmt.Sprintf("Snapshot WebSocket connection attempt from %s", r.RemoteAddr))
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		utils.LogError(fmt.Sprintf("Snapshot WebSocket upgrade error: %v", err))
		return
	}
	defer conn.Close()

	agentID, err := authenticateClient(conn)
	if err != nil {
		utils.LogError(fmt.Sprintf("Snapshot authentication failed: %v", err))
		return
	}
	agent, err := service.GetAgent(agentID)
	if err != nil {
		utils.LogError(fmt.Sprintf("getting agent %s: %v", agentID, err))
	}

	utils.LogDebug(fmt.Sprintf("registering agent %s", agentID))
	agent.AgentId = agentID
	agent.Connected = true
	agent.ClonePercentage = 0
	agent.Prerequisites = true
	agent.CurrentAgentAction = ""
	if err := service.InsertOrUpdateAgent(agent); err != nil {
		utils.LogError("Agent registration failed disconnecting " + agentID + ": " + err.Error())
		return
	}
	utils.LogDebug("Agent " + agentID + " connected to snapshot endpoint")
	agentChannels := getOrCreateAgentChannels(agentID, conn, nil, nil)

	go WriteFullBackupToFile(&agentChannels.SnapshotChannel, agentID, &agentChannels.Progress)

	for {
		var bulkMessage utils.AgentBulkMessage
		err := conn.ReadJSON(&bulkMessage)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				utils.LogError(fmt.Sprintf("WebSocket read error: %v", err))
			}
			break
		}

		select {
		case agentChannels.SnapshotChannel <- bulkMessage:

		default:
			utils.LogError(fmt.Sprintf("Snapshot channel full for agent %s", agentID))
			return
		}
	}
}

func handleLiveWebSocket(w http.ResponseWriter, r *http.Request) {
	utils.LogDebug(fmt.Sprintf("Live WebSocket connection attempt from %s", r.RemoteAddr))
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		utils.LogError(fmt.Sprintf("Live WebSocket upgrade error: %v", err))
		return
	}
	defer conn.Close()

	agentID, err := authenticateClient(conn)
	if err != nil {
		utils.LogError(fmt.Sprintf("Live authentication failed: %v", err))
		return
	}
	utils.LogDebug("Agent " + agentID + " connected to live endpoint")
	agentChannels := getOrCreateAgentChannels(agentID, nil, conn, nil)

	// Set up ping-pong
	conn.SetPongHandler(func(string) error {
		utils.LogDebug("agent " + agentID + " pong received")
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	// Start a goroutine for sending pings
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			<-ticker.C
			utils.LogDebug("Pinging agent " + agentID)
			if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
				utils.LogError(fmt.Sprintf("Ping failed for agent %s: %v", agentID, err))
				return
			}
		}
	}()
	// Set initial read deadline
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	agentConfig, err := service.GetAgent(agentID)
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to get agent config: %v", err))
	}
	if agentConfig.LiveSyncFreq == "" {
		for {
			time.Sleep(5 * time.Second)
			utils.LogDebug(fmt.Sprintf("Agent %s has no live sync frequency set, waiting...", agentID))
			agentConfig, err = service.GetAgent(agentID)
			if err != nil {
				utils.LogError(fmt.Sprintf("Failed to get agent config: %v", err))
			}
			if agentConfig.LiveSyncFreq != "" {
				break
			}
		}
	}
	liveSyncFreq, err := utils.ParseDuration(agentConfig.LiveSyncFreq)
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to parse live sync frequency: %v", err))
	}
	go ChangeSectorTracker(&agentChannels.LiveChannel, agentID)

	ticker := time.NewTicker(liveSyncFreq)
	defer ticker.Stop() // Ensure the ticker is stopped when the function returns

	// Start a goroutine to handle the periodic sync
	go func() {
		for range ticker.C {
			SyncData(conn, agentID, false)
			// TotalSyncData(conn, agentID)
		}
	}()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				utils.LogError("Recovered from panic in WriteIncrementalBackupToFile: " + fmt.Sprintf("%v", r))
			}
		}()
		utils.LogDebug("Starting incremental backup")
		WriteIncrementalBackupToFile(&agentChannels.SyncChannel, agentID, 512)
	}()
	for {
		_, p, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				utils.LogError(fmt.Sprintf("WebSocket unexpected close error for agent %s: %v", agentID, err))
			} else if strings.Contains(err.Error(), "i/o timeout") {
				utils.LogError(fmt.Sprintf("WebSocket timeout for agent %s: %v", agentID, err))
			} else {
				utils.LogError(fmt.Sprintf("WebSocket read error for agent %s: %v", agentID, err))
			}
			utils.LogError(fmt.Sprintf("live WS Message read failed for agent %s: %v\n", agentID, err))
			// Update agent status to disconnected
			agent, _ := service.GetAgent(agentID)
			agent.Connected = false
			agent.LastSeen = time.Now()
			service.InsertOrUpdateAgent(agent)
			return
		}
		var blkMsg utils.AgentBulkMessage

		dec := gob.NewDecoder(bytes.NewReader(p))
		if err := dec.Decode(&blkMsg); err != nil {
			utils.LogError(fmt.Sprintf("Could not decode binary as LiveSector or AgentBulkMessage for agent %s: %v", agentID, err))
			continue
		}
		if blkMsg.DataType == "incremental" {
			select {
			case agentChannels.SyncChannel <- blkMsg:
				utils.LogDebug(fmt.Sprintf("AgentBulkMessage sent to SyncChannel for agent %s, SrcPath: %s", agentID, blkMsg.SrcPath))
			default:
				utils.LogError(fmt.Sprintf("SyncChannel channel for agent %s, SrcPath %s is full or blocked", agentID, blkMsg.SrcPath))
			}
		} else {
			select {
			case agentChannels.LiveChannel <- blkMsg:
			default:
				utils.LogError(fmt.Sprintf("Live channel full for agent %s", agentID))
			}
		}
	}
}

func handleConfigWebSocket(w http.ResponseWriter, r *http.Request) {
	utils.LogDebug(fmt.Sprintf("Config WebSocket connection attempt from %s", r.RemoteAddr))
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		utils.LogError(fmt.Sprintf("Config WebSocket upgrade error: %v", err))
		return
	}
	defer conn.Close()

	agentID, err := authenticateClient(conn)
	if err != nil {
		utils.LogError(fmt.Sprintf("Config authentication failed: %v", err))
		return
	}
	utils.LogDebug("Agent " + agentID + " connected to config endpoint")

	// Get agent configuration from boltdb
	agent, err := service.GetAgent(agentID)
	if err != nil {
		utils.LogError(fmt.Sprintf("Failed to get agent config: %v", err))
		agent.AgentId = agentID
		service.InsertOrUpdateAgent(agent)
		return
	}
	// TODO: Create agent channels for seperate disks as we get the agent config here
	// Send initial configuration to agent
	configMessage := utils.Message{
		Action: "config",
		ConfigMessage: utils.AgentConfig{
			BlockSize:      512,
			SyncFreq:       3,
			Disks:          agent.Disks,
			BandwidthLimit: agent.CloneSchedule.Bandwidth,
			SnapshotFreq:   agent.CloneSchedule.Frequency,
			SnapshotTime:   agent.CloneSchedule.Time,
			CDC:            true,
		},
	}

	if err := conn.WriteJSON(configMessage); err != nil {
		utils.LogError(fmt.Sprintf("Failed to send config to agent: %v", err))
		return
	}
	utils.LogDebug(fmt.Sprintf("Config sent to agent %s: %v", agentID, configMessage))

	for {
		var bulkMessage utils.AgentBulkMessage
		err := conn.ReadJSON(&bulkMessage)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				utils.LogError(fmt.Sprintf("WebSocket read error: %v", err))
			}
			break
		}
		if bulkMessage.DataType == "footprint" {
			utils.LogDebug(fmt.Sprintf("Footprint received from agent %s: %v", agentID, bulkMessage.Footprint))
			agent.Footprint = bulkMessage.Footprint
			service.InsertOrUpdateAgent(agent)
		}
	}
}

func handleRestoreWebSocket(w http.ResponseWriter, r *http.Request) {
	utils.LogDebug(fmt.Sprintf("Restore WebSocket connection attempt from %s", r.RemoteAddr))
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		utils.LogError(fmt.Sprintf("Restore WebSocket upgrade error: %v", err))
		return
	}
	defer conn.Close()

	agentID, err := authenticateClient(conn)
	if err != nil {
		utils.LogError(fmt.Sprintf("Restore authentication failed: %v", err))
		return
	}
	utils.LogDebug("Agent " + agentID + " connected to restore endpoint")
	_ = getOrCreateAgentChannels(agentID, nil, nil, conn)

	ticker := time.NewTicker(time.Duration(10) * time.Second)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			utils.LogDebug(fmt.Sprintf("Checking actions for agent %s", agentID))
			CheckActions(agentID)
		}
	}()

	for {
		var bulkMessage utils.AgentBulkMessage
		err := conn.ReadJSON(&bulkMessage)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				utils.LogError(fmt.Sprintf("WebSocket read error: %v", err))
			}
			break
		}
	}
}

func getOrCreateAgentChannels(agentID string, snapConn *websocket.Conn, liveConn *websocket.Conn, restConn *websocket.Conn) *AgentChannels {
	agentsMutex.Lock()
	defer agentsMutex.Unlock()

	utils.LogDebug(fmt.Sprintf("getOrCreateAgentChannels called for agent %s. Current agents count: %d", agentID, len(agents)))

	if agents == nil {
		utils.LogError("agents map is nil. Reinitializing...")
		agents = make(map[string]*AgentChannels)
	}

	if agent, exists := agents[agentID]; exists {
		utils.LogDebug(fmt.Sprintf("Existing agent found for %s", agentID))
		// Update connections if provided
		if snapConn != nil {
			agent.SnapConn = snapConn
		}
		if liveConn != nil {
			agent.LiveConn = liveConn
		}
		if restConn != nil {
			agent.RestoreConn = restConn
		}
		return agent
	}

	utils.LogDebug(fmt.Sprintf("Creating new agent for %s", agentID))
	newAgent := &AgentChannels{
		ID:              agentID,
		SnapshotChannel: make(chan utils.AgentBulkMessage, 10000),
		LiveChannel:     make(chan utils.AgentBulkMessage, 10000),
		SyncChannel:     make(chan utils.AgentBulkMessage, 10000),
		SnapConn:        snapConn,
		LiveConn:        liveConn,
		RestoreConn:     restConn,
	}
	agents[agentID] = newAgent
	utils.LogDebug(fmt.Sprintf("New agent added. Current agents count: %d", len(agents)))
	return newAgent
}

func authenticateClient(conn *websocket.Conn) (string, error) {
	// Read authentication message
	_, message, err := conn.ReadMessage()
	if err != nil {
		return "", fmt.Errorf("WebSocket read error: %v", err)
	}

	// Implement your authentication logic here
	// For this example, we'll use the message as the agentID
	// In a real-world scenario, you'd validate the token and extract the agentID
	agentID := string(message)

	if agentID == "" {
		return "", fmt.Errorf("Invalid authentication token")
	}

	conn.WriteMessage(websocket.TextMessage, []byte("Authenticated"))
	return agentID, nil
}

func ShowCheckpoints(start string, end string, agent string, dataDir string, disk string) ([]utils.Checkpoint, error) {
	files, err := os.ReadDir(filepath.Join(dataDir, "incremental"))
	if err != nil {
		return nil, fmt.Errorf("error reading directory: %w", err)
	}

	var checkpoints []utils.Checkpoint
	safeDisk := strings.ReplaceAll(disk, "/", "-")
	prefix := fmt.Sprintf("%s_%s", agent, safeDisk)
	utils.LogDebug(fmt.Sprintf("Debug: agent=%s, disk=%s, safeDisk=%s, prefix=%s dataDir=%s\n", agent, disk, safeDisk, prefix, dataDir))
	for _, file := range files {
		if strings.HasPrefix(file.Name(), prefix+"_") && strings.HasSuffix(file.Name(), ".bak") {
			ts, err := time.Parse("200601021504", strings.TrimSuffix(strings.TrimPrefix(file.Name(), prefix+"_"), ".bak"))
			if err != nil {
				utils.LogError(fmt.Sprintf("Error parsing timestamp for file %s: %v\n", file.Name(), err))
				continue
			}
			checkpoints = append(checkpoints, utils.Checkpoint{
				Timestamp: ts,
				Filename:  file.Name(),
			})
		}
	}

	sort.Slice(checkpoints, func(i, j int) bool {
		return checkpoints[i].Timestamp.Before(checkpoints[j].Timestamp)
	})

	var startTime, endTime time.Time
	var err1, err2 error

	if start != "" {
		startTime, err1 = time.Parse("200601021504", start)
		if err1 != nil {
			return nil, fmt.Errorf("error parsing start time. Please use format YYYYMMDDHHMM: %w", err1)
		}
	}
	if end != "" {
		endTime, err2 = time.Parse("200601021504", end)
		if err2 != nil {
			return nil, fmt.Errorf("error parsing end time. Please use format YYYYMMDDHHMM: %w", err2)
		}
	}

	var filteredCheckpoints []utils.Checkpoint
	// fmt.Println("Available checkpoints:")
	for _, cp := range checkpoints {
		if (start == "" || cp.Timestamp.After(startTime) || cp.Timestamp.Equal(startTime)) &&
			(end == "" || cp.Timestamp.Before(endTime) || cp.Timestamp.Equal(endTime)) {
			// fmt.Printf("%s (%s)\n", cp.Timestamp.Format("2006-01-02 15:04"), filepath.Base(cp.Timestamp.Format("200601021504")))
			filteredCheckpoints = append(filteredCheckpoints, cp)
		}
	}
	utils.LogDebug(fmt.Sprintf("Found %d checkpoints for agent %s, disk %s", len(filteredCheckpoints), agent, disk))
	return filteredCheckpoints, nil
}

func ShowDisks(agentID string, dataDir string) ([]string, error) {
	disks, err := service.GetAgent(agentID)
	if err != nil {
		return nil, fmt.Errorf("error getting agent: %w", err)
	}
	return disks.Disks, nil
}

func Restore(checkpoint, dataDir string, agent string) {
	utils.LogDebug(fmt.Sprintf("Restoring checkpoint %s for agent %s", checkpoint, agent))
	processAndCheckCompletion(agent, checkpoint, dataDir)
}

func CheckpointMergeFromSnapshot(snapshotPath string, snapshotId string) (string, error) {
	// Extract the base filename from the full path
	baseFileName := filepath.Base(snapshotPath)

	// Parse components from the filename (agentID_diskpath_202403191200.img)
	parts := strings.Split(strings.TrimSuffix(baseFileName, ".img"), "_")
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid snapshot filename format: %s", baseFileName)
	}
	// Extract timestamp from the last part
	timestamp := parts[len(parts)-1]

	// Validate timestamp format (YYYYMMDDHHMM)
	if len(timestamp) != 12 {
		return "", fmt.Errorf("invalid timestamp format in filename: %s", timestamp)
	}
	_, err := time.Parse("200601021504", timestamp)
	if err != nil {
		return "", fmt.Errorf("invalid timestamp in filename: %w", err)
	}

	// Reconstruct agent identifier (everything except the timestamp)
	// Join all parts except the last one (timestamp) with underscore
	agent := strings.Join(parts[:len(parts)-1], "_")

	utils.LogDebug(fmt.Sprintf("Processing snapshot %s for agent %s with timestamp %s",
		baseFileName, agent, timestamp))

	// Create the final snapshot path
	finalSnapshot := filepath.Join(utils.AppConfiguration.DataDir, "final", fmt.Sprintf("%s-%s%s.img", parts[0], snapshotId, strings.ReplaceAll(parts[1], "-", "_")))

	// Copy the snapshot to the final location
	err = utils.CopyFile(snapshotPath, finalSnapshot)
	if err != nil {
		return "", fmt.Errorf("failed to copy snapshot to final location: %w", err)
	}
	utils.LogDebug(fmt.Sprintf("Copied snapshot to final location: %s", finalSnapshot))

	// Process incremental backups
	err = ProcessIncrementalBackups(filepath.Join(utils.AppConfiguration.DataDir, "incremental"), agent, finalSnapshot, timestamp)
	if err != nil {
		return "", fmt.Errorf("failed to process incremental backups: %w", err)
	}

	return finalSnapshot, nil
}

func CheckpointMerge(checkpoint, dataDir string, agent string, disk string) (string, error) {
	pattern := `(\d{12})\.bak$`
	re := regexp.MustCompile(pattern)
	match := re.FindStringSubmatch(checkpoint)
	if len(match) < 2 {
		return "", fmt.Errorf("no timestamp found in the filename")
	}
	checkpoint = match[1]
	safeDisk := strings.ReplaceAll(disk, "/", "-")
	agent = fmt.Sprintf("%s_%s", agent, safeDisk)

	utils.LogDebug(fmt.Sprintf("Mounting checkpoint %s for agent %s", checkpoint, agent))
	processAndCheckCompletion(agent, checkpoint, dataDir)
	finalSnapshot := filepath.Join(dataDir, "final", fmt.Sprintf("%s-%s.img", agent, checkpoint))

	return finalSnapshot, nil
}
