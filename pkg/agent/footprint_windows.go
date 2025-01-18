//go:build windows
// +build windows

package agent

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"blxrep/utils"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/mem"
)

func Footprint() (*utils.VMInfo, error) {

	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	cpuInfo, err := cpu.Info()
	if err != nil {
		return nil, err
	}

	cpuCores := len(cpuInfo)

	memInfo, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}

	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var InterfaceInfo []struct {
		Name         string `json:"name"`
		IPAddress    string `json:"ip_address"`
		SubnetMask   string `json:"subnet_mask"`
		CIDRNotation string `json:"cidr_notation"`
		NetworkCIDR  string `json:"network_cidr"`
	}

	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			fmt.Println(err)
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip4 := ipNet.IP.To4()
			if ip4 == nil {
				continue
			}

			mask := ipNet.Mask
			networkIP := net.IP(make([]byte, 4))
			for i := range ip4 {
				networkIP[i] = ip4[i] & mask[i]
			}

			cidr, _ := ipNet.Mask.Size()

			InterfaceInfo = append(InterfaceInfo, struct {
				Name         string `json:"name"`
				IPAddress    string `json:"ip_address"`
				SubnetMask   string `json:"subnet_mask"`
				CIDRNotation string `json:"cidr_notation"`
				NetworkCIDR  string `json:"network_cidr"`
			}{
				Name:         iface.Name,
				IPAddress:    ip4.String(),
				SubnetMask:   net.IP(mask).String(),
				CIDRNotation: fmt.Sprintf("%s/%d", ip4, cidr),
				NetworkCIDR:  fmt.Sprintf("%s/%d", networkIP, cidr),
			})
		}
	}

	diskDetails, err := getDiskDetails()
	if err != nil {
		return nil, err
	}

	osVersion, err := host.Info()
	if err != nil {
		return nil, err
	}

	vmInfo := utils.VMInfo{
		Hostname:      hostname,
		CpuModel:      cpuInfo[0].ModelName,
		CpuCores:      cpuCores,
		Ram:           memInfo.Total,
		InterfaceInfo: InterfaceInfo,
		DiskDetails:   diskDetails,
		OsDistro:      osVersion.Platform,
		OsVersion:     osVersion.PlatformVersion,
	}
	utils.LogDebug(fmt.Sprintf("Footprint %s", vmInfo))
	return &vmInfo, nil
}

func getDiskDetails() ([]utils.DiskDetailsStruct, error) {
	physicalDrives, err := getPhysicalDrives()
	if err != nil {
		return nil, err
	}

	cDriveDiskIndex, err := getCDriveDiskIndex()
	if err != nil {
		return nil, err
	}

	var diskDetails []utils.DiskDetailsStruct
	for _, drive := range physicalDrives {
		size := drive.BytesPerSector * drive.TotalSectors
		mountPoint := "/data"
		if drive.Index == cDriveDiskIndex {
			mountPoint = "/"
		}

		diskDetails = append(diskDetails, utils.DiskDetailsStruct{
			FsType:     "NTFS",
			Size:       size,
			Uuid:       "", // UUID not retrieved in this approach
			Name:       drive.DeviceID,
			MountPoint: mountPoint,
		})
	}

	return diskDetails, nil
}

type PhysicalDrive struct {
	DeviceID       string
	BytesPerSector uint64
	Partitions     int
	TotalSectors   uint64
	Index          int
}

func getPhysicalDrives() ([]PhysicalDrive, error) {
	cmd := exec.Command("wmic", "diskdrive", "get", "DeviceID,BytesPerSector,Partitions,TotalSectors,Index")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}

	var drives []PhysicalDrive
	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		utils.LogDebug(fmt.Sprintf("line physical drive %s", line))
		if i == 0 || strings.TrimSpace(line) == "" { // Skip the header and empty lines
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		deviceID := fields[1]
		bytesPerSector, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			utils.LogError(fmt.Sprintf("Error parsing BytesPerSector for %s: %v", deviceID, err))
			continue
		}
		partitions, err := strconv.Atoi(fields[3])
		if err != nil {
			utils.LogError(fmt.Sprintf("Error parsing Partitions for %s: %v", deviceID, err))
			continue
		}
		totalSectors, err := strconv.ParseUint(fields[4], 10, 64)
		if err != nil {
			utils.LogError(fmt.Sprintf("Error parsing TotalSectors for %s: %v", deviceID, err))
			continue
		}
		index, err := strconv.Atoi(fields[2])
		if err != nil {
			utils.LogError(fmt.Sprintf("Error parsing Index for %s: %v", deviceID, err))
			continue
		}

		drives = append(drives, PhysicalDrive{
			DeviceID:       deviceID,
			BytesPerSector: bytesPerSector,
			Partitions:     partitions,
			TotalSectors:   totalSectors,
			Index:          index,
		})
	}

	return drives, nil
}

type Partition struct {
	DeviceID  string
	DiskIndex int
}

func getPartitions() ([]Partition, error) {
	cmd := exec.Command("wmic", "partition", "get", "DeviceID,DiskIndex")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}

	var partitions []Partition
	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		utils.LogDebug(fmt.Sprintf("line partitions %s", line))
		if i == 0 || strings.TrimSpace(line) == "" { // Skip the header and empty lines
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		deviceID := fields[0]
		diskIndex, err := strconv.Atoi(fields[1])
		if err != nil {
			utils.LogError(fmt.Sprintf("Error parsing DiskIndex for %s: %v", deviceID, err))
			continue
		}

		partitions = append(partitions, Partition{
			DeviceID:  deviceID,
			DiskIndex: diskIndex,
		})
	}

	return partitions, nil
}

func getCDriveDiskIndex() (int, error) {
	cmd := exec.Command("wmic", "path", "Win32_LogicalDiskToPartition", "get", "Antecedent,Dependent")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return -1, err
	}

	lines := strings.Split(string(output), "\n")
	re := regexp.MustCompile(`Win32_DiskPartition\.DeviceID="([^"]+)"`)
	for i, line := range lines {
		utils.LogDebug(fmt.Sprintf("line c drive disk index: %s", line))
		if i == 0 || strings.TrimSpace(line) == "" { // Skip the header and empty lines
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			utils.LogError(fmt.Sprintf("Unexpected output from wmic: %s length: %d", line, len(parts)))
			continue
		}

		// Combine parts for parsing
		combinedParts := strings.Join(parts, " ")
		if strings.Contains(combinedParts, "C:") {
			match := re.FindStringSubmatch(combinedParts)
			if len(match) > 1 {
				partitionID := match[1]
				diskIndex, err := getDiskIndexFromPartition(partitionID)
				if err != nil {
					return -1, err
				}
				return diskIndex, nil
			}
		}
	}
	return -1, fmt.Errorf("C: drive not found")
}

func getDiskIndexFromPartition(partitionID string) (int, error) {
	// Construct the command string
	command := fmt.Sprintf("wmic partition where (DeviceID='%s') get DiskIndex", partitionID)
	utils.LogDebug(fmt.Sprintf("Executing command: %s", command))

	// Execute the command
	cmd := exec.Command("cmd", "/C", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		utils.LogError(fmt.Sprintf("Error getting DiskIndex for partition %s", partitionID))
		utils.LogDebug(fmt.Sprintf("Command output: %s", string(output)))
		utils.LogError(fmt.Sprintf("Error message: %s", err.Error()))
		return -1, err
	}

	// Log the output for debugging
	utils.LogDebug(fmt.Sprintf("Command output: %s", string(output)))

	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		utils.LogDebug(fmt.Sprintf("line disk index from partition: %s", line))
		if i == 0 || strings.TrimSpace(line) == "" { // Skip the header and empty lines
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 1 {
			return strconv.Atoi(fields[0])
		}
	}

	return -1, fmt.Errorf("DiskIndex not found for partition: %s", partitionID)
}
