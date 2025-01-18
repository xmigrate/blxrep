package tui

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/xmigrate/blxrep/pkg/dispatcher"
	"github.com/xmigrate/blxrep/utils"
)

func (t *DispatcherTUI) showCheckpoints(agentID string, disk string) {
	checkpoints, err := dispatcher.ShowCheckpoints("", "", agentID, t.dataDir, disk)
	if err != nil {
		t.showError(fmt.Sprintf("Error fetching checkpoints: %v", err))
		return
	}

	t.viewState = viewCheckpoints
	t.updateInfoBar([]string{
		"[green]<enter>[white] Select",
		"[green]<esc>[white] Back",
		"[green]<q>[white] Quit",
	})
	t.currentAgentID = agentID

	t.table.Clear()
	t.table.SetTitle(fmt.Sprintf("<Checkpoints for Agent: %s>", agentID))

	t.table.SetCell(0, 0, tview.NewTableCell("Timestamp").SetTextColor(tcell.ColorYellow).SetSelectable(false))
	t.table.SetCell(0, 1, tview.NewTableCell("Filename").SetTextColor(tcell.ColorYellow).SetSelectable(false))

	for i, cp := range checkpoints {
		t.table.SetCell(i+1, 0, tview.NewTableCell(cp.Timestamp.Format("2006-01-02 15:04:05")))
		t.table.SetCell(i+1, 1, tview.NewTableCell(cp.Filename))
	}

	if len(checkpoints) == 0 {
		t.table.SetCell(1, 0, tview.NewTableCell("No checkpoints found").SetTextColor(tcell.ColorRed))
	}

	t.table.Select(1, 0).SetFixed(1, 0)
	t.app.SetFocus(t.table)
}

func (t *DispatcherTUI) selectCheckpoint() {
	row, _ := t.table.GetSelection()
	if row > 0 && row <= t.table.GetRowCount() {
		t.selectedCheckpoint = &utils.Checkpoint{
			Filename:  t.table.GetCell(row, 1).Text,
			Timestamp: t.parseTimestamp(t.table.GetCell(row, 0).Text),
		}
		t.showCheckpointOptions()
	}
}

func (t *DispatcherTUI) showCheckpointOptions() {
	t.viewState = viewCheckpointOptions

	t.table.Clear()
	t.table.SetBorders(false)

	t.table.SetTitle(fmt.Sprintf("<Options for Checkpoint: %s>", t.selectedCheckpoint.Filename))

	t.table.SetCell(0, 0, tview.NewTableCell("Option").SetTextColor(tcell.ColorYellow).SetSelectable(false))
	t.table.SetCell(0, 1, tview.NewTableCell("Description").SetTextColor(tcell.ColorYellow).SetSelectable(false))

	t.table.SetCell(1, 0, tview.NewTableCell("Restore").SetTextColor(tcell.ColorWhite))
	t.table.SetCell(1, 1, tview.NewTableCell("Restore this checkpoint"))

	t.table.SetCell(2, 0, tview.NewTableCell("Browse").SetTextColor(tcell.ColorWhite))
	t.table.SetCell(2, 1, tview.NewTableCell("Browse files in this checkpoint"))

	t.table.Select(1, 0).SetFixed(1, 0)

	t.content.Clear()
	t.content.AddItem(t.table, 0, 1, true)
	t.app.SetFocus(t.table)
}

func (t *DispatcherTUI) selectOption() {
	row, _ := t.table.GetSelection()
	switch row {
	case 1:
		t.restoreCheckpoint()
	case 2:
		t.browseCheckpoint()
	}
}

func (t *DispatcherTUI) restoreCheckpoint() {
	t.viewState = viewRestoreOptions

	t.table.Clear()
	t.table.SetBorders(false)

	t.table.SetTitle(fmt.Sprintf("<Restore Options for Checkpoint: %s>", t.selectedCheckpoint.Filename))

	t.table.SetCell(0, 0, tview.NewTableCell("Option").SetTextColor(tcell.ColorYellow).SetSelectable(false))
	t.table.SetCell(0, 1, tview.NewTableCell("Description").SetTextColor(tcell.ColorYellow).SetSelectable(false))

	t.table.SetCell(1, 0, tview.NewTableCell("Restore Partition").SetTextColor(tcell.ColorWhite))
	t.table.SetCell(1, 1, tview.NewTableCell("Restore a specific partition from the checkpoint"))

	t.table.SetCell(2, 0, tview.NewTableCell("Restore Disk").SetTextColor(tcell.ColorWhite))
	t.table.SetCell(2, 1, tview.NewTableCell("Restore the entire disk from the checkpoint"))

	t.table.Select(1, 0).SetFixed(1, 0)

	t.content.Clear()
	t.content.AddItem(t.table, 0, 1, true)
	t.app.SetFocus(t.table)

}

func (t *DispatcherTUI) parseTimestamp(timeStr string) time.Time {
	timestamp, err := time.Parse("2006-01-02 15:04:05", timeStr)
	if err != nil {
		// Handle the error, maybe log it or use a default time
		return time.Now()
	}
	return timestamp
}
