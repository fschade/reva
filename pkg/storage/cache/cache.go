// Copyright 2018-2021 CERN
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// In applying this license, CERN does not waive the privileges and immunities
// granted to it by virtue of its status as an Intergovernmental Organization
// or submit itself to any jurisdiction.

package cache

import (
	"fmt"
	"strings"
	"sync"
	"time"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/cs3org/reva/v2/pkg/store"
	"github.com/shamaton/msgpack/v2"
	microstore "go-micro.dev/v4/store"
)

var (
	// DefaultStatCache is the memory store.
	statCaches                = make(map[string]StatCache)
	providerCaches            = make(map[string]ProviderCache)
	createHomeCaches          = make(map[string]CreateHomeCache)
	createPersonalSpaceCaches = make(map[string]CreatePersonalSpaceCache)
	fileMetadataCaches        = make(map[string]FileMetadataCache)
	mutex                     sync.Mutex
)

// Options contains options of configuring a cache
type Options struct {
	Store    string   `mapstructure:"cache_store"`
	Nodes    []string `mapstructure:"cache_nodes"`
	Database string   `mapstructure:"cache_database"`
	Table    string   `mapstructure:"cache_table"`
	TTL      int      `mapstructure:"cache_ttl"`
	Size     int      `mapstructure:"cache_size"`
}

// Cache handles key value operations on caches
type Cache interface {
	PullFromCache(key string, dest interface{}) error
	PushToCache(key string, src interface{}) error
	List(opts ...microstore.ListOption) ([]string, error)
	Delete(key string, opts ...microstore.DeleteOption) error
	Close() error
}

// StatCache handles removing keys from a stat cache
type StatCache interface {
	Cache
	RemoveStat(userID *userpb.UserId, res *provider.ResourceId)
	GetKey(userID *userpb.UserId, ref *provider.Reference, metaDataKeys, fieldMaskPaths []string) string
}

// ProviderCache handles removing keys from a storage provider cache
type ProviderCache interface {
	Cache
	RemoveListStorageProviders(res *provider.ResourceId)
	GetKey(userID *userpb.UserId, spaceID string) string
}

// CreateHomeCache handles removing keys from a create home cache
type CreateHomeCache interface {
	Cache
	RemoveCreateHome(res *provider.ResourceId)
	GetKey(userID *userpb.UserId) string
}

// CreatePersonalSpaceCache handles removing keys from a create home cache
type CreatePersonalSpaceCache interface {
	Cache
	GetKey(userID *userpb.UserId) string
}

// FileMetadataCache handles file metadata
type FileMetadataCache interface {
	Cache
	RemoveMetadata(path string) error
}

// GetStatCache will return an existing StatCache for the given store, nodes, database and table
// If it does not exist yet it will be created, different TTLs are ignored
func GetStatCache(cacheStore string, cacheNodes []string, database, table string, ttl time.Duration, size int) StatCache {
	mutex.Lock()
	defer mutex.Unlock()

	key := strings.Join(append(append([]string{cacheStore}, cacheNodes...), database, table), ":")
	if statCaches[key] == nil {
		statCaches[key] = NewStatCache(cacheStore, cacheNodes, database, table, ttl, size)
	}
	return statCaches[key]
}

// GetProviderCache will return an existing ProviderCache for the given store, nodes, database and table
// If it does not exist yet it will be created, different TTLs are ignored
func GetProviderCache(cacheStore string, cacheNodes []string, database, table string, ttl time.Duration, size int) ProviderCache {
	mutex.Lock()
	defer mutex.Unlock()

	key := strings.Join(append(append([]string{cacheStore}, cacheNodes...), database, table), ":")
	if providerCaches[key] == nil {
		providerCaches[key] = NewProviderCache(cacheStore, cacheNodes, database, table, ttl, size)
	}
	return providerCaches[key]
}

// GetCreateHomeCache will return an existing CreateHomeCache for the given store, nodes, database and table
// If it does not exist yet it will be created, different TTLs are ignored
func GetCreateHomeCache(cacheStore string, cacheNodes []string, database, table string, ttl time.Duration, size int) CreateHomeCache {
	mutex.Lock()
	defer mutex.Unlock()

	key := strings.Join(append(append([]string{cacheStore}, cacheNodes...), database, table), ":")
	if createHomeCaches[key] == nil {
		createHomeCaches[key] = NewCreateHomeCache(cacheStore, cacheNodes, database, table, ttl, size)
	}
	return createHomeCaches[key]
}

// GetCreatePersonalSpaceCache will return an existing CreatePersonalSpaceCache for the given store, nodes, database and table
// If it does not exist yet it will be created, different TTLs are ignored
func GetCreatePersonalSpaceCache(cacheStore string, cacheNodes []string, database, table string, ttl time.Duration, size int) CreatePersonalSpaceCache {
	mutex.Lock()
	defer mutex.Unlock()

	key := strings.Join(append(append([]string{cacheStore}, cacheNodes...), database, table), ":")
	if createPersonalSpaceCaches[key] == nil {
		createPersonalSpaceCaches[key] = NewCreatePersonalSpaceCache(cacheStore, cacheNodes, database, table, ttl, size)
	}
	return createPersonalSpaceCaches[key]
}

// GetFileMetadataCache will return an existing GetFileMetadataCache for the given store, nodes, database and table
// If it does not exist yet it will be created, different TTLs are ignored
func GetFileMetadataCache(cacheStore string, cacheNodes []string, database, table string, ttl time.Duration, size int) FileMetadataCache {
	mutex.Lock()
	defer mutex.Unlock()

	key := strings.Join(append(append([]string{cacheStore}, cacheNodes...), database, table), ":")
	if fileMetadataCaches[key] == nil {
		fileMetadataCaches[key] = NewFileMetadataCache(cacheStore, cacheNodes, database, table, ttl, size)
	}
	return fileMetadataCaches[key]
}

// CacheStore holds cache store specific configuration
type cacheStore struct {
	s               microstore.Store
	database, table string
	ttl             time.Duration
}

// PullFromCache pulls a value from the configured database and table of the underlying store using the given key
func (cache cacheStore) PullFromCache(key string, dest interface{}) error {
	r, err := cache.s.Read(key, microstore.ReadFrom(cache.database, cache.table), microstore.ReadLimit(1))
	if err != nil {
		return err
	}
	if len(r) == 0 {
		return fmt.Errorf("not found")
	}

	return msgpack.Unmarshal(r[0].Value, &dest)
}

// PushToCache pushes a key and value to the configured database and table of the underlying store
func (cache cacheStore) PushToCache(key string, src interface{}) error {
	b, err := msgpack.Marshal(src)
	if err != nil {
		return err
	}
	return cache.s.Write(
		&microstore.Record{Key: key, Value: b},
		microstore.WriteTo(cache.database, cache.table),
		microstore.WriteTTL(cache.ttl),
	)
}

// List lists the keys on the configured database and table of the underlying store
func (cache cacheStore) List(opts ...microstore.ListOption) ([]string, error) {
	o := []microstore.ListOption{
		microstore.ListFrom(cache.database, cache.table),
	}
	o = append(o, opts...)
	keys, err := cache.s.List(o...)
	if err != nil {
		return nil, err
	}
	for i, key := range keys {
		keys[i] = strings.TrimPrefix(key, cache.table)
	}
	return keys, nil
}

// Delete deletes the given key on the configured database and table of the underlying store
func (cache cacheStore) Delete(key string, opts ...microstore.DeleteOption) error {
	o := []microstore.DeleteOption{
		microstore.DeleteFrom(cache.database, cache.table),
	}
	o = append(o, opts...)
	return cache.s.Delete(key, o...)
}

// Close closes the underlying store
func (cache cacheStore) Close() error {
	return cache.s.Close()
}

func getStore(storeType string, nodes []string, database, table string, ttl time.Duration, size int) microstore.Store {
	return store.Create(
		store.Store(storeType),
		microstore.Nodes(nodes...),
		microstore.Database(database),
		microstore.Table(table),
		store.TTL(ttl),
		store.Size(size),
	)
}
