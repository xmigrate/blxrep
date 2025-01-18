package storage

type Storage struct {
}

type Service interface {
	Open() error
	Close() error
	SelectAll(limit int) (map[string][]byte, error)
	Insert(string, []byte) error
	Get(string) ([]byte, error)
	GetKeyCount() (uint64, error)
	Delete(string) error
}
