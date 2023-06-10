// Copyright 2018-2022 CERN
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

package receivedsharecache

import (
	"context"
	"encoding/json"
	"fmt"
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
const tracerName = "receivedsharecache"

// Cache stores the list of received shares and their states
// It functions as an in-memory cache with a persistence layer
// The storage is sharded by user
type Cache struct {
	ReceivedSpaces map[string]*Spaces

	storage metadata.Storage
	ttl     time.Duration
	mu      sync.RWMutex
}

func (c *Cache) getReceivedSpaces(userID string) *Spaces {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.ReceivedSpaces[userID]
}

func (c *Cache) addReceivedSpaces(userID string, receivedSpaces *Spaces) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ReceivedSpaces[userID] = receivedSpaces
}

// Spaces holds the received shares of one user per space
type Spaces struct {
	Mtime  time.Time
	Spaces map[string]*Space

	nextSync time.Time
	mu       sync.RWMutex
}

func (s *Spaces) getSpace(spaceID string) *Space {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.Spaces[spaceID]
}

func (s *Spaces) addSpace(spaceID string, space *Space) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Spaces[spaceID] = space
}

func (s *Spaces) getNextSync() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.nextSync
}

func (s *Spaces) setNextSync(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextSync = t
}

func (s *Spaces) getMtime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.Mtime
}

func (s *Spaces) setMtime(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Mtime = t
}

// Space holds the received shares of one user in one space
type Space struct {
	Mtime  time.Time
	States map[string]*State

	mu sync.RWMutex
}

func (s *Space) getState(stateID string) *State {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.States[stateID]
}

func (s *Space) addState(stateID string, state *State) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.States == nil {
		s.States = map[string]*State{}
	}

	s.States[stateID] = state
}

func (s *Space) getMtime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.Mtime
}

func (s *Space) setMtime(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Mtime = t
}

// State holds the state information of a received share
type State struct {
	State      cs3SharingCollaboration.ShareState
	MountPoint *cs3StorageProvider.Reference

	mu sync.RWMutex
}

// New returns a new Cache instance
func New(s metadata.Storage, ttl time.Duration) Cache {
	return Cache{
		ReceivedSpaces: map[string]*Spaces{},
		storage:        s,
		ttl:            ttl,
	}
}

// Add adds a new entry to the cache
func (c *Cache) Add(ctx context.Context, userID, spaceID string, rs *cs3SharingCollaboration.ReceivedShare) error {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Add")
	defer span.End()
	span.SetAttributes(attribute.String("cs3.userid", userID), attribute.String("cs3.spaceid", spaceID))

	if c.getReceivedSpaces(userID) == nil {
		c.addReceivedSpaces(userID, &Spaces{
			Spaces: map[string]*Space{},
		})
	}

	receivedSpaces, space, _ := c.get(userID, spaceID, "")
	if space == nil {
		receivedSpaces.addSpace(spaceID, &Space{})
	}

	_, space, _ = c.get(userID, spaceID, "")
	space.setMtime(time.Now())
	space.addState(rs.Share.Id.GetOpaqueId(), &State{
		State:      rs.State,
		MountPoint: rs.MountPoint,
	})

	return c.Persist(ctx, userID)
}

// Get returns one entry from the cache
func (c *Cache) Get(userID, spaceID, shareID string) *State {
	_, _, state := c.get(userID, spaceID, shareID)

	return state
}

// Sync updates the in-memory data with the data from the storage if it is outdated
func (c *Cache) Sync(ctx context.Context, userID string) error {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Sync")
	defer span.End()
	span.SetAttributes(attribute.String("cs3.userid", userID))

	log := appctx.GetLogger(ctx).With().Str("userID", userID).Logger()

	var mtime time.Time

	receivedSpaces := c.getReceivedSpaces(userID)
	if receivedSpaces != nil {
		if time.Now().Before(receivedSpaces.getNextSync()) {
			span.AddEvent("skip sync")
			span.SetStatus(codes.Ok, "")
			return nil
		}
		receivedSpaces.setNextSync(time.Now().Add(c.ttl))
		mtime = receivedSpaces.getMtime()
	} else {
		mtime = time.Time{} // Set zero time so that data from storage always takes precedence
	}

	jsonPath := userJSONPath(userID)
	info, err := c.storage.Stat(ctx, jsonPath) // TODO we only need the mtime ... use fieldmask to make the request cheaper
	if err != nil {
		if _, ok := err.(errtypes.NotFound); ok {
			span.AddEvent("no file")
			span.SetStatus(codes.Ok, "")
			return nil // Nothing to sync against
		}

		span.SetStatus(codes.Error, fmt.Sprintf("Failed to stat the received share: %s", err.Error()))
		log.Error().Err(err).Msg("Failed to stat the received share")

		return err
	}
	// check mtime of /users/{userid}/created.json
	if utils.TSToTime(info.Mtime).After(mtime) {
		span.AddEvent("updating cache")
		//  - update cached list of created shares for the user in memory if changed
		createdBlob, err := c.storage.SimpleDownload(ctx, jsonPath)
		if err != nil {
			span.SetStatus(codes.Error, fmt.Sprintf("Failed to download the received share: %s", err.Error()))
			log.Error().Err(err).Msg("Failed to download the received share")
			return err
		}

		newSpaces := &Spaces{}
		err = json.Unmarshal(createdBlob, newSpaces)
		if err != nil {
			span.SetStatus(codes.Error, fmt.Sprintf("Failed to unmarshal the received share: %s", err.Error()))
			log.Error().Err(err).Msg("Failed to unmarshal the received share")
			return err
		}

		newSpaces.Mtime = utils.TSToTime(info.Mtime)
		c.addReceivedSpaces(userID, newSpaces)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

// Persist persists the data for one user to the storage
func (c *Cache) Persist(ctx context.Context, userID string) error {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Persist")
	defer span.End()
	span.SetAttributes(attribute.String("cs3.userid", userID))

	receivedSpaces := c.getReceivedSpaces(userID)
	if receivedSpaces == nil {
		return nil
	}

	oldMtime := receivedSpaces.getMtime()
	mTime := time.Now()
	receivedSpaces.setMtime(mTime)

	createdBytes, err := json.Marshal(receivedSpaces)
	if err != nil {
		receivedSpaces.setMtime(oldMtime)
		return err
	}
	jsonPath := userJSONPath(userID)
	if err := c.storage.MakeDirIfNotExist(ctx, path.Dir(jsonPath)); err != nil {
		receivedSpaces.setMtime(oldMtime)
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
		receivedSpaces.setMtime(oldMtime)
		return err
	}
	return nil
}

func (c *Cache) get(userID, spaceID, stateID string) (*Spaces, *Space, *State) {

	if userID == "" {
		return nil, nil, nil
	}

	receivedSpaces := c.getReceivedSpaces(userID)
	if receivedSpaces == nil {
		return nil, nil, nil
	}

	if spaceID == "" {
		return receivedSpaces, nil, nil
	}

	space := receivedSpaces.getSpace(spaceID)
	if space == nil {
		return receivedSpaces, nil, nil
	}

	if stateID == "" {
		return receivedSpaces, space, nil
	}

	state := space.getState(stateID)
	if state == nil {
		return receivedSpaces, space, nil
	}

	return receivedSpaces, space, state
}

func userJSONPath(userID string) string {
	return filepath.Join("/users", userID, "received.json")
}
