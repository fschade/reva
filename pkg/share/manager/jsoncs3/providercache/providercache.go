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

package providercache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	cs3SharingCollaboration "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	cs3StorageProvider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/cs3org/reva/v2/pkg/appctx"
	"github.com/cs3org/reva/v2/pkg/errtypes"
	"github.com/cs3org/reva/v2/pkg/storage/utils/metadata"
	"github.com/cs3org/reva/v2/pkg/utils"
)

// name is the Tracer name used to identify this instrumentation library.
const tracerName = "providercache"

// Cache holds share information structured by cs3StorageProvider and space
type Cache struct {
	Providers map[string]*Spaces

	mu      sync.RWMutex
	storage metadata.Storage
	ttl     time.Duration
}

func (c *Cache) getProvider(providerID string) *Spaces {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Providers[providerID]
}

func (c *Cache) addProvider(providerID string, provider *Spaces) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Providers[providerID] = provider
}

// Spaces holds the share information for cs3StorageProvider
type Spaces struct {
	Spaces map[string]*Shares

	mu sync.RWMutex
}

func (s *Spaces) getSpace(spaceID string) *Shares {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.Spaces[spaceID]
}

func (s *Spaces) addSpace(spaceID string, space *Shares) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Spaces[spaceID] = space
}

// Shares holds the share information of one space
type Shares struct {
	Shares map[string]*cs3SharingCollaboration.Share
	Mtime  time.Time

	mu       sync.RWMutex
	nextSync time.Time
}

func (s *Shares) getShare(shareID string) *cs3SharingCollaboration.Share {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.Shares[shareID]
}

func (s *Shares) addShare(shareID string, share *cs3SharingCollaboration.Share) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Shares[shareID] = share
}

func (s *Shares) getMtime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.Mtime
}

func (s *Shares) setMtime(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Mtime = t
}

func (s *Shares) getNextSync() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.nextSync
}

func (s *Shares) setNextSync(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextSync = t
}

func (s *Shares) deleteShare(shareID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.Shares, shareID)
}

// UnmarshalJSON overrides the default unmarshaling
// Shares are tricky to unmarshal because they contain an interface (Grantee) which makes the json Unmarshal bail out
// To work around that problem we unmarshal into json.RawMessage in a first step and then try to manually unmarshal
// into the specific types in a second step.
func (s *Shares) UnmarshalJSON(data []byte) error {
	tmp := struct {
		Shares map[string]json.RawMessage
		Mtime  time.Time
	}{}

	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}

	shares := make(map[string]*cs3SharingCollaboration.Share, len(tmp.Shares))

	for id, genericShare := range tmp.Shares {
		userShare := &cs3SharingCollaboration.Share{
			Grantee: &cs3StorageProvider.Grantee{Id: &cs3StorageProvider.Grantee_UserId{}},
		}

		err = json.Unmarshal(genericShare, userShare) // is this a user share?
		if err == nil && userShare.Grantee.Type == cs3StorageProvider.GranteeType_GRANTEE_TYPE_USER {
			s.Shares[id] = userShare
			continue
		}

		groupShare := &cs3SharingCollaboration.Share{
			Grantee: &cs3StorageProvider.Grantee{Id: &cs3StorageProvider.Grantee_GroupId{}},
		}

		err = json.Unmarshal(genericShare, groupShare) // try to unmarshal to a group share if the user share unmarshalling failed
		if err != nil {
			return err
		}

		shares[id] = groupShare
	}

	{
		s.mu.Lock()
		s.Shares = shares
		s.Mtime = tmp.Mtime
		s.mu.Unlock()
	}

	return nil
}

// New returns a new Cache instance
func New(s metadata.Storage, ttl time.Duration) Cache {
	return Cache{
		Providers: map[string]*Spaces{},
		storage:   s,
		ttl:       ttl,
	}
}

// Add adds a share to the cache
func (c *Cache) Add(ctx context.Context, storageID, spaceID, shareID string, share *cs3SharingCollaboration.Share) error {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Add")
	defer span.End()
	span.SetAttributes(attribute.String("cs3.storageid", storageID), attribute.String("cs3.spaceid", spaceID), attribute.String("cs3.shareid", shareID))

	switch {
	case storageID == "":
		return fmt.Errorf("missing storage id")
	case spaceID == "":
		return fmt.Errorf("missing space id")
	case shareID == "":
		return fmt.Errorf("missing share id")
	}

	c.initializeIfNeeded(storageID, spaceID)

	_, space, _ := c.get(storageID, spaceID, "")
	space.addShare(shareID, share)

	return c.Persist(ctx, storageID, spaceID)
}

// Remove removes a share from the cache
func (c *Cache) Remove(ctx context.Context, storageID, spaceID, shareID string) error {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Remove")
	defer span.End()
	span.SetAttributes(attribute.String("cs3.storageid", storageID), attribute.String("cs3.spaceid", spaceID), attribute.String("cs3.shareid", shareID))

	_, space, _ := c.get(storageID, spaceID, "")
	if space == nil {
		return nil
	}

	space.deleteShare(shareID)

	return c.Persist(ctx, storageID, spaceID)
}

// Get returns one entry from the cache
func (c *Cache) Get(storageID, spaceID, shareID string) *cs3SharingCollaboration.Share {
	_, _, share := c.get(storageID, spaceID, shareID)
	return share
}

// ListSpace returns the list of shares in a given space
func (c *Cache) ListSpace(storageID, spaceID string) *Shares {
	_, space, _ := c.get(storageID, spaceID, "")
	if space == nil {
		return &Shares{}
	}

	return space
}

// PersistWithTime persists the data of one space if it has not been modified since the given mtime
func (c *Cache) PersistWithTime(ctx context.Context, storageID, spaceID string, mtime time.Time) error {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "PersistWithTime")
	defer span.End()
	span.SetAttributes(attribute.String("cs3.storageid", storageID), attribute.String("cs3.spaceid", spaceID))

	_, space, _ := c.get(storageID, spaceID, "")
	if space == nil {
		return nil
	}

	oldMtime := space.getMtime()
	space.setMtime(mtime)

	// FIXME there is a race when between this time now and the below Uploed another process also updates the file -> we need a lock
	createdBytes, err := json.Marshal(space)
	if err != nil {
		space.setMtime(oldMtime)
		return err
	}

	jsonPath := spaceJSONPath(storageID, spaceID)
	if err := c.storage.MakeDirIfNotExist(ctx, path.Dir(jsonPath)); err != nil {
		space.setMtime(oldMtime)
		return err
	}

	c.mu.Lock()
	err = c.storage.Upload(ctx, metadata.UploadRequest{
		Path:              jsonPath,
		Content:           createdBytes,
		IfUnmodifiedSince: mtime,
	})
	c.mu.Unlock()

	if err != nil {
		space.setMtime(oldMtime)
		return err
	}

	return nil
}

// Persist persists the data of one space
func (c *Cache) Persist(ctx context.Context, storageID, spaceID string) error {
	return c.PersistWithTime(ctx, storageID, spaceID, time.Now())
}

// Sync updates the in-memory data with the data from the storage if it is outdated
func (c *Cache) Sync(ctx context.Context, storageID, spaceID string) error {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Sync")
	defer span.End()

	span.SetAttributes(attribute.String("cs3.storageid", storageID), attribute.String("cs3.spaceid", spaceID))

	log := appctx.GetLogger(ctx).With().Str("storageID", storageID).Str("spaceID", spaceID).Logger()

	var mtime time.Time

	_, space, _ := c.get(storageID, spaceID, "")

	if space != nil {
		mtime = space.getMtime()

		if time.Now().Before(space.getNextSync()) {
			span.AddEvent("skip sync")
			span.SetStatus(codes.Ok, "")
			return nil
		}

		space.setNextSync(time.Now().Add(c.ttl))
	} else {
		mtime = time.Time{} // Set zero time so that data from storage always takes precedence
	}

	jsonPath := spaceJSONPath(storageID, spaceID)
	info, err := c.storage.Stat(ctx, jsonPath)
	if err != nil {
		if _, ok := err.(errtypes.NotFound); ok {
			span.AddEvent("no file")
			span.SetStatus(codes.Ok, "")
			return nil // Nothing to sync against
		}

		if _, ok := err.(*os.PathError); ok {
			span.AddEvent("no dir")
			span.SetStatus(codes.Ok, "")
			return nil // Nothing to sync against
		}

		span.SetStatus(codes.Error, fmt.Sprintf("Failed to stat the cs3StorageProvider cache: %s", err.Error()))
		log.Error().Err(err).Msg("Failed to stat the cs3StorageProvider cache")

		return err
	}

	// check mtime of /users/{userid}/created.json
	if utils.TSToTime(info.Mtime).After(mtime) {
		span.AddEvent("updating cache")
		//  - update cached list of created shares for the user in memory if changed
		createdBlob, err := c.storage.SimpleDownload(ctx, jsonPath)
		if err != nil {
			span.SetStatus(codes.Error, fmt.Sprintf("Failed to download the cs3StorageProvider cache: %s", err.Error()))
			log.Error().Err(err).Msg("Failed to download the cs3StorageProvider cache")
			return err
		}

		newShares := &Shares{}
		err = json.Unmarshal(createdBlob, newShares)
		if err != nil {
			span.SetStatus(codes.Error, fmt.Sprintf("Failed to unmarshal the cs3StorageProvider cache: %s", err.Error()))
			log.Error().Err(err).Msg("Failed to unmarshal the cs3StorageProvider cache")
			return err
		}

		newShares.Mtime = utils.TSToTime(info.Mtime)
		c.initializeIfNeeded(storageID, spaceID)
		c.getProvider(storageID).addSpace(spaceID, newShares)
	}

	span.SetStatus(codes.Ok, "")

	return nil
}

func (c *Cache) initializeIfNeeded(storageID, spaceID string) {
	if c.getProvider(storageID) == nil {
		c.addProvider(storageID, &Spaces{
			Spaces: map[string]*Shares{},
		})
	}

	provider, space, _ := c.get(storageID, spaceID, "")
	if space == nil {
		provider.addSpace(spaceID, &Shares{
			Shares: map[string]*cs3SharingCollaboration.Share{},
		})
	}
}

func (c *Cache) get(providerID, spaceID, shareID string) (*Spaces, *Shares, *cs3SharingCollaboration.Share) {

	if providerID == "" {
		return nil, nil, nil
	}

	provider := c.getProvider(providerID)
	if provider == nil {
		return nil, nil, nil
	}

	if spaceID == "" {
		return provider, nil, nil
	}

	space := provider.getSpace(spaceID)
	if space == nil {
		return provider, nil, nil
	}

	if shareID == "" {
		return provider, space, nil
	}

	share := space.getShare(shareID)
	if space == nil {
		return provider, space, nil
	}

	return provider, space, share
}

func spaceJSONPath(storageID, spaceID string) string {
	return filepath.Join("/storages", storageID, spaceID+".json")
}
