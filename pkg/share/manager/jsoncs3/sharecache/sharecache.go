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

package sharecache

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/cs3org/reva/v2/pkg/appctx"
	"github.com/cs3org/reva/v2/pkg/errtypes"
	"github.com/cs3org/reva/v2/pkg/share/manager/jsoncs3/shareid"
	"github.com/cs3org/reva/v2/pkg/storage/utils/metadata"
	"github.com/cs3org/reva/v2/pkg/utils"
)

// name is the Tracer name used to identify this instrumentation library.
const tracerName = "sharecache"

// Cache caches the list of share ids for users/groups
// It functions as an in-memory cache with a persistence layer
// The storage is sharded by user/group
type Cache struct {
	UserShares map[string]*UserShareCache

	storage   metadata.Storage
	namespace string
	filename  string
	ttl       time.Duration
	mu        sync.RWMutex
}

func (c *Cache) getUserShareCache(userID string) *UserShareCache {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.UserShares[userID]
}

func (c *Cache) addUserShareCache(userID string, userShareCache *UserShareCache) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.UserShares[userID] = userShareCache
}

// UserShareCache holds the space/share map for one user
type UserShareCache struct {
	Mtime      time.Time
	UserShares map[string]*SpaceShareIDs

	nextSync time.Time
	mu       sync.RWMutex
}

func (s *UserShareCache) getNextSync() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.nextSync
}

func (s *UserShareCache) setNextSync(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextSync = t
}

func (c *UserShareCache) getUserShare(ssID string) *SpaceShareIDs {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.UserShares[ssID]
}

func (c *UserShareCache) getUserShares() map[string]*SpaceShareIDs {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.UserShares
}

func (c *UserShareCache) addUserShare(ssID string, userShare *SpaceShareIDs) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.UserShares[ssID] = userShare
}

func (s *UserShareCache) getMtime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.Mtime
}

func (s *UserShareCache) setMtime(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Mtime = t
}

// SpaceShareIDs holds the unique list of share ids for a space
type SpaceShareIDs struct {
	Mtime time.Time
	IDs   map[string]struct{}

	mu sync.RWMutex
}

func (c *SpaceShareIDs) getIDs() map[string]struct{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.IDs
}

func (c *SpaceShareIDs) getID(shareID string) *struct{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	s, ok := c.IDs[shareID]
	if !ok {
		return nil
	}

	return &s
}

func (c *SpaceShareIDs) addID(shareID string, str struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.IDs[shareID] = str
}

func (c *SpaceShareIDs) deleteID(shareID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.IDs, shareID)
}

func (c *SpaceShareIDs) getMtime() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Mtime
}

func (c *SpaceShareIDs) setMtime(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Mtime = t
}

// New returns a new Cache instance
func New(s metadata.Storage, namespace, filename string, ttl time.Duration) Cache {
	return Cache{
		UserShares: map[string]*UserShareCache{},
		storage:    s,
		namespace:  namespace,
		filename:   filename,
		ttl:        ttl,
	}
}

// Add adds a share to the cache
func (c *Cache) Add(ctx context.Context, userid, shareID string) error {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Add")
	defer span.End()
	span.SetAttributes(attribute.String("cs3.userid", userid), attribute.String("cs3.shareid", shareID))

	storageid, spaceid, _ := shareid.Decode(shareID)
	ssid := storageid + shareid.IDDelimiter + spaceid

	now := time.Now()
	userShareCache, _, _ := c.get(userid, "", "")
	if userShareCache == nil {
		c.addUserShareCache(userid, &UserShareCache{
			UserShares: map[string]*SpaceShareIDs{},
		})
	}

	userShareCache, userShare, _ := c.get(userid, ssid, "")
	if userShare == nil {
		userShareCache.addUserShare(ssid, &SpaceShareIDs{
			IDs: map[string]struct{}{},
		})
	}

	a, b, _ := c.get(userid, ssid, "")
	a.setMtime(now)
	b.setMtime(now)
	b.addID(shareID, struct{}{})

	return c.Persist(ctx, userid)
}

// Remove removes a share for the given user
func (c *Cache) Remove(ctx context.Context, userid, shareID string) error {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Remove")
	defer span.End()
	span.SetAttributes(attribute.String("cs3.userid", userid), attribute.String("cs3.shareid", shareID))

	storageid, spaceid, _ := shareid.Decode(shareID)
	ssid := storageid + shareid.IDDelimiter + spaceid

	now := time.Now()
	userShareCache, _, _ := c.get(userid, "", "")
	if userShareCache == nil {
		c.addUserShareCache(userid, &UserShareCache{
			Mtime:      now,
			UserShares: map[string]*SpaceShareIDs{},
		})
	}

	userShareCache, userShare, _ := c.get(userid, shareID, "")
	if userShare != nil {
		// remove share id
		userShareCache.setMtime(now)
		userShare.setMtime(now)
		userShare.deleteID(ssid)
	}

	return c.Persist(ctx, userid)
}

// List return the list of spaces/shares for the given user/group
func (c *Cache) List(userid string) map[string]SpaceShareIDs {
	r := map[string]SpaceShareIDs{}

	userShareCache, _, _ := c.get(userid, "", "")
	if userShareCache == nil {
		return r
	}

	for ssid, cached := range userShareCache.getUserShares() {
		r[ssid] = SpaceShareIDs{
			Mtime: cached.getMtime(),
			IDs:   cached.getIDs(),
		}
	}

	return r
}

// Sync updates the in-memory data with the data from the storage if it is outdated
func (c *Cache) Sync(ctx context.Context, userID string) error {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Sync")
	defer span.End()
	span.SetAttributes(attribute.String("cs3.userid", userID))

	log := appctx.GetLogger(ctx).With().Str("userID", userID).Logger()

	var mtime time.Time
	userShareCache, _, _ := c.get(userID, "", "")
	//  - do we have a cached list of created shares for the user in memory?
	if userShareCache != nil {
		if time.Now().Before(userShareCache.getNextSync()) {
			span.AddEvent("skip sync")
			span.SetStatus(codes.Ok, "")
			return nil
		}
		userShareCache.setNextSync(time.Now().Add(c.ttl))
		mtime = userShareCache.getMtime()
		//    - y: set If-Modified-Since header to only download if it changed
	} else {
		mtime = time.Time{} // Set zero time so that data from storage always takes precedence
	}

	userCreatedPath := c.userCreatedPath(userID)
	info, err := c.storage.Stat(ctx, userCreatedPath)
	if err != nil {
		if _, ok := err.(errtypes.NotFound); ok {
			span.AddEvent("no file")
			span.SetStatus(codes.Ok, "")
			return nil // Nothing to sync against
		}
		span.SetStatus(codes.Error, fmt.Sprintf("Failed to stat the share cache: %s", err.Error()))
		log.Error().Err(err).Msg("Failed to stat the share cache")
		return err
	}
	// check mtime of /users/{userid}/created.json
	if utils.TSToTime(info.Mtime).After(mtime) {
		span.AddEvent("updating cache")
		//  - update cached list of created shares for the user in memory if changed
		createdBlob, err := c.storage.SimpleDownload(ctx, userCreatedPath)
		if err != nil {
			span.SetStatus(codes.Error, fmt.Sprintf("Failed to download the share cache: %s", err.Error()))
			log.Error().Err(err).Msg("Failed to download the share cache")
			return err
		}
		newShareCache := &UserShareCache{}
		err = json.Unmarshal(createdBlob, newShareCache)
		if err != nil {
			span.SetStatus(codes.Error, fmt.Sprintf("Failed to unmarshal the share cache: %s", err.Error()))
			log.Error().Err(err).Msg("Failed to unmarshal the share cache")
			return err
		}
		newShareCache.Mtime = utils.TSToTime(info.Mtime)
		c.addUserShareCache(userID, newShareCache)
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

// Persist persists the data for one user/group to the storage
func (c *Cache) Persist(ctx context.Context, userid string) error {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Persist")
	defer span.End()
	span.SetAttributes(attribute.String("cs3.userid", userid))

	userShareCache, _, _ := c.get(userid, "", "")
	oldMtime := userShareCache.getMtime()
	mTime := time.Now()
	userShareCache.setMtime(mTime)

	createdBytes, err := json.Marshal(c.getUserShareCache(userid))
	if err != nil {
		userShareCache.setMtime(oldMtime)
		return err
	}
	jsonPath := c.userCreatedPath(userid)
	if err := c.storage.MakeDirIfNotExist(ctx, path.Dir(jsonPath)); err != nil {
		userShareCache.setMtime(oldMtime)
		return err
	}

	c.mu.Lock()
	err = c.storage.Upload(ctx, metadata.UploadRequest{
		Path:              jsonPath,
		Content:           createdBytes,
		IfUnmodifiedSince: mTime,
	})
	c.mu.Unlock()

	if err != nil {
		userShareCache.setMtime(oldMtime)
		return err
	}
	return nil
}

func (c *Cache) get(userID, ssID, idID string) (*UserShareCache, *SpaceShareIDs, *struct{}) {

	if userID == "" {
		return nil, nil, nil
	}

	userShareCache := c.getUserShareCache(userID)
	if userShareCache == nil {
		return nil, nil, nil
	}

	if ssID == "" {
		return userShareCache, nil, nil
	}

	userShare := userShareCache.getUserShare(ssID)
	if userShare == nil {
		return userShareCache, nil, nil
	}

	if idID == "" {
		return userShareCache, userShare, nil
	}

	id := userShare.getID(idID)
	if id == nil {
		return userShareCache, userShare, nil
	}

	return userShareCache, userShare, &struct{}{}
}

func (c *Cache) userCreatedPath(userid string) string {
	return filepath.Join("/", c.namespace, userid, c.filename)
}
