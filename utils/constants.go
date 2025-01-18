package utils

type CONST_AGENT_ACTION string
type CONST_DISPATCHER_ACTION string
type CONST_ACTION_TYPE string
type CONST_ACTION_STATUS_TYPE string

const (
	CONST_ADHOC_ACTION                   CONST_ACTION_TYPE        = "ADHOC_ACTION"
	CONST_SCHEDULED_ACTION               CONST_ACTION_TYPE        = "SCHEDULED_ACTION"
	CONST_START_ACTION                   CONST_AGENT_ACTION       = "started"
	CONST_COMPRESS_ACTION                CONST_AGENT_ACTION       = "compress"
	CONST_AGENT_ACTION_CLONE             CONST_AGENT_ACTION       = "clone"
	CONST_AGENT_ACTION_PAUSE             CONST_AGENT_ACTION       = "pause"
	CONST_AGENT_ACTION_RESUME            CONST_AGENT_ACTION       = "resume"
	CONST_AGENT_ACTION_LIVE              CONST_AGENT_ACTION       = "live"
	CONST_AGENT_ACTION_STOP_LIVE         CONST_AGENT_ACTION       = "stop_live"
	CONST_AGENT_ACTION_SYNC              CONST_AGENT_ACTION       = "sync"
	CONST_AGENT_ACTION_RESTORE           CONST_AGENT_ACTION       = "restore"
	CONST_AGENT_ACTION_PREPARE           CONST_AGENT_ACTION       = "prepare"
	CONST_AGENT_ACTION_PARTITION_RESTORE CONST_AGENT_ACTION       = "partition_restore"
	CONST_ACTION_STATUS_IN_PROGRESS      CONST_ACTION_STATUS_TYPE = "in_progress"
	CONST_ACTION_STATUS_COMPLETED        CONST_ACTION_STATUS_TYPE = "completed"
	CONST_ACTION_STATUS_FAILED           CONST_ACTION_STATUS_TYPE = "failed"
	CONST_ACTION_STATUS_PAUSED           CONST_ACTION_STATUS_TYPE = "paused"
	CONST_ACTION_STATUS_WAITING          CONST_ACTION_STATUS_TYPE = "waiting"
	CONST_ACTION_STATUS_RESUMED          CONST_ACTION_STATUS_TYPE = "resumed"
	CONST_BLOCK_SIZE                     uint64                   = 512
	CONST_CHANNEL_SIZE                   uint64                   = 12000
	Daily                                Frequency                = "daily"
	Weekly                               Frequency                = "weekly"
	Monthly                              Frequency                = "monthly"
)
