package tui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/xmigrate/blxrep/pkg/dispatcher"
)

func (t *DispatcherTUI) showDisks(agentID string) {
	disks, err := dispatcher.ShowDisks(agentID, t.dataDir)
	if err != nil {
		t.showError(fmt.Sprintf("Error fetching disks: %v", err))
		return
	}

	t.viewState = viewDisks
	t.updateInfoBar([]string{
		"[green]<enter>[white] Select",
		"[green]<esc>[white] Back",
		"[green]<q>[white] Quit",
	})
	t.currentAgentID = agentID

	t.table.Clear()
	t.table.SetTitle(fmt.Sprintf("<Disks for Agent: %s>", agentID))

	t.table.SetCell(0, 0, tview.NewTableCell("Disk").SetTextColor(tcell.ColorYellow).SetSelectable(false))

	for i, disk := range disks {
		t.table.SetCell(i+1, 0, tview.NewTableCell(disk))
	}

	if len(disks) == 0 {
		t.table.SetCell(1, 0, tview.NewTableCell("No disks found").SetTextColor(tcell.ColorRed))
	}

	t.table.Select(1, 0).SetFixed(1, 0)
	t.app.SetFocus(t.table)
}

func (t *DispatcherTUI) selectDisk() {
	row, _ := t.table.GetSelection()
	if row > 0 && row <= t.table.GetRowCount() {
		t.selectedDisk = t.table.GetCell(row, 0).Text
		t.showCheckpoints(t.currentAgentID, t.selectedDisk)
	}
}

func (t *DispatcherTUI) showDiskOptions() {
	t.viewState = viewDiskOptions

	t.table.Clear()
	t.table.SetBorders(false)

	t.table.SetTitle(fmt.Sprintf("<Options for Disk: %s>", t.selectedDisk))

	t.table.SetCell(0, 0, tview.NewTableCell("Option").SetTextColor(tcell.ColorYellow).SetSelectable(false))
	t.table.SetCell(0, 1, tview.NewTableCell("Description").SetTextColor(tcell.ColorYellow).SetSelectable(false))

	t.table.SetCell(1, 0, tview.NewTableCell("Restore").SetTextColor(tcell.ColorWhite))
	t.table.SetCell(1, 1, tview.NewTableCell("Restore this disk"))

	t.table.SetCell(2, 0, tview.NewTableCell("Browse").SetTextColor(tcell.ColorWhite))
	t.table.SetCell(2, 1, tview.NewTableCell("Browse files in this disk"))

	t.table.Select(1, 0).SetFixed(1, 0)

	t.content.Clear()
	t.content.AddItem(t.table, 0, 1, true)
	t.app.SetFocus(t.table)
}

func (t *DispatcherTUI) selectDisks() {
	row, _ := t.table.GetSelection()
	switch row {
	case 1:
		t.restoreDiskOptions()
	case 2:
		t.showCheckpoints(t.currentAgentID, t.selectedDisk)
	}
}

func (t *DispatcherTUI) restoreDiskOptions() {
	t.viewState = viewRestoreOptions

	t.table.Clear()
	t.table.SetBorders(false)

	t.table.SetTitle(fmt.Sprintf("<Restore Options for Disk: %s>", t.selectedDisk))

	t.table.SetCell(0, 0, tview.NewTableCell("Option").SetTextColor(tcell.ColorYellow).SetSelectable(false))
	t.table.SetCell(0, 1, tview.NewTableCell("Description").SetTextColor(tcell.ColorYellow).SetSelectable(false))

	t.table.SetCell(1, 0, tview.NewTableCell("Restore Partition").SetTextColor(tcell.ColorWhite))
	t.table.SetCell(1, 1, tview.NewTableCell("Restore a specific partition from the disk"))

	t.table.SetCell(2, 0, tview.NewTableCell("Restore Disk").SetTextColor(tcell.ColorWhite))
	t.table.SetCell(2, 1, tview.NewTableCell("Restore the entire disk from the disk"))

	t.table.Select(1, 0).SetFixed(1, 0)

	t.content.Clear()
	t.content.AddItem(t.table, 0, 1, true)
	t.app.SetFocus(t.table)

}
