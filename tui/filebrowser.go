package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xmigrate/blxrep/service"
	"github.com/xmigrate/blxrep/utils"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func (t *DispatcherTUI) showFileBrowser(rootDir string) {
	t.viewState = viewFileBrowser
	t.updateInfoBar([]string{
		"[green]<ctrl-r>[white] Restore",
		"[green]<enter>[white] View/Browse",
	})
	table := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false)

	table.SetTitle("<File Browser>").
		SetBorder(true).SetBorderColor(tcell.ColorGreen)

	t.updateFileTable(table, rootDir)

	t.content.Clear()
	t.content.AddItem(table, 0, 1, true)
	t.app.SetFocus(table)

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlR {
			row, _ := table.GetSelection()
			if row > 0 { // Ignore header row
				cellContent := table.GetCell(row, 0).Text
				fileName := strings.TrimPrefix(cellContent, "[::b]")
				filePath := filepath.Join(t.currentDir, fileName)
				t.showRestorePrompt(filePath)
				return nil
			}
		}
		return event
	})
}

func (t *DispatcherTUI) showRestorePrompt(sourcePath string) {
	t.isRestoreFormActive = true

	form := tview.NewForm()

	form.AddInputField("Source Path", sourcePath, 0, nil, nil)
	form.AddInputField("Destination Path", sourcePath, 0, nil, nil)

	form.AddButton("Restore", func() {
		sourceInput := form.GetFormItemByLabel("Source Path").(*tview.InputField)
		destInput := form.GetFormItemByLabel("Destination Path").(*tview.InputField)
		source := sourceInput.GetText()
		dest := destInput.GetText()
		t.showRestoreConfirmation(source, dest)
	})

	form.AddButton("Cancel", func() {
		t.isRestoreFormActive = false
		t.app.SetRoot(t.mainFlex, true)
	})

	form.SetBorder(true).SetTitle("Create Restore Action")

	// Set custom input capture for the form
	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			// Exit the form
			t.isRestoreFormActive = false
			t.app.SetRoot(t.mainFlex, true)
			return nil
		}
		// For all other keys, including Enter, let the form handle them
		return event
	})

	t.app.SetRoot(form, true)
}

func (t *DispatcherTUI) showRestoreConfirmation(sourcePath, destPath string) {
	modal := tview.NewModal().
		SetText(fmt.Sprintf("Are you sure you want to restore?\nFrom: %s\nTo: %s", sourcePath, destPath)).
		AddButtons([]string{"Restore", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Restore" {
				utils.LogDebug("Restore button pressed")
				actionId, err := t.createRestoreAction(sourcePath, destPath)
				if err != nil {
					utils.LogError(fmt.Sprintf("Failed to create restore action: %v", err))
					t.showError(fmt.Sprintf("Failed to create restore action: %v", err))
				} else {
					utils.LogDebug(fmt.Sprintf("Restore action created with ID: %s", actionId))
					t.showRestoreProgress(actionId)
				}
			} else {
				utils.LogDebug("Cancel button pressed")
				t.isRestoreFormActive = false
				t.app.SetRoot(t.mainFlex, true)
			}
		})

	t.app.SetRoot(modal, true)
}

func (t *DispatcherTUI) showRestoreProgress(actionId string) {
	utils.LogDebug(fmt.Sprintf("Showing restore progress for action ID: %s", actionId))

	progressText := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText("Status: [yellow]Starting[white]\nProgress: [yellow]0%[white]")

	// Custom progress bar
	progressBar := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)

	progressFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(tview.NewTextView().SetText("Restore in progress...").SetTextAlign(tview.AlignCenter), 0, 1, false).
		AddItem(progressText, 0, 1, false).
		AddItem(progressBar, 1, 1, false)

	progressFlex.SetBorder(true).SetTitle("Restore Progress")

	t.app.SetRoot(progressFlex, true)

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				action, err := service.GetAction(actionId)
				if err != nil {
					utils.LogError(fmt.Sprintf("Error fetching action: %v", err))
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

					// Update custom progress bar
					_, _, width, _ := progressFlex.GetInnerRect()
					progressBarWidth := width
					completedWidth := int(float64(progress) / 100 * float64(progressBarWidth))
					progressBar.SetText(fmt.Sprintf("[green]%s[white]%s",
						strings.Repeat("█", completedWidth),
						strings.Repeat("░", progressBarWidth-completedWidth)))

					if status == string(utils.CONST_ACTION_STATUS_COMPLETED) || status == string(utils.CONST_ACTION_STATUS_FAILED) {
						time.Sleep(2 * time.Second) // Show the final status for 2 seconds
						t.isRestoreFormActive = false
						t.app.SetRoot(t.mainFlex, true)
						return
					}
				})
			}
		}
	}()

}

func (t *DispatcherTUI) createRestoreAction(sourcePath, destPath string) (string, error) {
	action := utils.Action{
		Id:             utils.GenerateUUID(),
		AgentId:        t.currentAgentID,
		Action:         string(utils.CONST_AGENT_ACTION_RESTORE),
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

func (t *DispatcherTUI) updateFileTable(table *tview.Table, dir string) {
	table.Clear()

	table.SetCell(0, 0, tview.NewTableCell("Name").SetTextColor(tcell.ColorYellow).SetSelectable(false))
	table.SetCell(0, 1, tview.NewTableCell("Type").SetTextColor(tcell.ColorYellow).SetSelectable(false))
	table.SetCell(0, 2, tview.NewTableCell("Size").SetTextColor(tcell.ColorYellow).SetSelectable(false))
	table.SetCell(0, 3, tview.NewTableCell("Modified").SetTextColor(tcell.ColorYellow).SetSelectable(false))

	files, err := os.ReadDir(dir)
	if err != nil {
		t.showError(fmt.Sprintf("Error reading directory: %v", err))
		return
	}

	table.SetCell(1, 0, tview.NewTableCell("..").SetTextColor(tcell.ColorDarkCyan))
	table.SetCell(1, 1, tview.NewTableCell("Directory"))
	table.SetCell(1, 2, tview.NewTableCell(""))
	table.SetCell(1, 3, tview.NewTableCell(""))

	row := 2
	for _, file := range files {
		info, err := file.Info()
		if err != nil {
			continue
		}

		name := file.Name()
		fileType := "File"
		size := fmt.Sprintf("%d", info.Size())
		modified := info.ModTime().Format("2006-01-02 15:04:05")

		if file.IsDir() {
			fileType = "Directory"
			size = ""
			name = "[::b]" + name // Make directories bold
		}

		table.SetCell(row, 0, tview.NewTableCell(name).SetTextColor(tcell.ColorWhite))
		table.SetCell(row, 1, tview.NewTableCell(fileType))
		table.SetCell(row, 2, tview.NewTableCell(size))
		table.SetCell(row, 3, tview.NewTableCell(modified))

		row++
	}

	table.SetTitle(fmt.Sprintf("<File Browser - %s>", dir)).SetBorderColor(tcell.ColorGreen)
	table.Select(1, 0).SetFixed(1, 0).SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			row, _ := table.GetSelection()
			if row == 1 {
				// Go to parent directory
				parentDir := filepath.Dir(dir)
				if parentDir != dir {
					t.updateFileTable(table, parentDir)
				}
			} else if row > 1 && row <= len(files)+1 {
				selectedFile := files[row-2]
				if selectedFile.IsDir() {
					t.updateFileTable(table, filepath.Join(dir, selectedFile.Name()))
				} else {
					// You can add file viewing functionality here if needed
					// t.showMessage(fmt.Sprintf("Selected file: %s", selectedFile.Name()))
				}
			}
		}
	})
}
