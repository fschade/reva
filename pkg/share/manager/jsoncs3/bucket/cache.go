package bucket

import (
	"strings"

	"github.com/cs3org/reva/v2/pkg/syncx"
)

type Cache[V any] struct {
	m syncx.Map[string, V]
}

func (cs *Cache[V]) Upsert(key string, value V) {
	cs.m.Store(key, value)
}

func (cs *Cache[V]) Find(prefix string) []V {
	var items []V

	cs.m.Range(func(key string, value V) bool {
		if strings.HasPrefix(key, prefix) {
			items = append(items, value)
		}

		return true
	})

	return items
}

func (cs *Cache[V]) Get(key string) (V, bool) {
	return cs.m.Load(key)
}

func (cs *Cache[V]) Delete(key string) {
	cs.m.Delete(key)
}
