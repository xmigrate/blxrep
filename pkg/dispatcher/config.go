package dispatcher

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/xmigrate/blxrep/service"
	"github.com/xmigrate/blxrep/utils"
	"gopkg.in/yaml.v3"
)

func Contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// ConfigScheduler reads backup policies from files and updates agent configurations
func ConfigScheduler(policyDir string) error {
	// Get all agents from DB as Map
	agentMap, err := service.GetAllAgentsMap(-1)
	if err != nil {
		return fmt.Errorf("failed to get agents from DB: %w", err)
	}
	// Walk through all YAML files in the policy directory
	err = filepath.WalkDir(policyDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip if not a YAML file
		if !d.IsDir() && (filepath.Ext(path) == ".yaml" || filepath.Ext(path) == ".yml") {
			if err := processBackupPolicy(path, agentMap); err != nil {
				return fmt.Errorf("failed to process policy file %s: %w", path, err)
			}
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk policy directory: %w", err)
	}

	// Update agents in the database
	if err := service.InsertOrUpdateAgentsMap(agentMap); err != nil {
		return fmt.Errorf("failed to update agents in DB: %w", err)
	}

	return nil
}

func processBackupPolicy(filePath string, agentMap map[string]utils.Agent) error {
	// Read policy file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read policy file: %w", err)
	}

	// Parse YAML
	var policy utils.BackupPolicy
	if err := yaml.Unmarshal(data, &policy); err != nil {
		return fmt.Errorf("failed to parse policy file: %w", err)
	}
	utils.LogDebug(fmt.Sprintf("Processed policy: %s", filePath))
	// Process each target group in the policy
	for _, targetGroup := range policy.Targets {
		// Expand the pattern to get all matching hostnames
		hostnames := expandPattern(targetGroup.Pattern, agentMap)
		// Update configuration for all matching agents
		for _, hostname := range hostnames {
			if agent, exists := agentMap[hostname]; exists {
				updateAgentConfig(&agent, policy, targetGroup)
				agentMap[hostname] = agent
			}
		}
	}

	return nil
}

// expandPattern expands patterns like "web[1-3].example.com" to ["web1.example.com", "web2.example.com", "web3.example.com"]
func expandPattern(pattern string, agentMap map[string]utils.Agent) []string {
	// First check if pattern contains range expression
	rangeRegex := regexp.MustCompile(`\[(\d+)-(\d+)\]`)
	matches := rangeRegex.FindStringSubmatch(pattern)

	if len(matches) == 3 {
		// We found a range expression
		start, _ := strconv.Atoi(matches[1])
		end, _ := strconv.Atoi(matches[2])

		var result []string
		prefix := pattern[:strings.Index(pattern, "[")]
		suffix := pattern[strings.Index(pattern, "]")+1:]

		// Generate all hostnames in the range
		for i := start; i <= end; i++ {
			hostname := fmt.Sprintf("%s%d%s", prefix, i, suffix)
			result = append(result, hostname)
		}
		return result
	}

	// Handle comma-separated lists [1,2,3]
	listRegex := regexp.MustCompile(`\[([^\]]+)\]`)
	matches = listRegex.FindStringSubmatch(pattern)

	if len(matches) == 2 {
		items := strings.Split(matches[1], ",")
		var result []string
		prefix := pattern[:strings.Index(pattern, "[")]
		suffix := pattern[strings.Index(pattern, "]")+1:]

		for _, item := range items {
			hostname := fmt.Sprintf("%s%s%s", prefix, strings.TrimSpace(item), suffix)
			result = append(result, hostname)
		}
		return result
	}

	// Handle wildcards (* and ?)
	if strings.ContainsAny(pattern, "*?") {
		// Convert glob pattern to regex for matching
		regexPattern := strings.ReplaceAll(pattern, ".", "\\.")
		regexPattern = strings.ReplaceAll(regexPattern, "*", ".*")
		regexPattern = strings.ReplaceAll(regexPattern, "?", ".")
		regexPattern = "^" + regexPattern + "$"

		reg, err := regexp.Compile(regexPattern)
		if err != nil {
			utils.LogError(fmt.Sprintf("Invalid pattern %s: %v", pattern, err))
			return []string{pattern}
		}

		var result []string
		// Match against all known hostnames
		for hostname := range agentMap {
			if reg.MatchString(hostname) {
				result = append(result, hostname)
			}
		}
		return result
	}

	// If no special pattern, return as is
	return []string{pattern}
}

func updateAgentConfig(agent *utils.Agent, policy utils.BackupPolicy, targetGroup utils.TargetGroup) {
	agent.CloneSchedule.Frequency = policy.SnapshotFrequency
	agent.CloneSchedule.Time = policy.SnapshotTime
	agent.CloneSchedule.Bandwidth = policy.BandwidthLimit
	agent.Prerequisites = true
	agent.SnapshotRetention = policy.SnapshotRetention
	agent.ArchiveInterval = policy.ArchiveInterval
	agent.LiveSyncFreq = policy.LiveSyncFrequency
	agent.TransitionAfterDays = policy.TransitionAfterDays
	agent.DeleteAfterDays = policy.DeleteAfterDays
	utils.AppConfiguration.ArchiveInterval = policy.ArchiveInterval
	utils.LogDebug(fmt.Sprintf("Updated agent config: %+v", agent.AgentId))

	// Log footprint disk information for debugging
	log.Printf("🔍 AGENT %s - Processing disk selection", agent.AgentId)
	log.Printf("   Footprint contains %d disks:", len(agent.Footprint.DiskDetails))
	for i, disk := range agent.Footprint.DiskDetails {
		log.Printf("   %d. %s - Size: %d, FS: %s, Mount: %s",
			i+1, disk.Name, disk.Size, disk.FsType, disk.MountPoint)
	}

	log.Printf("   Policy excludes %d disks:", len(targetGroup.DisksExcluded))
	for i, excludedDisk := range targetGroup.DisksExcluded {
		log.Printf("   %d. %s", i+1, excludedDisk)
	}

	// Update disk configuration
	originalDiskCount := len(agent.Disks)
	for _, disk := range agent.Footprint.DiskDetails {
		if !Contains(targetGroup.DisksExcluded, disk.Name) {
			if !Contains(agent.Disks, disk.Name) {
				agent.Disks = append(agent.Disks, disk.Name)
				log.Printf("   ✅ ADDED disk: %s", disk.Name)
			} else {
				log.Printf("   ⏭️  SKIP disk (already exists): %s", disk.Name)
			}
		} else {
			log.Printf("   ❌ EXCLUDED disk: %s", disk.Name)
		}
	}

	log.Printf("   Final agent.Disks array: %v", agent.Disks)
	log.Printf("   📊 Disk count: %d → %d (+%d added)",
		originalDiskCount, len(agent.Disks), len(agent.Disks)-originalDiskCount)
}
