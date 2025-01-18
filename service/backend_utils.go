package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/xmigrate/blxrep/utils"
)

func GetAgentConfigFromBackend(token, url string) (utils.ApiResponse, error) {

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return utils.ApiResponse{}, err
	}

	query := req.URL.Query()
	query.Add("hostname", "all")
	req.URL.RawQuery = query.Encode()
	req.Header.Set("accept", "application/json")
	req.Header.Set("token", token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return utils.ApiResponse{}, err
	}
	defer resp.Body.Close()
	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return utils.ApiResponse{}, err
	}

	// Unmarshal (parse) the JSON response
	var apiResponse utils.ApiResponse
	err = json.Unmarshal(body, &apiResponse)
	if err != nil {
		return utils.ApiResponse{}, err
	}

	if resp.StatusCode != http.StatusOK {
		return utils.ApiResponse{}, fmt.Errorf("error fetching configuration status code :- %d", apiResponse.Status)
	}
	return apiResponse, nil

}

func GetAgentActionFromBackend(token string, url string, action_type utils.CONST_ACTION_TYPE) ([]utils.Action, error) {

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return []utils.Action{}, err
	}
	query := req.URL.Query()
	query.Add("action_status", string(utils.CONST_ACTION_STATUS_WAITING))
	query.Add("action_status", string(utils.CONST_ACTION_STATUS_PAUSED))
	query.Add("action_status", string(utils.CONST_ACTION_STATUS_RESUMED))
	req.URL.RawQuery = query.Encode()
	req.Header.Set("accept", "application/json")
	req.Header.Set("token", token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return []utils.Action{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return []utils.Action{}, err
	}

	var action utils.ApiActionResponse
	err = json.Unmarshal(body, &action)
	if err != nil {
		return []utils.Action{}, err
	}

	if resp.StatusCode != http.StatusOK {
		return []utils.Action{}, fmt.Errorf("error fetching action status code :- %d", action.Status)
	}
	actions := []utils.Action{}
	for _, backendAction := range action.Actions {
		disk := map[string]utils.DiskSnapshot{
			backendAction.Disk: {
				Name: backendAction.Disk,
			},
		}
		actions = append(actions, utils.Action{
			Id:             backendAction.Id,
			ActionId:       backendAction.ActionId,
			SnapshotId:     backendAction.SnapshotId,
			AgentId:        backendAction.AgentId,
			OsVersion:      backendAction.OsVersion,
			FileSystem:     backendAction.FileSystem,
			Distro:         backendAction.Distro,
			Disk:           disk,
			Action:         backendAction.Action,
			ActionType:     backendAction.ActionType,
			ActionStatus:   backendAction.ActionStatus,
			ActionProgress: backendAction.ActionProgress,
			Hostname:       backendAction.Hostname,
			TargetName:     backendAction.TargetName,
			TimeCreated:    backendAction.TimeCreated,
			TimeStarted:    backendAction.TimeStarted,
			TimeUpdated:    backendAction.TimeUpdated,
			TimeFinished:   backendAction.TimeFinished,
			SourceFilePath: backendAction.SourceFilePath,
			TargetFilePath: backendAction.TargetFilePath,
		})
	}
	return actions, nil

}

func PushActionToBackend(token string, url string, action utils.ActionPutRequest) error {

	reqBody, err := json.Marshal(action)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(reqBody))

	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("token", token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	utils.LogDebug(fmt.Sprintf("Response: %s", string(body)))
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("Error pushing action status code: %s", string(body))
	}
	return nil
}

func PostActionToBackend(token string, url string, action utils.ActionPostRequest) (string, error) {
	reqBody, err := json.Marshal(action)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("token", token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("Error pushing action status code: %s", string(body))
	}
	var actionId utils.ActionIdResponse
	err = json.Unmarshal(body, &actionId)
	if err != nil {
		return "", err
	}
	return actionId.ActionId, nil
}
