package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/xmigrate/blxrep/pkg/dispatcher"
	"github.com/xmigrate/blxrep/service"
	"github.com/xmigrate/blxrep/utils"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type Partition struct {
	Device string
	Type   string
	Size   string
}

func (t *DispatcherTUI) getPartitions() []Partition {
	return t.partitions
}

func (t *DispatcherTUI) getSelectedPartition() Partition {
	row, _ := t.table.GetSelection()
	return Partition{
		Device: t.table.GetCell(row, 0).Text,
		Type:   t.table.GetCell(row, 1).Text,
		Size:   t.table.GetCell(row, 2).Text,
	}
}

func (t *DispatcherTUI) restorePartition() {
	// Implement the restore functionality
	imagePath, err := dispatcher.CheckpointMerge(t.selectedCheckpoint.Filename, t.dataDir, t.currentAgentID, t.selectedDisk)
	if err != nil {
		t.showError(fmt.Sprintf("Error checking if checkpoint is mounted: %v", err))
		return
	}

	// Show loading message
	loadingModal := tview.NewModal().
		SetText("Creating loopback device and identifying partitions...").
		AddButtons([]string{"Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Cancel" {
				t.app.Stop()
			}
		})
	t.app.SetRoot(loadingModal, true)

	// Use a channel to signal when the operation is complete
	done := make(chan struct{})

	go func() {
		defer close(done)

		// Step 1: Create a loopback device
		loopDev, err := t.createLoopbackDevice(imagePath)
		if err != nil {
			t.app.QueueUpdateDraw(func() {
				t.showError(fmt.Sprintf("Failed to create loopback device: %v", err))
			})
			return
		}
		time.Sleep(2 * time.Second)
		t.loopDev = loopDev

		// Step 2: Identify partitions
		err = t.identifyPartitions(loopDev)
		if err != nil {
			t.app.QueueUpdateDraw(func() {
				t.showError(fmt.Sprintf("Failed to identify partitions: %v", err))
			})
			return
		}

		if len(t.partitions) == 0 {
			t.app.QueueUpdateDraw(func() {
				t.showError(fmt.Sprintf("No partitions found on the device %s", loopDev))
			})
			return
		}
	}()

	// Wait for the operation to complete
	go func() {
		<-done
		t.app.QueueUpdateDraw(func() {
			t.app.SetRoot(t.mainFlex, true)
			// Step 3: Ask user to select a partition to restore
			t.selectPartitionRestore()
		})
	}()
}

func (t *DispatcherTUI) createLoopbackDevice(imagePath string) (string, error) {
	cmd := exec.Command("losetup", "--partscan", "--find", "--show", imagePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create loopback device: %v, output: %s", err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

func (t *DispatcherTUI) identifyPartitions(loopDev string) error {
	cmd := exec.Command("lsblk", "-nlpo", "NAME,TYPE,SIZE", loopDev)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to list partitions: %v, output: %s", err, string(output))
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	t.partitions = []Partition{} // Clear existing partitions
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[1] == "part" {
			t.partitions = append(t.partitions, Partition{
				Device: fields[0],
				Type:   fields[1],
				Size:   fields[2],
			})
		}
	}
	return nil
}

func (t *DispatcherTUI) selectPartitionTUI() {
	t.viewState = viewPartitions

	t.table.Clear()
	t.table.SetBorders(false)
	t.table.SetTitle("<Select a partition to mount>")

	t.table.SetCell(0, 0, tview.NewTableCell("Device").SetTextColor(tcell.ColorYellow).SetSelectable(false))
	t.table.SetCell(0, 1, tview.NewTableCell("Size").SetTextColor(tcell.ColorYellow).SetSelectable(false))

	for i, part := range t.partitions {
		t.table.SetCell(i+1, 0, tview.NewTableCell(part.Device))
		t.table.SetCell(i+1, 1, tview.NewTableCell(part.Size))
	}

	t.table.Select(1, 0).SetFixed(1, 0)

	t.content.Clear()
	t.content.AddItem(t.table, 0, 1, true)
	t.app.SetFocus(t.table)
}

func (t *DispatcherTUI) selectPartitionRestore() {
	t.viewState = viewPartitions
	t.updateInfoBar([]string{
		"[green]<ctrl-r>[white] Restore",
		"[green]<enter>[white] View/Browse",
		"[green]<esc>[white] Back",
	})
	t.table.Clear()
	t.table.SetBorders(false)
	t.table.SetTitle("<Select a partition to restore>").SetBorderColor(tcell.ColorGreen)

	t.table.SetCell(0, 0, tview.NewTableCell("Device").SetTextColor(tcell.ColorYellow).SetSelectable(false))
	t.table.SetCell(0, 1, tview.NewTableCell("Size").SetTextColor(tcell.ColorYellow).SetSelectable(false))

	for i, part := range t.partitions {
		t.table.SetCell(i+1, 0, tview.NewTableCell(part.Device))
		t.table.SetCell(i+1, 1, tview.NewTableCell(part.Size))
	}

	t.table.Select(1, 0).SetFixed(1, 0)

	t.content.Clear()
	t.content.AddItem(t.table, 0, 1, true)
	t.app.SetFocus(t.table)

	t.table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlR {
			row, _ := t.table.GetSelection()
			if row > 0 && row <= len(t.partitions) {
				selectedPartition := t.partitions[row-1]
				t.showRestoreDialog(selectedPartition.Device)
				return nil
			}
		}
		return event
	})
}

func (t *DispatcherTUI) showRestoreDialog(sourcePartition string) {
	t.isRestoreFormActive = true

	form := tview.NewForm()

	form.AddInputField("Source Partition", sourcePartition, 0, nil, nil)
	form.AddInputField("Destination Partition", sourcePartition, 0, nil, nil)

	form.AddButton("Restore", func() {
		sourceInput := form.GetFormItemByLabel("Source Partition").(*tview.InputField)
		destInput := form.GetFormItemByLabel("Destination Partition").(*tview.InputField)
		source := sourceInput.GetText()
		dest := destInput.GetText()
		t.showPartitionRestoreConfirmation(source, dest)
	})

	form.AddButton("Cancel", func() {
		t.isRestoreFormActive = false
		t.app.SetRoot(t.mainFlex, true)
	})

	form.SetBorder(true).SetTitle("<Restore Partition>").SetBorderColor(tcell.ColorGreen)

	// Disable editing of the source partition field
	sourceField := form.GetFormItemByLabel("Source Partition").(*tview.InputField)
	sourceField.SetDisabled(true)

	// Set custom input capture for the form
	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			t.isRestoreFormActive = false
			t.app.SetRoot(t.mainFlex, true)
			return nil
		}
		// For all other keys, including Enter, let the form handle them
		return event
	})

	t.app.SetRoot(form, true)
}

func (t *DispatcherTUI) showPartitionRestoreConfirmation(sourcePath, destPath string) {
	modal := tview.NewModal().
		SetText(fmt.Sprintf("Are you sure you want to restore?\nFrom: %s\nTo: %s", sourcePath, destPath)).
		AddButtons([]string{"Restore", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Restore" {
				actionId, err := t.createPartitionRestoreAction(sourcePath, destPath)
				if err != nil {
					t.showError(fmt.Sprintf("Failed to create restore action: %v", err))
				} else {
					t.showPartitionRestoreProgress(actionId)
				}
			} else {
				t.isRestoreFormActive = false
				t.app.SetRoot(t.mainFlex, true)
			}
		})

	t.app.SetRoot(modal, true)
}

func (t *DispatcherTUI) showPartitionRestoreProgress(actionId string) {
	progressText := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText("Status: [yellow]Starting[white]\nProgress: [yellow]0%[white]")

	progressBar := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)

	progressFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(tview.NewTextView().SetText("Partition Restore in progress...").SetTextAlign(tview.AlignCenter), 0, 1, false).
		AddItem(progressText, 0, 1, false).
		AddItem(progressBar, 1, 1, false)

	progressFlex.SetBorder(true).SetTitle("Partition Restore Progress")

	t.app.SetRoot(progressFlex, true)

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				action, err := service.GetAction(actionId)
				if err != nil {
					t.app.QueueUpdateDraw(func() {
						progressText.SetText(fmt.Sprintf("Error: %v", err))
					})
					return
				}

				t.app.QueueUpdateDraw(func() {
					status := action.ActionStatus
					progress := action.ActionProgress

					statusColor := "yellow"
					if status == string(utils.CONST_ACTION_STATUS_COMPLETED) {
						statusColor = "green"
					} else if status == string(utils.CONST_ACTION_STATUS_FAILED) {
						statusColor = "red"
					}

					progressText.SetText(fmt.Sprintf("Status: [%s]%s[white]\nProgress: [%s]%d%%[white]", statusColor, status, statusColor, progress))

					_, _, width, _ := progressFlex.GetInnerRect()
					progressBarWidth := width
					completedWidth := int(float64(progress) / 100 * float64(progressBarWidth))
					progressBar.SetText(fmt.Sprintf("[green]%s[white]%s",
						strings.Repeat("█", completedWidth),
						strings.Repeat("░", progressBarWidth-completedWidth)))

					if status == string(utils.CONST_ACTION_STATUS_COMPLETED) || status == string(utils.CONST_ACTION_STATUS_FAILED) {
						time.Sleep(2 * time.Second)
						t.isRestoreFormActive = false
						t.app.SetRoot(t.mainFlex, true)
						return
					}
				})
			}
		}
	}()
}

func (t *DispatcherTUI) createPartitionRestoreAction(sourcePath, destPath string) (string, error) {
	action := utils.Action{
		Id:             utils.GenerateUUID(),
		AgentId:        t.currentAgentID,
		Action:         string(utils.CONST_AGENT_ACTION_PARTITION_RESTORE),
		ActionType:     string(utils.CONST_AGENT_ACTION_RESTORE),
		ActionStatus:   string(utils.CONST_ACTION_STATUS_WAITING),
		SourceFilePath: sourcePath,
		TargetFilePath: destPath,
		TimeCreated:    utils.NewUTCTime(time.Now()),
	}

	err := service.InsertOrUpdateAction(action)
	if err != nil {
		return "", err
	}

	return action.Id, nil
}

func (t *DispatcherTUI) mountSelectedPartition(partition Partition) {
	mountDir := filepath.Join(t.dataDir, t.currentAgentID, "mount")
	t.mountDir = mountDir
	if err := os.MkdirAll(mountDir, 0755); err != nil {
		t.showError(fmt.Sprintf("Failed to create mount directory: %v", err))
		return
	}
	err := t.mountPartition(partition.Device, mountDir)
	if err != nil {
		t.showError(fmt.Sprintf("Failed to mount partition: %v", err))
		return
	}
	t.currentDir = mountDir
	t.showFileBrowser(mountDir)
}

func (t *DispatcherTUI) mountPartition(partitionDevice, mountDir string) error {
	cmd := exec.Command("mount", partitionDevice, mountDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to mount partition: %v, output: %s", err, string(output))
	}
	return nil
}

func (t *DispatcherTUI) browseCheckpoint() {
	imagePath, err := dispatcher.CheckpointMerge(t.selectedCheckpoint.Filename, t.dataDir, t.currentAgentID, t.selectedDisk)
	if err != nil {
		t.showError(fmt.Sprintf("Error checking if checkpoint is mounted: %v", err))
		return
	}

	// Show loading message
	loadingModal := tview.NewModal().
		SetText("Creating loopback device and identifying partitions...").
		AddButtons([]string{"Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Cancel" {
				t.app.Stop()
			}
		})
	t.app.SetRoot(loadingModal, true)

	// Use a channel to signal when the operation is complete
	done := make(chan struct{})

	go func() {
		defer close(done)

		// Step 1: Create a loopback device
		loopDev, err := t.createLoopbackDevice(imagePath)
		if err != nil {
			t.app.QueueUpdateDraw(func() {
				t.showError(fmt.Sprintf("Failed to create loopback device: %v", err))
			})
			return
		}
		time.Sleep(2 * time.Second)
		t.loopDev = loopDev

		// Step 2: Identify partitions
		err = t.identifyPartitions(loopDev)
		if err != nil {
			t.app.QueueUpdateDraw(func() {
				t.showError(fmt.Sprintf("Failed to identify partitions: %v", err))
			})
			return
		}

		if len(t.partitions) == 0 {
			t.app.QueueUpdateDraw(func() {
				t.showError(fmt.Sprintf("No partitions found on the device %s", loopDev))
			})
			return
		}
	}()

	// Wait for the operation to complete
	go func() {
		<-done
		t.app.QueueUpdateDraw(func() {
			t.app.SetRoot(t.mainFlex, true)
			// Step 3: Ask user to select a partition
			t.selectPartitionTUI()
		})
	}()
}
