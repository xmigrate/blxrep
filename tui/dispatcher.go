package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xmigrate/blxrep/utils"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type DispatcherTUI struct {
	app                 *tview.Application
	mainFlex            *tview.Flex
	infoBarLeft         *tview.TextView
	infoBarRight        *tview.TextView
	content             *tview.Flex
	cmdInput            *tview.InputField
	dataDir             string
	agents              map[string]utils.Agent
	viewState           viewState
	table               *tview.Table
	selectedCheckpoint  *utils.Checkpoint
	selectedDisk        string
	currentAgentID      string
	currentDir          string
	partitions          []Partition
	loopDev             string
	mountDir            string
	isRestoreFormActive bool
	tableMutex          sync.RWMutex
}

type viewState int

const (
	viewAgents viewState = iota
	viewCheckpoints
	viewCheckpointOptions
	viewPartitions
	viewFileBrowser
	viewRestoreOptions
	viewActions
	viewDisks
	viewDiskOptions
)

func RunDispatcherTUI(dataDir string) {
	utils.AppConfiguration.DataDir = dataDir
	tui := &DispatcherTUI{
		app:       tview.NewApplication(),
		dataDir:   dataDir,
		viewState: viewAgents,
	}

	tui.setup()

	if err := tui.app.Run(); err != nil {
		panic(err)
	}
}

func (t *DispatcherTUI) setup() {
	dataDir := fmt.Sprintf("Data Dir: %s", t.dataDir)
	banner := utils.GetDiskBanner()
	infoText := fmt.Sprintf("%s \n %s", banner, dataDir) // Adjust 50 as needed

	t.infoBarLeft = tview.NewTextView().
		SetTextColor(tcell.ColorPurple).
		SetDynamicColors(true).
		SetRegions(true).
		SetWrap(false).
		SetText(infoText)
	t.infoBarLeft.SetBorder(false)

	t.infoBarRight = tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetWrap(false).
		SetTextAlign(tview.AlignLeft)
	t.infoBarRight.SetBorder(false)
	infoBarFlex := tview.NewFlex().
		AddItem(t.infoBarLeft, 0, 4, false).
		AddItem(t.infoBarRight, 0, 1, false)

	t.content = tview.NewFlex().SetDirection(tview.FlexColumn)

	t.cmdInput = tview.NewInputField().
		SetLabel(" Command: ").
		SetFieldWidth(0).
		SetDoneFunc(t.handleCommand).SetFieldBackgroundColor(tcell.ColorBlack).SetLabelColor(tcell.ColorWhite)

	t.mainFlex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(infoBarFlex, 8, 1, false).
		AddItem(t.content, 0, 1, true)

	t.app.SetRoot(t.mainFlex, true)

	t.app.SetInputCapture(t.globalInputHandler)

	t.showAgents()
}

func (t *DispatcherTUI) updateInfoBar(shortcuts []string) {
	banner := utils.GetDiskBanner()
	dataDir := fmt.Sprintf("[orange]Data Dir:[white] %s", t.dataDir)

	// Update left column
	leftText := fmt.Sprintf("%s\n%s", banner, dataDir)
	t.infoBarLeft.SetText(leftText)

	// Update right column
	rightText := strings.Join(shortcuts, "\n")
	t.infoBarRight.SetText(rightText)
}

func (t *DispatcherTUI) globalInputHandler(event *tcell.EventKey) *tcell.EventKey {
	if t.isRestoreFormActive {
		return event
	}
	switch event.Key() {
	case tcell.KeyRune:
		switch event.Rune() {
		case ':':
			t.showCommandInput()
			return nil
		case 'q', 'Q':
			t.app.Stop()
			return nil

		case 'a', 'A':
			if t.viewState == viewAgents {
				t.showActions()
				return nil
			}
		case 'p', 'P':
			if t.viewState == viewActions {
				row, _ := t.table.GetSelection()
				agentID := t.table.GetCell(row, 0).Text
				actionID := t.table.GetCell(row, 1).Text
				actionStatus := t.table.GetCell(row, 3).Text
				t.pauseSelectedAction(agentID, actionID, actionStatus)
				return nil
			}
		case 'r', 'R':
			if t.viewState == viewActions {
				row, _ := t.table.GetSelection()
				agentID := t.table.GetCell(row, 0).Text
				actionID := t.table.GetCell(row, 1).Text
				actionStatus := t.table.GetCell(row, 3).Text
				t.resumeSelectedAction(agentID, actionID, actionStatus)
				return nil
			}
		}
	case tcell.KeyEscape:
		switch t.viewState {
		case viewActions:
			t.showAgents()
			return nil
		case viewCheckpoints:
			t.showDisks(t.currentAgentID)
			return nil
		case viewDisks:
			t.showAgents()
			return nil
		case viewDiskOptions:
			t.showDisks(t.currentAgentID)
			return nil
		case viewCheckpointOptions:
			t.showCheckpoints(t.currentAgentID, t.selectedDisk)
			return nil
		case viewPartitions:
			exec.Command("losetup", "-d", t.loopDev).Run()
			t.showCheckpointOptions()
			return nil
		case viewFileBrowser:
			// Go back to partition selection when in file browser
			exec.Command("umount", t.mountDir).Run()
			t.selectPartitionTUI()
			return nil
		case viewRestoreOptions:
			t.showCheckpointOptions()
			return nil

		}
	case tcell.KeyEnter:
		switch t.viewState {
		case viewAgents:
			t.showCheckpointsForSelectedAgent()
			return nil
		case viewCheckpoints:
			t.selectCheckpoint()
			return nil
		case viewDisks:
			t.selectDisk()
			return nil
		case viewDiskOptions:
			t.selectOption()
			return nil
		case viewCheckpointOptions:
			t.selectOption()
			return nil
		case viewPartitions:
			row, _ := t.table.GetSelection()
			if row > 0 && row <= len(t.partitions) {
				selectedPartition := t.partitions[row-1]
				t.mountSelectedPartition(selectedPartition)
			}
			return nil
		case viewFileBrowser:
			table := t.content.GetItem(0).(*tview.Table)
			row, _ := table.GetSelection()
			if row == 1 {
				// Go to parent directory
				parentDir := filepath.Dir(t.currentDir)
				if parentDir != t.currentDir {
					t.updateFileTable(table, parentDir)
					t.currentDir = parentDir
				}
			} else if row > 1 {
				cellContent := table.GetCell(row, 0).Text
				fileName := strings.TrimPrefix(cellContent, "[::b]") // Remove bold formatting if present
				filePath := filepath.Join(t.currentDir, fileName)
				fileInfo, err := os.Stat(filePath)
				if err != nil {
					t.showError(fmt.Sprintf("Error accessing file: %v", err))
					return nil
				}
				if fileInfo.IsDir() {
					t.updateFileTable(table, filePath)
					t.currentDir = filePath
				} else {
					// You can add file viewing functionality here if needed
					t.showMessage(fmt.Sprintf("Selected file: %s", fileName))
				}
			}
			return nil
		case viewRestoreOptions:
			row, _ := t.table.GetSelection()
			switch row {
			case 1:
				t.restorePartition()
			case 2:
				t.restoreDisk()
			}
			return nil
		}
	}
	return event
}

func (t *DispatcherTUI) restoreDisk() {
	// Implement full disk restoration logic here
	t.showMessage("Restoring full disk... (Not yet implemented)")
}

func (t *DispatcherTUI) showCommandInput() {
	t.mainFlex.RemoveItem(t.content)
	t.mainFlex.AddItem(t.cmdInput, 1, 1, true)
	t.mainFlex.AddItem(t.content, 0, 1, false)
	t.app.SetFocus(t.cmdInput)
}

func (t *DispatcherTUI) hideCommandInput() {
	t.mainFlex.RemoveItem(t.cmdInput)
	t.mainFlex.RemoveItem(t.content)
	t.mainFlex.AddItem(t.content, 0, 1, true)
	t.app.SetFocus(t.content)
}

func (t *DispatcherTUI) handleCommand(key tcell.Key) {
	if key != tcell.KeyEnter {
		return
	}

	cmd := strings.TrimSpace(t.cmdInput.GetText())
	t.cmdInput.SetText("")
	t.hideCommandInput()

	switch cmd {
	case "refresh":
		t.showAgents()
	default:
		t.showError(fmt.Sprintf("Unknown command: %s", cmd))
	}
}

func (t *DispatcherTUI) showError(message string) {
	t.table.Clear()
	t.table.SetBorders(false)
	t.table.SetTitle("Error")

	// Split the message into words
	words := strings.Fields(message)
	lines := []string{}
	currentLine := ""

	// Create lines with a maximum width of 80 characters
	for _, word := range words {
		if len(currentLine)+len(word)+1 > 80 {
			lines = append(lines, strings.TrimSpace(currentLine))
			currentLine = word
		} else {
			if currentLine != "" {
				currentLine += " "
			}
			currentLine += word
		}
	}
	if currentLine != "" {
		lines = append(lines, strings.TrimSpace(currentLine))
	}

	// Add each line to the table
	for i, line := range lines {
		t.table.SetCell(i, 0, tview.NewTableCell(line).SetTextColor(tcell.ColorRed))
	}

	t.app.SetFocus(t.table)
}

func (t *DispatcherTUI) showMessage(message string) {
	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			t.app.SetRoot(t.mainFlex, true)
		})

	t.app.SetRoot(modal, true)
}
