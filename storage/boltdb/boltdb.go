package boltdb

import (
	"encoding/json"
	"fmt"

	"github.com/xmigrate/blxrep/storage"
	"github.com/xmigrate/blxrep/utils"

	bolt "go.etcd.io/bbolt"
)

type BoltDB struct {
	DB   *bolt.DB
	Path string
}

func New(path string) storage.Service {
	return &BoltDB{Path: path}
}

func (db *BoltDB) Open() error {

	dbInstance, err := bolt.Open(db.Path, 0600, nil)
	if err != nil {
		utils.LogError("unable to open db for path: " + db.Path)
		return err
	}

	db.DB = dbInstance

	err = db.DB.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("default"))
		if err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		utils.LogError("unable to create 'default' bucket: " + err.Error())
		return err
	}

	return nil
}

func (db *BoltDB) Close() error { return db.DB.Close() }

func (db *BoltDB) SelectAll(limit int) (map[string][]byte, error) {
	if limit == 0 {
		limit = 10
	}

	blocks := make(map[string][]byte, 0)

	err := db.DB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("default"))
		if b == nil {
			return fmt.Errorf("Bucket %q not found!", "default")
		}

		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if limit != -1 && len(blocks) >= limit {
				break
			}

			blocks[string(k)] = v

		}

		return nil
	})

	return blocks, err
}

func (db *BoltDB) Insert(id string, data []byte) error {

	return db.DB.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("default"))
		if err != nil {
			return err
		}

		key, err := json.Marshal(id)
		if err != nil {
			return err
		}

		return b.Put(key, data)
	})
}

func (db *BoltDB) Get(agentId string) ([]byte, error) {

	var agent []byte

	err := db.DB.View(func(tx *bolt.Tx) error {
		// Retrieve the bucket (assumes it exists)
		bucket := tx.Bucket([]byte("default"))

		key, err := json.Marshal(agentId)
		if err != nil {
			return err
		}

		// Check if the key exists
		if value := bucket.Get(key); value != nil {
			// Key exists, print the value

			agent = value
		} else {
			// Key does not exist
			return fmt.Errorf("key %s does not exists", agentId)
		}

		return nil
	})

	return agent, err
}

func (db *BoltDB) GetKeyCount() (uint64, error) {
	var keyCount uint64

	err := db.DB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("default"))
		if b == nil {
			return fmt.Errorf("Bucket %s not found!", "default")
		}

		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			keyCount++
		}

		return nil
	})

	if err != nil {
		return 0, err
	}

	return keyCount, nil
}

func (db *BoltDB) Delete(agentId string) error {
	return db.DB.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("default"))
		if b == nil {
			return fmt.Errorf("Bucket %s not found!", "default")
		}

		key, err := json.Marshal(agentId)
		if err != nil {
			return err
		}

		return b.Delete(key)
	})
}
