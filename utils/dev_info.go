package utils

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	BLKGETSIZE64 = 0x80081272
)

func GetTotalSectors(devicePath string) (uint64, error) {
	file, err := os.Open(devicePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open device %s: %v", devicePath, err)
	}
	defer file.Close()

	var size uint64
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), BLKGETSIZE64, uintptr(unsafe.Pointer(&size)))
	if errno != 0 {
		return 0, fmt.Errorf("ioctl error: %v", errno)
	}

	// Assuming 512-byte sectors, which is common
	sectorSize := uint64(512)
	totalSectors := size / sectorSize

	return totalSectors, nil
}
