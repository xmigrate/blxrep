package tui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/xmigrate/blxrep/service"
)

func (t *DispatcherTUI) showAgents() {
	t.viewState = viewAgents
	t.updateInfoBar([]string{
		"[green]<a>[white] Actions",
		"[green]<enter>[white] Browse",
		"[green]<q>[white] Quit",
	})
	var err error
	t.agents, err = service.GetConnectedAgentsMap()

	if err != nil {
		t.showError(fmt.Sprintf("Error fetching connected agents: %v", err))
		return
	}

	agentsTable := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false)

	agentsTable.SetTitle("<Connected Agents>").
		SetBorder(true).SetTitleColor(tcell.ColorPurple).SetBorderColor(tcell.ColorGreen)

	agentsTable.SetCell(0, 0, tview.NewTableCell("Agent ID").SetTextColor(tcell.ColorYellow).SetSelectable(false))
	agentsTable.SetCell(0, 1, tview.NewTableCell("Status").SetTextColor(tcell.ColorYellow).SetSelectable(false))

	row := 1
	for id, agent := range t.agents {
		agentsTable.SetCell(row, 0, tview.NewTableCell(id))
		status := "Disconnected"
		if agent.Connected {
			status = "Connected"
		}
		agentsTable.SetCell(row, 1, tview.NewTableCell(status))
		row++
	}

	if len(t.agents) == 0 {
		agentsTable.SetCell(1, 0, tview.NewTableCell("No connected agents found").SetTextColor(tcell.ColorRed))
	}

	agentsTable.Select(1, 0).SetFixed(1, 0)

	t.content.Clear()
	t.content.AddItem(agentsTable, 0, 1, true)
	t.table = agentsTable
	t.app.SetFocus(agentsTable)
}

func (t *DispatcherTUI) showCheckpointsForSelectedAgent() {
	if t.table == nil {
		t.showError("No agent table available")
		return
	}

	row, _ := t.table.GetSelection()
	if row == 0 {
		t.showError("Please select an agent")
		return
	}

	agentID := t.table.GetCell(row, 0).Text
	t.currentAgentID = agentID
	t.showDisks(agentID)
}
