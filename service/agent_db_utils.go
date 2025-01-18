package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/xmigrate/blxrep/storage"
	"github.com/xmigrate/blxrep/storage/boltdb"
	"github.com/xmigrate/blxrep/utils"
)

func getDBInstance() storage.Service {
	return boltdb.New(utils.AppConfiguration.DataDir + "/xmdispatcher.db")
}

func GetAgent(agentID string) (utils.Agent, error) {

	db := getDBInstance()

	if err := db.Open(); err != nil {
		return utils.Agent{}, err
	}
	//close db and handle error inside defer
	defer func() {
		if err := db.Close(); err != nil {
			utils.LogError("unable to close DB : " + err.Error())
		}
	}()

	agentObj, err := db.Get(agentID)

	if err != nil {
		return utils.Agent{}, err
	}
	var agent utils.Agent

	err = json.Unmarshal(agentObj, &agent)
	if err != nil {
		return utils.Agent{}, err
	}

	return agent, nil
}

func InsertOrUpdateAgent(agent utils.Agent) error {

	db := getDBInstance()

	if err := db.Open(); err != nil {
		return err
	}

	defer func() {
		if err := db.Close(); err != nil {
			utils.LogError("unable to close DB : " + err.Error())
		}
	}()

	data, err := json.Marshal(agent)
	if err != nil {
		return err
	}

	if err := db.Insert(agent.AgentId, data); err != nil {
		return err
	}

	return nil
}

func InsertOrUpdateAgents(agents []utils.Agent) error {

	db := getDBInstance()

	if err := db.Open(); err != nil {
		return err
	}

	defer func() {
		if err := db.Close(); err != nil {
			utils.LogError("unable to close DB : " + err.Error())
		}
	}()

	for _, agent := range agents {
		ag, err := json.Marshal(agent)
		if err != nil {
			return err
		}
		if err := db.Insert(agent.AgentId, ag); err != nil {
			return err
		}
	}

	return nil
}

func InsertOrUpdateAgentsMap(agents map[string]utils.Agent) error {

	// convert map to slice
	var agentsSlice []utils.Agent
	for _, v := range agents {
		agentsSlice = append(agentsSlice, v)
	}
	return InsertOrUpdateAgents(agentsSlice)
}

func GetAllAgents(limit int) ([]utils.Agent, error) {

	db := getDBInstance()

	if err := db.Open(); err != nil {
		return []utils.Agent{}, nil
	}
	defer func() {
		if err := db.Close(); err != nil {
			utils.LogError("unable to close DB : " + err.Error())
		}
	}()

	agents, err := db.SelectAll(limit)

	if err != nil {
		return []utils.Agent{}, err
	}

	// convert map to agents slice
	agentsSlice := make([]utils.Agent, 0)
	for _, v := range agents {
		agent := utils.Agent{}
		if err := json.Unmarshal(v, &agent); err != nil {
			return []utils.Agent{}, err
		}
		agentsSlice = append(agentsSlice, agent)
	}

	return agentsSlice, nil
}

func GetAllAgentsMap(limit int) (map[string]utils.Agent, error) {

	agents, err := GetAllAgents(limit)
	if err != nil {
		return nil, err
	}

	agentMap := make(map[string]utils.Agent)
	for _, i := range agents {
		agentMap[i.AgentId] = i
	}

	return agentMap, nil
}

func SetAgentAction(agentId string, action utils.CONST_AGENT_ACTION) error {
	db := getDBInstance()

	if err := db.Open(); err != nil {
		return nil
	}

	defer func() {
		if err := db.Close(); err != nil {
			utils.LogError("unable to close DB : " + err.Error())
		}
	}()

	agentObj, err := db.Get(agentId)
	var agent utils.Agent

	if err != nil {
		return err
	}

	err = json.Unmarshal(agentObj, &agent)
	if err != nil {
		return err
	}

	agent.Action = action

	ag, err := json.Marshal(agent)
	if err != nil {
		return err
	}

	if err := db.Insert(agent.AgentId, ag); err != nil {
		return err
	}

	return nil
}

func GetConnectedAgents() ([]utils.Agent, error) {
	db := getDBInstance()
	if db == nil {
		return nil, fmt.Errorf("failed to get database instance")
	}

	if err := db.Open(); err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}
	defer db.Close() // Use defer to ensure db is closed

	agents, err := db.SelectAll(-1)
	if err != nil {
		return nil, fmt.Errorf("failed to select agents: %v", err)
	}

	if agents == nil {
		return make([]utils.Agent, 0), nil
	}

	utils.LogDebug(fmt.Sprintf("Retrieved %d raw agents from database", len(agents)))

	agentSlice := make([]utils.Agent, 0, len(agents))
	for i, v := range agents {
		if v == nil {
			utils.LogError(fmt.Sprintf("Warning: nil agent data at index %d", i))
			continue
		}

		var agent utils.Agent
		if err := json.Unmarshal(v, &agent); err != nil {
			utils.LogError(fmt.Sprintf("Error unmarshaling agent at index %d: %v", i, err))
			continue // Skip invalid agents instead of failing completely
		}

		// Validate critical fields
		if agent.AgentId == "" {
			utils.LogError(fmt.Sprintf("Warning: agent at index %d has empty AgentId", i))
			continue
		}

		agentSlice = append(agentSlice, agent)
	}

	// Filter connected agents
	connectedAgents := make([]utils.Agent, 0, len(agentSlice))
	for _, agent := range agentSlice {
		if agent.Connected {
			// Ensure LastSeen is not zero
			if agent.LastSeen.IsZero() {
				agent.LastSeen = time.Now()
			}

			// Initialize maps if nil
			if agent.CloneStatus == nil {
				agent.CloneStatus = make(map[string]int)
			}

			connectedAgents = append(connectedAgents, agent)
		}
	}

	utils.LogDebug(fmt.Sprintf("Returning %d connected agents", len(connectedAgents)))
	return connectedAgents, nil
}

func GetConnectedAgentsMap() (map[string]utils.Agent, error) {
	agents, err := GetConnectedAgents()
	if err != nil {
		utils.LogError("Error in GetConnectedAgents: " + err.Error())
		return nil, fmt.Errorf("failed to get connected agents: %v", err)
	}

	agentMap := make(map[string]utils.Agent)
	for _, agent := range agents {
		// Double check AgentId is not empty
		if agent.AgentId == "" {
			utils.LogError("Found agent with empty AgentId")
			continue
		}
		agentMap[agent.AgentId] = agent
	}

	// Log the result for debugging
	utils.LogDebug(fmt.Sprintf("Created agent map with %d entries", len(agentMap)))

	return agentMap, nil
}
