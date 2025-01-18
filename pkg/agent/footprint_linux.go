//go:build linux
// +build linux

package agent

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/xmigrate/blxrep/utils"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
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

	// var ipAddress, network, subnet string
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

	partitions, err := disk.Partitions(true)
	if err != nil {
		return nil, err
	}

	var diskDetails []utils.DiskDetailsStruct
	for _, partition := range partitions {
		mountpoint := partition.Mountpoint
		if !strings.HasPrefix(mountpoint, "/var/") && !strings.HasPrefix(mountpoint, "/run/") {
			usage, err := disk.Usage(mountpoint)
			if err == nil {
				fsType := partition.Fstype
				if fsType == "xfs" || strings.HasPrefix(fsType, "ext") {
					partitionName := partition.Device
					diskName := strings.TrimRightFunc(partitionName, func(r rune) bool {
						return '0' <= r && r <= '9'
					})

					if strings.Contains(diskName, "nvme") {
						diskName = strings.TrimRightFunc(diskName, func(r rune) bool {
							return 'p' <= r
						})
					}

					cmd := exec.Command("sudo", "blkid", partitionName)
					output, err := cmd.CombinedOutput()
					if err != nil {
						return nil, err
					}

					re := regexp.MustCompile(`\b(?i)UUID="([a-f0-9-]+)"`)
					match := re.FindStringSubmatch(string(output))
					diskDetails = append(diskDetails,
						utils.DiskDetailsStruct{
							FsType:     fsType,
							Size:       usage.Total,
							Uuid:       match[1],
							Name:       diskName,
							MountPoint: mountpoint,
						})
				}
			}
		}
	}

	content, err := ioutil.ReadFile("/etc/os-release")
	if err != nil {
		return nil, err
	}

	osRelease := string(content)
	distro := getValue(osRelease, "ID")
	majorVersion := getValue(osRelease, "VERSION_ID")

	vmInfo := utils.VMInfo{
		Hostname:      hostname,
		CpuModel:      cpuInfo[0].ModelName,
		CpuCores:      cpuCores,
		Ram:           memInfo.Total,
		InterfaceInfo: InterfaceInfo,
		DiskDetails:   diskDetails,
		OsDistro:      distro,
		OsVersion:     majorVersion,
	}

	return &vmInfo, nil
}

func getValue(osRelease, key string) string {
	lines := strings.Split(osRelease, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, key+"=") {
			return strings.Trim(strings.TrimPrefix(line, key+"="), `"`)
		}
	}
	return ""
}
