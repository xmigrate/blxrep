package utils

import (
	"sync"
	"time"
)

var AppConfiguration AppConfig
var PublicKeyData []byte

// Configuration states
type ConfigState int

const (
	ConfigNotSet ConfigState = iota
	ConfigSetting
	ConfigReady
)

// ConfigManager provides thread-safe access to agent configuration
type ConfigManager struct {
	config      AgentConfig
	mutex       sync.Mutex
	state       ConfigState
	cond        *sync.Cond
	lastUpdated time.Time
}

var agentConfigManager = &ConfigManager{
	state: ConfigNotSet,
}

func init() {
	agentConfigManager.cond = sync.NewCond(&agentConfigManager.mutex)
}

// GetAgentConfiguration returns a copy of the current agent configuration
func GetAgentConfiguration() AgentConfig {
	agentConfigManager.mutex.Lock()
	defer agentConfigManager.mutex.Unlock()
	return agentConfigManager.config
}

// SetAgentConfiguration sets the agent configuration in a thread-safe manner
func SetAgentConfiguration(config AgentConfig) {
	agentConfigManager.mutex.Lock()
	defer agentConfigManager.mutex.Unlock()

	agentConfigManager.config = config
	agentConfigManager.state = ConfigReady
	agentConfigManager.lastUpdated = time.Now()
	agentConfigManager.cond.Broadcast()
}

// WaitForConfiguration waits until configuration is ready or timeout occurs
func WaitForConfiguration(timeout time.Duration) (AgentConfig, bool) {
	agentConfigManager.mutex.Lock()
	defer agentConfigManager.mutex.Unlock()

	if agentConfigManager.state == ConfigReady {
		return agentConfigManager.config, true
	}

	deadline := time.After(timeout)
	go func() {
		<-deadline
		agentConfigManager.cond.Broadcast()
	}()

	agentConfigManager.cond.Wait()

	if agentConfigManager.state == ConfigReady {
		return agentConfigManager.config, true
	}

	return AgentConfig{}, false
}

// IsConfigurationReady returns true if configuration is ready
func IsConfigurationReady() bool {
	agentConfigManager.mutex.Lock()
	defer agentConfigManager.mutex.Unlock()
	return agentConfigManager.state == ConfigReady
}

// ResetConfiguration resets the configuration state (useful for testing or reconnection)
func ResetConfiguration() {
	agentConfigManager.mutex.Lock()
	defer agentConfigManager.mutex.Unlock()

	agentConfigManager.state = ConfigNotSet
	agentConfigManager.config = AgentConfig{}
}

// GetConfigurationHash returns a hash of the current configuration for change detection
func GetConfigurationHash() string {
	agentConfigManager.mutex.Lock()
	defer agentConfigManager.mutex.Unlock()

	// Create a simple hash based on key configuration fields
	config := agentConfigManager.config
	if len(config.Disks) == 0 && config.SnapshotTime == "" {
		return "empty"
	}

	// Create hash from disks and snapshot time
	hash := ""
	for _, disk := range config.Disks {
		hash += disk + "|"
	}
	hash += config.SnapshotTime + "|" + config.SnapshotFreq

	return hash
}

// HasConfigurationChanged returns true if the configuration hash has changed
func HasConfigurationChanged(previousHash string) bool {
	currentHash := GetConfigurationHash()
	return previousHash != currentHash
}

// Legacy variable for backward compatibility - marked as deprecated
// TODO: Remove this variable after updating all references to use ConfigManager
var AgentConfiguration AgentConfig // Deprecated: Use GetAgentConfiguration() instead
