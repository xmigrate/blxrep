package utils

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func MountImage(imagePath, mountDir string) (bool, error) {
	// Step 1: Create a loopback device
	loopDev, err := createLoopbackDevice(imagePath)
	if err != nil {
		return false, fmt.Errorf("failed to create loopback device: %v", err)
	}
	defer func() {
		if err != nil {
			// If an error occurred, try to clean up the loopback device
			exec.Command("losetup", "-d", loopDev).Run()
		}
	}()

	// Step 2: Mount the loopback device
	err = mountLoopbackDevice(loopDev, mountDir)
	if err != nil {
		exec.Command("losetup", "-d", loopDev).Run()
		return false, fmt.Errorf("failed to mount loopback device: %v", err)
	}

	fmt.Printf("Successfully mounted %s to %s using loopback device %s\n", imagePath, mountDir, loopDev)
	return true, nil
}

func createLoopbackDevice(imagePath string) (string, error) {
	cmd := exec.Command("losetup", "--partscan", "--find", "--show", imagePath)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to create loopback device: %v", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func mountLoopbackDevice(loopDev, mountDir string) error {
	// Ensure the mount directory exists
	if err := os.MkdirAll(mountDir, 0755); err != nil {
		return fmt.Errorf("failed to create mount directory: %v", err)
	}

	cmd := exec.Command("mount", loopDev, mountDir)
	return cmd.Run()
}
