package agent

import (
	"log"
)

func Start(agentID string, dispatcherAddr string) {
	log.Printf("Agent %s is running...", agentID)
	// Connect to snapshot endpoint
	go ConnectToDispatcher(agentID, dispatcherAddr)
	// Keep the main goroutine alive
	select {}
}
