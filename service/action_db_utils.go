package service

import (
	"encoding/json"
	"fmt"

	"github.com/xmigrate/blxrep/storage"
	"github.com/xmigrate/blxrep/storage/boltdb"
	"github.com/xmigrate/blxrep/utils"
)

func getActionDBInstance() storage.Service {
	return boltdb.New(utils.AppConfiguration.DataDir + "/xmaction.db")
}

func GetAction(actionID string) (utils.Action, error) {
	db := getActionDBInstance()

	if err := db.Open(); err != nil {
		return utils.Action{}, err
	}
	//close db and handle error inside defer
	defer func() {
		if err := db.Close(); err != nil {
			utils.LogError("unable to close DB : " + err.Error())
		}
	}()

	actionObj, err := db.Get(actionID)

	if err != nil {
		return utils.Action{}, err
	}

	var action utils.Action
	err = json.Unmarshal(actionObj, &action)
	if err != nil {
		return utils.Action{}, err
	}

	return action, nil
}

func GetActionWithId(actionID string) (utils.Action, error) {
	db := getActionDBInstance()

	if err := db.Open(); err != nil {
		return utils.Action{}, err
	}
	//close db and handle error inside defer
	defer func() {
		if err := db.Close(); err != nil {
			utils.LogError("unable to close DB : " + err.Error())
		}
	}()

	actionObj, err := db.SelectAll(-1)

	if err != nil {
		return utils.Action{}, err
	}
	for _, v := range actionObj {
		var action utils.Action
		err = json.Unmarshal(v, &action)
		if err != nil {
			return utils.Action{}, err
		}
		if action.ActionId == actionID {
			return action, nil
		}
	}

	return utils.Action{}, fmt.Errorf("action not found")
}

func InsertOrUpdateAction(action utils.Action) error {
	db := getActionDBInstance()

	if err := db.Open(); err != nil {
		return err
	}

	actionObj, err := json.Marshal(action)
	if err != nil {
		return err
	}
	actionId := action.Id
	err = db.Insert(actionId, actionObj)
	if err != nil {
		return err
	}

	defer func() {
		if err := db.Close(); err != nil {
			utils.LogError("unable to close DB : " + err.Error())
		}
	}()

	return nil
}

func GetAllActions(limit int) ([]utils.Action, error) {
	db := getActionDBInstance()

	if err := db.Open(); err != nil {
		return nil, err
	}
	//close db and handle error inside defer
	defer func() {
		if err := db.Close(); err != nil {
			utils.LogError("unable to close DB : " + err.Error())
		}
	}()

	actions, err := db.SelectAll(limit)

	if err != nil {
		return nil, err
	}

	actionSlice := make([]utils.Action, 0)
	for _, v := range actions {
		action := utils.Action{}
		if err := json.Unmarshal(v, &action); err != nil {
			return nil, err
		}
		actionSlice = append(actionSlice, action)
	}

	return actionSlice, nil
}

func GetAllActionsWithStatus(limit int, status utils.CONST_ACTION_STATUS_TYPE) ([]utils.Action, error) {

	actions, err := GetAllActions(limit)
	if err != nil {
		return nil, err
	}

	filteredActions := make([]utils.Action, 0)
	for _, action := range actions {
		if action.ActionStatus == string(status) {
			filteredActions = append(filteredActions, action)
		}
	}

	return filteredActions, nil
}

func GetAllActionsWithUpdateStatus(limit int, status bool) ([]utils.Action, error) {
	actions, err := GetAllActions(limit)
	if err != nil {
		return nil, err
	}

	filteredActions := make([]utils.Action, 0)
	for _, action := range actions {
		if action.UpdateBackend == status {
			filteredActions = append(filteredActions, action)
		}
	}

	return filteredActions, nil

}
