package utils

import (
	"log"
	"os"

	"github.com/gorilla/websocket"
)

func StreamData(blocks []AgentDataBlock, websock *websocket.Conn, resume bool, srcPath string, action CONST_AGENT_ACTION, startTime int64) {
	var agentBlocks AgentBulkMessage
	agentBlocks.StartTime = startTime
	agentBlocks.AgentID, _ = os.Hostname()
	agentBlocks.Data = blocks
	agentBlocks.SrcPath = srcPath
	agentBlocks.Action = action
	agentBlocks.TotalBlocks, _ = GetTotalSectors(srcPath)

	if resume {
		agentBlocks.DataType = "resume"
	} else {
		agentBlocks.DataType = "snapshot"
	}
	err := websock.WriteJSON(agentBlocks)
	if err != nil {
		log.Fatalf("Could not send snapshot data: %v", err)
	}
}
