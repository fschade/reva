package remote

import (
	"context"
	"path"
	"sync"

	"github.com/cs3org/reva/v2/pkg/storage/utils/metadata"
	"github.com/cs3org/reva/v2/pkg/syncx"
)

const (
	OPPull int = 0
	OPPush
)

type Manager[T any] struct {
	resources syncx.Map[string, *Resource[T]]
	locks     syncx.Map[string, *sync.RWMutex]
	storage   metadata.Storage
}

func NewManager[T any](storage metadata.Storage) Manager[T] {
	return Manager[T]{
		storage: storage,
	}
}

func (m *Manager[T]) Get(ctx context.Context, p string) (*Resource[T], *sync.RWMutex, error) {
	mu, _ := m.locks.LoadOrStore(p, &sync.RWMutex{})

	r, loaded := m.resources.LoadOrStore(p, &Resource[T]{
		path:    p,
		storage: m.storage,
	})

	if !loaded {
		mu.Lock()
		err := m.storage.MakeDirIfNotExist(ctx, path.Dir(p))
		if err != nil {
			return nil, mu, err
		}
		mu.Unlock()
	}

	return r, mu, nil
}
