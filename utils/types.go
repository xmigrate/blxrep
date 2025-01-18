package utils

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"time"
)

type Frequency string

type AgentDataBlock struct {
	BlockNumber uint64 `json:"block_number"`
	BlockData   []byte `json:"block_data"`
	Checksum    string
}

type AgentDataType string

type AgentBulkMessage struct {
	Action      CONST_AGENT_ACTION `json:"action,omitempty"`
	AgentID     string             `json:"agent_id"`
	Footprint   VMInfo             `json:"footprint"`
	SrcPath     string             `json:"src_path"`
	Data        []AgentDataBlock   `json:"data"`
	DataType    AgentDataType      `json:"data_type"`
	StartSector uint64             `json:"start_sector"`
	EndSector   uint64             `json:"end_sector"`
	TotalBlocks uint64             `json:"total_blocks"`
	StartTime   int64              `json:"start_time"`
	Config      AgentConfig        `json:"config,omitempty"`
}

type AppConfig struct {
	PolicyDir       string   `yaml:"policy_dir"`
	Targets         []string `yaml:"targets"`
	DataDir         string   `yaml:"data_dir"`
	Cdc             bool     `yaml:"cdc"`
	SyncFreq        int      `yaml:"sync_freq"`
	ArchiveInterval string   `yaml:"archive_interval"`
}

type Event struct {
	Block    uint64
	EndBlock uint64
}

type LiveSector struct {
	StartSector uint64 `json:"start_sector"`
	EndSector   uint64 `json:"end_sector"`
	SrcPath     string `json:"src_path"`
	AgentID     string `json:"agent_id"`
}

type BlockPair struct {
	Start uint64
	End   uint64
}

type SyncData struct {
	BlockSize   int         `json:"blockSize"`
	SrcPath     string      `json:"srcPath"`
	ChannelSize int         `json:"channelSize"`
	Blocks      []BlockPair `json:"blocks"`
}

type CloneData struct {
	BlockSize   int    `json:"blockSize"`
	SrcPath     string `json:"srcPath"`
	ChannelSize int    `json:"channelSize"` // channelSize := (bandwidth * 1024 * 1024) / blockSize
}

type ResumeData struct {
	BlockSize   int    `json:"blockSize"`
	SrcPath     string `json:"srcPath"`
	ChannelSize int    `json:"channelSize"`
	ReadFrom    int64  `json:"readFrom"`
}

type LiveData struct {
	BlockSize      int            `json:"blockSize"`
	SrcPath        string         `json:"srcPath"`
	ChannelSize    int            `json:"channelSize"`
	ReadAllBlocks  bool           `json:"readAllBlocks"`
	BlocksToModify map[int64]bool `json:"blocksToModify"`
}

type RestoreData struct {
	Type        string `json:"type"`
	FilePath    string `json:"filePath"`
	Data        string `json:"data"`
	TotalChunks int    `json:"totalChunks"`
	TotalSize   int64  `json:"totalSize"`
	ChunkIndex  int    `json:"chunkIndex"`
}

type Message struct {
	Action         CONST_AGENT_ACTION `json:"action"`
	ConfigMessage  AgentConfig        `json:"config_message,omitempty"`
	CloneMessage   CloneData          `json:"clone_data,omitempty"`
	LiveMessage    LiveData           `json:"live_data,omitempty"`
	ResumeMessage  ResumeData         `json:"resume_data,omitempty"`
	SyncMessage    SyncData           `json:"sync_data,omitempty"`
	RestoreMessage RestoreData        `json:"restore_data,omitempty"`
	Tags           map[string]string  `json:"tags,omitempty"`
}

type Checkpoint struct {
	Timestamp time.Time
	Filename  string
}

type DiskDetailsStruct struct {
	FsType     string `json:"filesystem"`
	Size       uint64 `json:"disk_size"` // divide 512 for block Size
	Uuid       string `json:"disk_uuid"`
	Name       string `json:"disk_name"`
	MountPoint string `json:"disk_mnt"`
}

type VMInfo struct {
	Hostname      string `json:"hostname"`
	CpuModel      string `json:"cpu_model"`
	CpuCores      int    `json:"cpu_cores"`
	Ram           uint64 `json:"ram"`
	InterfaceInfo []struct {
		Name         string `json:"name"`
		IPAddress    string `json:"ip_address"`
		SubnetMask   string `json:"subnet_mask"`
		CIDRNotation string `json:"cidr_notation"`
		NetworkCIDR  string `json:"network_cidr"`
	} `json:"network_interfaces"`
	DiskDetails []DiskDetailsStruct `json:"disk_details"`
	OsDistro    string              `json:"distro"`
	OsVersion   string              `json:"os_version"`
}

type Agent struct {
	AgentId             string             `json:"agent_id"`
	Connected           bool               `json:"connected"`
	Footprint           VMInfo             `json:"footprint"`
	LastSeen            time.Time          `json:"last_seen"`
	Action              CONST_AGENT_ACTION `json:"action"`
	ActionStatus        string             `json:"action_status"`
	CurrentAgentAction  CONST_AGENT_ACTION `json:"current_agent_action"`
	Prerequisites       bool               `json:"prerequisite"`
	ClonePercentage     int                `json:"clone_percentage"`
	BlockSize           int                `json:"block_size"`
	Disks               []string           `json:"disks"`
	ChannelSize         int                `json:"channel_size"`
	CloneStatus         map[string]int     `json:"clone_status"` //disk name and status completion
	CloneSchedule       AgentSchedule      `json:"clone_schedule"`
	SnapshotRetention   int                `json:"snapshot_retention"`
	ArchiveInterval     string             `json:"archive_interval"`
	LiveSyncFreq        string             `json:"live_sync_freq"`
	TransitionAfterDays int                `json:"transition_after_days"`
	DeleteAfterDays     int                `json:"delete_after_days"`
}

type AgentSchedule struct {
	Frequency string `json:"frequency"`
	Time      string `json:"time"`
	Bandwidth int    `json:"bandwidth"`
}

type DiskSnapshot struct {
	Name       string `json:"name"`
	Progress   int    `json:"progress"`
	Status     string `json:"status"`
	SnapshotId string `json:"snapshot_id"`
}

type UTCTime struct {
	time.Time
}

// Constructor to ensure UTC
func NewUTCTime(t time.Time) UTCTime {
	return UTCTime{t.UTC()}
}

// Ensure time is always in UTC when setting
func (t *UTCTime) Set(newTime time.Time) {
	t.Time = newTime.UTC()
}

// Custom JSON marshaling/unmarshaling
func (t *UTCTime) UnmarshalJSON(data []byte) error {
	var timeStr string
	if err := json.Unmarshal(data, &timeStr); err != nil {
		return err
	}

	parsedTime, err := time.Parse("2006-01-02T15:04:05.999999", timeStr)
	if err != nil {
		return err
	}

	t.Time = parsedTime.UTC()
	return nil
}

func (t UTCTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Time.Format("2006-01-02T15:04:05.999999"))
}

// func (a *Action) UnmarshalJSON(data []byte) error {
// 	type Alias Action // Create an alias to avoid recursive calls

// 	// Create a temporary struct with disk as string
// 	aux := struct {
// 		*Alias
// 		Disk string `json:"disk"`
// 	}{
// 		Alias: (*Alias)(a),
// 	}

// 	if err := json.Unmarshal(data, &aux); err != nil {
// 		return err
// 	}

// 	// Convert the disk string to DiskSnapshot
// 	a.Disk = DiskSnapshot{
// 		Name:   aux.Disk,
// 		Status: "pending", // Set default values as needed
// 	}

// 	return nil
// }

// // For marshaling JSON (to DB)
// func (a Action) MarshalJSON() ([]byte, error) {
// 	type Alias Action

// 	aux := struct {
// 		*Alias
// 		Disk string `json:"disk"`
// 	}{
// 		Alias: (*Alias)(&a),
// 		Disk:  a.Disk.Name, // Convert DiskSnapshot back to string
// 	}

// 	return json.Marshal(aux)
// }

type BackendAction struct {
	Id             string  `json:"id"`
	ActionId       string  `json:"action_id"`
	SnapshotId     string  `json:"prepare_snapshot_id"`
	AgentId        string  `json:"agent_id"`
	OsVersion      string  `json:"os_version"`
	FileSystem     string  `json:"filesystem"`
	Distro         string  `json:"distro"`
	Disk           string  `json:"disk"`
	Action         string  `json:"action"`
	ActionType     string  `json:"action_type"`
	ActionStatus   string  `json:"action_status"`
	ActionProgress int     `json:"action_progress"`
	Hostname       string  `json:"hostname"`
	TargetName     string  `json:"target_name"`
	UpdateBackend  bool    `json:"update_backend,omitempty"`
	SourceFilePath string  `json:"source_file_path,omitempty"`
	TargetFilePath string  `json:"target_file_path,omitempty"`
	TimeFinished   int64   `json:"time_finished,omitempty"`
	TimeCreated    UTCTime `json:"time_created,omitempty"`
	TimeStarted    int64   `json:"time_started,omitempty"`
	TimeUpdated    int64   `json:"time_updated,omitempty"`
}

type Action struct {
	Id             string                  `json:"id"`
	ActionId       string                  `json:"action_id"`
	SnapshotId     string                  `json:"prepare_snapshot_id"`
	AgentId        string                  `json:"agent_id"`
	OsVersion      string                  `json:"os_version"`
	FileSystem     string                  `json:"filesystem"`
	Distro         string                  `json:"distro"`
	Disk           map[string]DiskSnapshot `json:"disk"`
	Action         string                  `json:"action"`
	ActionType     string                  `json:"action_type"`
	ActionStatus   string                  `json:"action_status"`
	ActionProgress int                     `json:"action_progress"`
	Hostname       string                  `json:"hostname"`
	TargetName     string                  `json:"target_name"`
	UpdateBackend  bool                    `json:"update_backend,omitempty"`
	SourceFilePath string                  `json:"source_file_path,omitempty"`
	TargetFilePath string                  `json:"target_file_path,omitempty"`
	TimeFinished   int64                   `json:"time_finished,omitempty"`
	TimeCreated    UTCTime                 `json:"time_created,omitempty"`
	TimeStarted    int64                   `json:"time_started,omitempty"`
	TimeUpdated    int64                   `json:"time_updated,omitempty"`
}

type RestoreMessage struct {
	Type        string `json:"type"`
	ChunkIndex  int    `json:"chunkIndex"`
	TotalChunks int    `json:"totalChunks"`
	Data        string `json:"data"`
}

type RestoreState struct {
	Buffer         *bytes.Buffer
	GzipReader     *gzip.Reader
	TarReader      *tar.Reader
	CurrentFile    *os.File
	CurrentPath    string
	BytesWritten   int64
	FilePath       string
	TotalSize      int64
	CurrentSize    int64
	TotalChunks    int
	ChunksReceived int
}

type ApiAgents struct {
	Hostname      string   `json:"hostname"`
	DisksExcluded []string `json:"disks_excluded"`
}

type AgentConfig struct {
	Agents                  []ApiAgents `json:"agents"`
	BlockSize               int         `json:"block_size"`
	Disks                   []string    `json:"disks"`
	BandwidthLimit          int         `json:"bandwidth_limit"`
	SyncFreq                int         `json:"sync_freq"`
	SnapshotFreq            string      `json:"snapshot_frequency"`
	SnapshotTime            string      `json:"snapshot_time"`
	CDC                     bool        `json:"cdc"`
	Target                  string      `json:"target"`
	TargetPrefix            string      `json:"target_prefix"`
	EnableCompression       bool        `json:"enable_compression"`
	EnableEncryption        bool        `json:"enable_encryption"`
	SnapshotRetention       int         `json:"snapshot_retention"`
	ArchiveInterval         string      `json:"archive_interval"`
	ArchiveCompressionRatio float64     `json:"archive_compression_ratio"`
	LiveSyncFreq            string      `json:"live_sync_frequency"`
	TransitionAfterDays     int         `json:"transition_after_days"`
	DeleteAfterDays         int         `json:"delete_after_days"`
}

type ApiResponse struct {
	Status      int           `json:"status"`
	BackupPlans []AgentConfig `json:"backup_plans"`
}

type ApiActionResponse struct {
	Status  int             `json:"status"`
	Actions []BackendAction `json:"actions"`
}

type ActionIdResponse struct {
	Message  string `json:"message"`
	ActionId string `json:"action_id"`
}

type ActionPutRequest struct {
	ActionId       string `json:"action_id"`
	ActionStatus   string `json:"action_status,omitempty"`
	ActionProgress int    `json:"action_progress"`
	Disk           string `json:"disk"`
}

type ActionPostRequest struct {
	Hostname       string `json:"hostname"`
	ActionStatus   string `json:"action_status,omitempty"`
	ActionProgress int    `json:"action_progress"`
	Disk           string `json:"disk"`
	Action         string `json:"action"`
	ActionType     string `json:"action_type"`
}

type HeartbeatMessage struct {
	DisPatcher string   `json:"dispatcher"`
	Agents     []string `json:"agents"`
}

type SnapshotInfo struct {
	Timestamp time.Time
	BaseName  string
	ImgSize   int64
	LogSize   int64
}

// BackupPolicy represents the structure of a backup policy file
type BackupPolicy struct {
	Name                string        `yaml:"name"`
	Description         string        `yaml:"description"`
	ArchiveInterval     string        `yaml:"archive_interval"`
	SnapshotFrequency   string        `yaml:"snapshot_frequency"`
	SnapshotTime        string        `yaml:"snapshot_time"`
	BandwidthLimit      int           `yaml:"bandwidth_limit"`
	SnapshotRetention   int           `yaml:"snapshot_retention"`
	LiveSyncFrequency   string        `yaml:"live_sync_frequency"`
	TransitionAfterDays int           `yaml:"transition_after_days"`
	DeleteAfterDays     int           `yaml:"delete_after_days"`
	Targets             []TargetGroup `yaml:"targets"`
}

type TargetGroup struct {
	Pattern       string   `yaml:"pattern"` // Supports Ansible-like patterns
	DisksExcluded []string `yaml:"disks_excluded"`
}
