package agent

import (
	"fmt"

	"github.com/xmigrate/blxrep/utils"
)

func Start(agentID string, dispatcherAddr string, device []string) {
	utils.LogDebug(fmt.Sprintf("Agent %s started", agentID))
	fmt.Printf("Agent %s is running...\n", agentID)
	// Connect to snapshot endpoint
	go ConnectToDispatcher(agentID, dispatcherAddr, device)
	// Keep the main goroutine alive
	select {}
}
