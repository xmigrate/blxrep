package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/xmigrate/blxrep/pkg/dispatcher"
	"github.com/xmigrate/blxrep/service"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func (t *DispatcherTUI) showActions() {
	t.viewState = viewActions
	t.updateInfoBar([]string{
		"[green]<p>[white] Pause",
		"[green]<r>[white] Resume",
		"[green]<q>[white] Quit",
		"[green]<esc>[white] Back",
	})
	// Create a new table for actions
	actionsTable := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false)

	actionsTable.SetTitle("<Actions>").
		SetBorder(true).
		SetTitleColor(tcell.ColorPurple).
		SetBorderColor(tcell.ColorYellowGreen).SetBorderColor(tcell.ColorGreen)

	// Set up table headers
	actionsTable.SetCell(0, 0, tview.NewTableCell("Agent ID").SetTextColor(tcell.ColorYellow).SetSelectable(false))
	actionsTable.SetCell(0, 1, tview.NewTableCell("Action ID").SetTextColor(tcell.ColorYellow).SetSelectable(false))
	actionsTable.SetCell(0, 2, tview.NewTableCell("Action").SetTextColor(tcell.ColorYellow).SetSelectable(false))
	actionsTable.SetCell(0, 3, tview.NewTableCell("Status").SetTextColor(tcell.ColorYellow).SetSelectable(false))
	actionsTable.SetCell(0, 4, tview.NewTableCell("Type").SetTextColor(tcell.ColorYellow).SetSelectable(false))
	actionsTable.SetCell(0, 5, tview.NewTableCell("Created").SetTextColor(tcell.ColorYellow).SetSelectable(false))
	actionsTable.SetCell(0, 6, tview.NewTableCell("Progress").SetTextColor(tcell.ColorYellow).SetSelectable(false))

	t.content.Clear()
	t.content.AddItem(actionsTable, 0, 1, true)
	t.table = actionsTable
	t.app.SetFocus(actionsTable)

	// Start a goroutine to update the actions periodically
	go t.updateActionsPeriodicallly(actionsTable)
}

func (t *DispatcherTUI) updateActionsPeriodicallly(actionsTable *tview.Table) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			t.app.QueueUpdateDraw(func() {
				t.updateActionsTable(actionsTable)
			})
		}
	}
}

func (t *DispatcherTUI) updateActionsTable(actionsTable *tview.Table) {
	// t.tableMutex.Lock()
	// defer t.tableMutex.Unlock()

	actions, err := service.GetAllActions(100)
	if err != nil {
		t.showError(fmt.Sprintf("Error fetching actions: %v", err))
		return
	}

	// Sort actions by TimeStarted in descending order (most recent first)
	sort.Slice(actions, func(i, j int) bool {
		return actions[i].TimeStarted > actions[j].TimeStarted
	})

	// Update or add rows for each action
	for i, action := range actions {
		row := i + 1 // +1 because row 0 is the header

		// Ensure we have enough rows
		for r := actionsTable.GetRowCount(); r <= row; r++ {
			actionsTable.SetCell(r, 0, tview.NewTableCell(""))
			actionsTable.SetCell(r, 1, tview.NewTableCell(""))
			actionsTable.SetCell(r, 2, tview.NewTableCell(""))
			actionsTable.SetCell(r, 3, tview.NewTableCell(""))
			actionsTable.SetCell(r, 4, tview.NewTableCell(""))
			actionsTable.SetCell(r, 5, tview.NewTableCell(""))
			actionsTable.SetCell(r, 6, tview.NewTableCell(""))
		}

		actionsTable.GetCell(row, 0).SetText(action.AgentId)
		actionsTable.GetCell(row, 1).SetText(action.Id)
		actionsTable.GetCell(row, 2).SetText(action.Action)
		actionsTable.GetCell(row, 3).SetText(action.ActionStatus)
		actionsTable.GetCell(row, 4).SetText(action.ActionType)
		actionsTable.GetCell(row, 5).SetText(time.Unix(action.TimeStarted, 0).Format("2006-01-02 15:04:05"))
		progressBar := t.createProgressBar(action.ActionProgress, 20) // 20 is the width of the progress bar

		actionsTable.GetCell(row, 6).SetText(progressBar)

	}

	// Clear any extra rows
	for row := len(actions) + 1; row < actionsTable.GetRowCount(); row++ {
		for col := 0; col < actionsTable.GetColumnCount(); col++ {
			actionsTable.GetCell(row, col).SetText("")
		}
	}

	if len(actions) == 0 {
		actionsTable.GetCell(1, 0).SetText("No actions in progress").SetTextColor(tcell.ColorRed)
	}
}

func (t *DispatcherTUI) createProgressBar(progress int, width int) string {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}

	completed := int(float64(width) * float64(progress) / 100.0)
	remaining := width - completed

	bar := "["
	bar += strings.Repeat("[green]█[white]", completed)
	if remaining > 0 {
		bar += strings.Repeat("[green]░[white]", remaining)
	}
	bar += "]"

	return fmt.Sprintf("%s %3d%%", bar, progress)
}

func (t *DispatcherTUI) pauseSelectedAction(agentID string, actionID string, actionStatus string) {
	// Debug: Print the action details
	t.showMessage(fmt.Sprintf("Debug: AgentID: '%s', ActionID: '%s', Status: '%s'", agentID, actionID, actionStatus))

	if agentID == "" || actionID == "" {
		t.showError("Error: AgentID or ActionID is empty")
		return
	}

	if actionStatus == "" {
		t.showError("Error: Action status is empty")
		return
	}

	if strings.ToLower(actionStatus) != "in progress" {
		t.showMessage(fmt.Sprintf("Only actions in progress can be paused. Current status: %s", actionStatus))
		return
	}

	// Send pause message to the agent
	err := dispatcher.PauseAction(actionID, agentID)
	if err != nil {
		t.showError(fmt.Sprintf("Failed to pause action: %v", err))
		return
	}

	t.showMessage("Action paused successfully")
}

func (t *DispatcherTUI) resumeSelectedAction(agentID string, actionID string, actionStatus string) {
	// Debug: Print the action details
	t.showMessage(fmt.Sprintf("Debug: AgentID: '%s', ActionID: '%s', Status: '%s'", agentID, actionID, actionStatus))

	if agentID == "" || actionID == "" {
		t.showError("Error: AgentID or ActionID is empty")
		return
	}

	if actionStatus == "" {
		t.showError("Error: Action status is empty")
		return
	}

	if strings.ToLower(actionStatus) != "paused" {
		t.showMessage(fmt.Sprintf("Only paused actions can be resumed. Current status: %s", actionStatus))
		return
	}

	// Send resume message to the agent
	err := dispatcher.ResumeAction(actionID, agentID)
	if err != nil {
		t.showError(fmt.Sprintf("Failed to resume action: %v", err))
		return
	}

	t.showMessage("Action resumed successfully")
}
