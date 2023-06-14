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

package jsoncs3

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	gatewayv1beta1 "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	userv1beta1 "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	rpcv1beta1 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	collaboration "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/go-micro/plugins/v4/events/natsjs"
	"github.com/google/uuid"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/codes"
	"golang.org/x/sync/errgroup"
	"google.golang.org/genproto/protobuf/field_mask"

	"github.com/cs3org/reva/v2/pkg/appctx"
	ctxpkg "github.com/cs3org/reva/v2/pkg/ctx"
	"github.com/cs3org/reva/v2/pkg/errtypes"
	"github.com/cs3org/reva/v2/pkg/events"
	"github.com/cs3org/reva/v2/pkg/events/stream"
	"github.com/cs3org/reva/v2/pkg/rgrpc/todo/pool"
	revaShare "github.com/cs3org/reva/v2/pkg/share"
	"github.com/cs3org/reva/v2/pkg/share/manager/jsoncs3/bucket"
	managerShareID "github.com/cs3org/reva/v2/pkg/share/manager/jsoncs3/shareid"
	"github.com/cs3org/reva/v2/pkg/share/manager/registry"
	"github.com/cs3org/reva/v2/pkg/storage/utils/metadata" // nolint:staticcheck // we need the legacy package to convert V1 to V2 messages
	"github.com/cs3org/reva/v2/pkg/storagespace"
	"github.com/cs3org/reva/v2/pkg/utils"
)

/*
  The sharded json driver splits the json file per storage space. Similar to fileids shareIDs are prefixed with the spaceID for easier lookup.
  In addition to the space json the share manager keeps lists for users and groups to cache their lists of created and received shares
  and to hold the state of received shares.

  FAQ
  Q: Why not split shares by user and have a list per user?
  A: While shares are created by users, they are persisted as grants on a file.
     If we persist shares by their creator/owner they would vanish if a user is deprovisioned: shares
	 in project spaces could not be managed collaboratively.
	 By splitting by space, we are in fact not only splitting by user, but more granular, per space.


  File structure in the jsoncs3 space:

  /storages/{storageid}/{spaceID.json} 	// contains the share information of all shares in that space
  /users/{userID}/created.json			// points to the spaces the user created shares in, including the list of shares
  /users/{userID}/received.json			// holds the accepted/pending state and mount point of received shares for users
  /groups/{groupID}/received.json		// points to the spaces the group has received shares in including the list of shares

  Example:
  	├── groups
  	│	└── group1
  	│		└── received.json
  	├── storages
  	│	└── storageid
  	│		└── spaceID.json
  	└── users
   		├── admin
 		│	└── created.json
 		└── einstein
 			└── received.json

  Whenever a share is created, the share manager has to
  1. update the /storages/{storageid}/{spaceID}.json file,
  2. create /users/{userID}/created.json if it doesn't exist yet and add the space/share
  3. create /users/{userID}/received.json or /groups/{groupID}/received.json if it doesn exist yet and add the space/share

  When updating shares /storages/{storageid}/{spaceID}.json is updated accordingly. The mtime is used to invalidate in-memory caches:
  - TODO the upload is tried with an if-unmodified-since header
  - TODO when if fails, the {spaceID}.json file is downloaded, the changes are reapplied and the upload is retried with the new mtime

  When updating received shares the mountpoint and state are updated in /users/{userID}/received.json (for both user and group shares).

  When reading the list of received shares the /users/{userID}/received.json file and the /groups/{groupID}/received.json files are statted.
  - if the mtime changed we download the file to update the local cache

  When reading the list of created shares the /users/{userID}/created.json file is statted
  - if the mtime changed we download the file to update the local cache
*/

// name is the Tracer name used to identify this instrumentation library.
const tracerName = "jsoncs3"

func init() {
	registry.Register("jsoncs3", NewDefault)
}

type config struct {
	GatewayAddr       string       `mapstructure:"gateway_addr"`
	MaxConcurrency    int          `mapstructure:"max_concurrency"`
	ProviderAddr      string       `mapstructure:"provider_addr"`
	ServiceUserID     string       `mapstructure:"service_user_id"`
	ServiceUserIdp    string       `mapstructure:"service_user_idp"`
	MachineAuthAPIKey string       `mapstructure:"machine_auth_apikey"`
	CacheTTL          int          `mapstructure:"ttl"`
	Events            EventOptions `mapstructure:"events"`
}

// EventOptions are the configurable options for events
type EventOptions struct {
	NatsAddress          string `mapstructure:"natsaddress"`
	NatsClusterID        string `mapstructure:"natsclusterid"`
	TLSInsecure          bool   `mapstructure:"tlsinsecure"`
	TLSRootCACertificate string `mapstructure:"tlsrootcacertificate"`
}

// Manager implements a share manager using a cs3 storage backend with local caching
type Manager struct {
	sync.RWMutex

	CreatorBucket        bucket.CreatorBucket        // holds the list of shares a user has created, sharded by user id
	GroupRecipientBucket bucket.GroupRecipientBucket // holds the list of shares a group has access to, sharded by group id
	ProviderBucket       bucket.ProviderBucket       // holds all shares, sharded by provider id and space id
	UserRecipientBucket  bucket.UserRecipientBucket  // holds the state of shares a user has received, sharded by user id

	storage   metadata.Storage
	SpaceRoot *provider.ResourceId

	initialized bool

	MaxConcurrency int

	gateway     gatewayv1beta1.GatewayAPIClient
	eventStream events.Stream
}

// NewDefault returns a new manager instance with default dependencies
func NewDefault(m map[string]interface{}) (revaShare.Manager, error) {
	c := &config{}
	if err := mapstructure.Decode(m, c); err != nil {
		err = errors.Wrap(err, "error creating a new manager")
		return nil, err
	}

	s, err := metadata.NewCS3Storage(c.ProviderAddr, c.ProviderAddr, c.ServiceUserID, c.ServiceUserIdp, c.MachineAuthAPIKey)
	if err != nil {
		return nil, err
	}

	gc, err := pool.GetGatewayServiceClient(c.GatewayAddr)
	if err != nil {
		return nil, err
	}

	var es events.Stream
	if c.Events.NatsAddress != "" {
		evtsCfg := c.Events
		var (
			rootCAPool *x509.CertPool
			tlsConf    *tls.Config
		)
		if evtsCfg.TLSRootCACertificate != "" {
			rootCrtFile, err := os.Open(evtsCfg.TLSRootCACertificate)
			if err != nil {
				return nil, err
			}

			var certBytes bytes.Buffer
			if _, err := io.Copy(&certBytes, rootCrtFile); err != nil {
				return nil, err
			}

			rootCAPool = x509.NewCertPool()
			rootCAPool.AppendCertsFromPEM(certBytes.Bytes())
			evtsCfg.TLSInsecure = false

			tlsConf = &tls.Config{
				InsecureSkipVerify: evtsCfg.TLSInsecure, //nolint:gosec
				RootCAs:            rootCAPool,
			}
		}

		es, err = stream.Nats(
			natsjs.TLSConfig(tlsConf),
			natsjs.Address(evtsCfg.NatsAddress),
			natsjs.ClusterID(evtsCfg.NatsClusterID),
		)
		if err != nil {
			return nil, err
		}
	}

	return New(s, gc, c.CacheTTL, es, c.MaxConcurrency)
}

// New returns a new manager instance.
func New(s metadata.Storage, gc gatewayv1beta1.GatewayAPIClient, ttlSeconds int, es events.Stream, maxconcurrency int) (*Manager, error) {
	//ttl := time.Duration(ttlSeconds) * time.Second

	return &Manager{
		CreatorBucket:        bucket.NewCreatorBucket(s),
		GroupRecipientBucket: bucket.NewGroupRecipientBucket(s),
		ProviderBucket:       bucket.NewProviderBucket(s),
		UserRecipientBucket:  bucket.NewUserRecipientBucket(s),
		storage:              s,
		gateway:              gc,
		eventStream:          es,
		MaxConcurrency:       maxconcurrency,
	}, nil
}

func (m *Manager) initialize(ctx context.Context) error {
	_, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "initialize")
	defer span.End()
	if m.initialized {
		span.SetStatus(codes.Ok, "already initialized")
		return nil
	}

	m.Lock()
	defer m.Unlock()

	if m.initialized { // check if initialization happened while grabbing the lock
		span.SetStatus(codes.Ok, "initialized while grabbing lock")
		return nil
	}

	ctx = context.Background()
	err := m.storage.Init(ctx, "jsoncs3-share-manager-metadata")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	err = m.storage.MakeDirIfNotExist(ctx, "storages")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	err = m.storage.MakeDirIfNotExist(ctx, "users")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	err = m.storage.MakeDirIfNotExist(ctx, "groups")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	m.initialized = true
	span.SetStatus(codes.Ok, "initialized")

	return nil
}

// Share creates a new share
func (m *Manager) Share(ctx context.Context, md *provider.ResourceInfo, g *collaboration.ShareGrant) (*collaboration.Share, error) {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Share")
	defer span.End()

	if err := m.initialize(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	user := ctxpkg.ContextMustGetUser(ctx)
	ts := utils.TSNow()

	// do not allow share to myself or the owner if share is for a user
	// TODO: should this not already be caught at the gw level?
	if g.Grantee.Type == provider.GranteeType_GRANTEE_TYPE_USER &&
		(utils.UserEqual(g.Grantee.GetUserId(), user.Id) || utils.UserEqual(g.Grantee.GetUserId(), md.Owner)) {
		err := errtypes.BadRequest("jsoncs3: owner/creator and grantee are the same")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	// check if share already exists.
	key := &collaboration.ShareKey{
		ResourceId: md.Id,
		Grantee:    g.Grantee,
	}

	_, err := m.getByKey(ctx, key)
	if err == nil {
		// share already exists
		err := errtypes.AlreadyExists(key.String())
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	shareID := managerShareID.Encode(md.GetId().GetStorageId(), md.GetId().GetSpaceId(), uuid.NewString())
	s := &collaboration.Share{
		Id: &collaboration.ShareId{
			OpaqueId: shareID,
		},
		ResourceId:  md.Id,
		Permissions: g.Permissions,
		Grantee:     g.Grantee,
		Expiration:  g.Expiration,
		Owner:       md.Owner,
		Creator:     user.Id,
		Ctime:       ts,
		Mtime:       ts,
	}

	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		err = m.ProviderBucket.Upsert(ctx, s)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())

			return err
		}

		return nil
	})

	eg.Go(func() error {
		if err := m.CreatorBucket.Upsert(ctx, s.GetCreator().GetOpaqueId(), shareID); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}

		return nil
	})

	if g.Grantee.Type == provider.GranteeType_GRANTEE_TYPE_USER {
		eg.Go(func() error {
			userID := g.Grantee.GetUserId().GetOpaqueId()
			rs := &collaboration.ReceivedShare{
				Share: s,
				State: collaboration.ShareState_SHARE_STATE_PENDING,
			}

			if err := m.UserRecipientBucket.Upsert(ctx, userID, rs); err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())

				return err
			}

			return nil
		})
	}

	if g.Grantee.Type == provider.GranteeType_GRANTEE_TYPE_GROUP {
		eg.Go(func() error {
			groupID := g.Grantee.GetGroupId().GetOpaqueId()

			if err := m.GroupRecipientBucket.Upsert(ctx, groupID, shareID); err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())

				return err
			}

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	span.SetStatus(codes.Ok, "")

	return s, nil
}

func (m *Manager) getByID(ctx context.Context, id *collaboration.ShareId) (*collaboration.Share, error) {
	providerItem, _ := m.ProviderBucket.Get(ctx, id.OpaqueId)
	if providerItem == nil {
		return nil, errtypes.NotFound(id.String())
	}

	return providerItem.Share, nil
}

func (m *Manager) getByKey(ctx context.Context, key *collaboration.ShareKey) (*collaboration.Share, error) {
	providerItems, _ := m.ProviderBucket.Find(ctx, key.ResourceId.StorageId, key.ResourceId.SpaceId)

	for _, providerItem := range providerItems {
		share := providerItem.Share

		if utils.GranteeEqual(key.Grantee, share.Grantee) && utils.ResourceIDEqual(share.ResourceId, key.ResourceId) {
			return share, nil
		}
	}

	return nil, errtypes.NotFound(key.String())
}

func (m *Manager) getShare(ctx context.Context, ref *collaboration.ShareReference) (*collaboration.Share, error) {
	if ref.GetId() != nil {
		return m.getByID(ctx, ref.GetId())
	}

	if ref.GetKey() != nil {
		return m.getByKey(ctx, ref.GetKey())
	}

	return nil, errtypes.NotFound(ref.String())
}

// GetShare gets the information for a share by the given ref.
func (m *Manager) GetShare(ctx context.Context, ref *collaboration.ShareReference) (*collaboration.Share, error) {
	log := appctx.GetLogger(ctx)

	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "GetShare")
	defer span.End()

	if err := m.initialize(ctx); err != nil {
		return nil, err
	}

	share, err := m.getShare(ctx, ref)
	if err != nil {
		return nil, err
	}

	if revaShare.IsExpired(share) {
		if err := m.removeShare(ctx, share); err != nil {
			log.Error().Err(err).Msg("failed to unshare expired share")
		}

		if err := events.Publish(m.eventStream, events.ShareExpired{
			ShareID:        share.GetId(),
			ShareOwner:     share.GetOwner(),
			ItemID:         share.GetResourceId(),
			ExpiredAt:      time.Unix(int64(share.GetExpiration().GetSeconds()), int64(share.GetExpiration().GetNanos())),
			GranteeUserID:  share.GetGrantee().GetUserId(),
			GranteeGroupID: share.GetGrantee().GetGroupId(),
		}); err != nil {
			log.Error().Err(err).Msg("failed to publish share expired event")
		}
	}

	// check if we are the creator or the grantee
	// TODO allow manager to get shares in a space created by other users
	user := ctxpkg.ContextMustGetUser(ctx)
	if revaShare.IsCreatedByUser(share, user) || revaShare.IsGrantedToUser(share, user) {
		return share, nil
	}

	res, err := m.gateway.Stat(ctx, &provider.StatRequest{
		Ref: &provider.Reference{ResourceId: share.ResourceId},
	})

	if err == nil && res.Status.Code == rpcv1beta1.Code_CODE_OK && res.Info.PermissionSet.ListGrants {
		return share, nil
	}

	// we return not found to not disclose information
	return nil, errtypes.NotFound(ref.String())
}

// Unshare deletes a share
func (m *Manager) Unshare(ctx context.Context, ref *collaboration.ShareReference) error {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "Unshare")
	defer span.End()

	if err := m.initialize(ctx); err != nil {
		return err
	}

	user := ctxpkg.ContextMustGetUser(ctx)

	share, err := m.getShare(ctx, ref)
	if err != nil {
		return err
	}

	// TODO allow manager to unshare shares in a space created by other users
	if !revaShare.IsCreatedByUser(share, user) {
		// TODO why not permission denied?
		return errtypes.NotFound(ref.String())
	}

	return m.removeShare(ctx, share)
}

// UpdateShare updates the mode of the given share.
func (m *Manager) UpdateShare(ctx context.Context, shareReference *collaboration.ShareReference, sharePermissions *collaboration.SharePermissions, updated *collaboration.Share, fieldMask *field_mask.FieldMask) (*collaboration.Share, error) {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "UpdateShare")
	defer span.End()

	if err := m.initialize(ctx); err != nil {
		return nil, err
	}

	var share *collaboration.Share

	if shareReference != nil {
		var err error

		share, err = m.getShare(ctx, shareReference)
		if err != nil {
			return nil, err
		}
	} else if updated != nil {
		var err error

		share, err = m.getByID(ctx, updated.Id)
		if err != nil {
			return nil, err
		}
	}

	if fieldMask != nil {
		for i := range fieldMask.Paths {
			switch fieldMask.Paths[i] {
			case "permissions":
				share.Permissions = updated.Permissions
			case "expiration":
				share.Expiration = updated.Expiration
			default:
				return nil, errtypes.NotSupported("updating " + fieldMask.Paths[i] + " is not supported")
			}
		}
	}

	if !revaShare.IsCreatedByUser(share, ctxpkg.ContextMustGetUser(ctx)) {
		res, err := m.gateway.Stat(ctx, &provider.StatRequest{
			Ref: &provider.Reference{ResourceId: share.ResourceId},
		})
		if err != nil || res.Status.Code != rpcv1beta1.Code_CODE_OK || !res.Info.PermissionSet.UpdateGrant {
			return nil, errtypes.NotFound(shareReference.String())
		}
	}

	if sharePermissions != nil {
		share.Permissions = sharePermissions
	}

	share.Mtime = utils.TSNow()

	if err := m.ProviderBucket.Upsert(ctx, share); err != nil {
		return nil, err
	}

	return share, nil
}

// ListShares returns the shares created by the user
func (m *Manager) ListShares(ctx context.Context, filters []*collaboration.Filter) ([]*collaboration.Share, error) {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "ListShares")
	defer span.End()

	if err := m.initialize(ctx); err != nil {
		return nil, err
	}

	user := ctxpkg.ContextMustGetUser(ctx)

	if len(revaShare.FilterFiltersByType(filters, collaboration.Filter_TYPE_RESOURCE_ID)) > 0 {
		return m.listSharesByIDs(ctx, user, filters)
	}

	return m.listCreatedShares(ctx, user, filters)
}

func (m *Manager) listSharesByIDs(ctx context.Context, user *userv1beta1.User, filters []*collaboration.Filter) ([]*collaboration.Share, error) {
	log := appctx.GetLogger(ctx)

	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "listSharesByIDs")
	defer span.End()

	providerSpaces := make(map[string]map[string]struct{})
	for _, f := range revaShare.FilterFiltersByType(filters, collaboration.Filter_TYPE_RESOURCE_ID) {
		storageID := f.GetResourceId().GetStorageId()
		spaceID := f.GetResourceId().GetSpaceId()

		if providerSpaces[storageID] == nil {
			providerSpaces[storageID] = make(map[string]struct{})
		}

		providerSpaces[storageID][spaceID] = struct{}{}
	}

	var shares []*collaboration.Share

	statCache := make(map[string]struct{})
	for providerID, spaces := range providerSpaces {
		for spaceID := range spaces {
			providerItems, _ := m.ProviderBucket.Find(ctx, providerID, spaceID)

			for _, providerItem := range providerItems {
				share := providerItem.Share

				if revaShare.IsExpired(share) {
					if err := m.removeShare(ctx, share); err != nil {
						log.Error().Err(err).Msg("failed to unshare expired share")
					}

					if err := events.Publish(m.eventStream, events.ShareExpired{
						ShareOwner:     share.GetOwner(),
						ItemID:         share.GetResourceId(),
						ExpiredAt:      time.Unix(int64(share.GetExpiration().GetSeconds()), int64(share.GetExpiration().GetNanos())),
						GranteeUserID:  share.GetGrantee().GetUserId(),
						GranteeGroupID: share.GetGrantee().GetGroupId(),
					}); err != nil {
						log.Error().Err(err).Msg("failed to publish share expired event")
					}

					continue
				}

				if !revaShare.MatchesFilters(share, filters) {
					continue
				}

				if !(revaShare.IsCreatedByUser(share, user) || revaShare.IsGrantedToUser(share, user)) {
					key := storagespace.FormatResourceID(*share.ResourceId)

					if _, hit := statCache[key]; !hit {
						res, err := m.gateway.Stat(ctx, &provider.StatRequest{
							Ref: &provider.Reference{ResourceId: share.ResourceId},
						})
						if err != nil || res.Status.Code != rpcv1beta1.Code_CODE_OK || !res.Info.PermissionSet.ListGrants {
							continue
						}

						statCache[key] = struct{}{}
					}
				}

				shares = append(shares, share)
			}
		}
	}

	span.SetStatus(codes.Ok, "")

	return shares, nil
}

func (m *Manager) listCreatedShares(ctx context.Context, user *userv1beta1.User, filters []*collaboration.Filter) ([]*collaboration.Share, error) {
	log := appctx.GetLogger(ctx)

	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "listCreatedShares")
	defer span.End()

	var shares []*collaboration.Share

	groupRecipientItems, _ := m.CreatorBucket.Find(ctx, user.Id.OpaqueId)
	for _, creatorItem := range groupRecipientItems {
		providerItem, _ := m.ProviderBucket.Get(ctx, creatorItem.ShareReferenceId)
		if providerItem == nil {
			continue
		}

		share := providerItem.Share
		if revaShare.IsExpired(share) {
			if err := m.removeShare(ctx, share); err != nil {
				log.Error().Err(err).Msg("failed to unshare expired share")
			}

			if err := events.Publish(m.eventStream, events.ShareExpired{
				ShareOwner:     share.GetOwner(),
				ItemID:         share.GetResourceId(),
				ExpiredAt:      time.Unix(int64(share.GetExpiration().GetSeconds()), int64(share.GetExpiration().GetNanos())),
				GranteeUserID:  share.GetGrantee().GetUserId(),
				GranteeGroupID: share.GetGrantee().GetGroupId(),
			}); err != nil {
				log.Error().Err(err).Msg("failed to publish share expired event")
			}
			continue
		}

		if utils.UserEqual(user.GetId(), share.GetCreator()) && revaShare.MatchesFilters(share, filters) {
			shares = append(shares, share)
		}
	}

	span.SetStatus(codes.Ok, "")

	return shares, nil
}

// ListReceivedShares returns the list of shares the user has access to.
func (m *Manager) ListReceivedShares(ctx context.Context, filters []*collaboration.Filter) ([]*collaboration.ReceivedShare, error) {
	log := appctx.GetLogger(ctx)

	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "ListReceivedShares")
	defer span.End()

	if err := m.initialize(ctx); err != nil {
		return nil, err
	}

	user := ctxpkg.ContextMustGetUser(ctx)

	var recipientItems []*bucket.UserRecipientItem

	// first collect all spaceIDs the user has access to as a group member
	for _, group := range user.Groups {
		groupRecipientItems, _ := m.GroupRecipientBucket.Find(ctx, group)

		for _, groupRecipientItem := range groupRecipientItems {
			recipientItems = append(recipientItems, &bucket.UserRecipientItem{
				UserID:           groupRecipientItem.IdentityID,
				ShareReferenceId: groupRecipientItem.ShareReferenceId,
				State:            collaboration.ShareState_SHARE_STATE_PENDING,
				Mtime:            time.Now(),
			})
		}
	}

	userRecipientItems, _ := m.UserRecipientBucket.Find(ctx, user.Id.OpaqueId)
	for _, userRecipientItem := range userRecipientItems {
		recipientItems = append(recipientItems, userRecipientItem)
	}

	numWorkers := m.MaxConcurrency
	if numWorkers == 0 || len(recipientItems) < numWorkers {
		numWorkers = len(recipientItems)
	}

	type workItem struct {
		recipientItem *bucket.UserRecipientItem
	}

	work := make(chan workItem)
	results := make(chan *collaboration.ReceivedShare)

	eg, ctx := errgroup.WithContext(ctx)

	// Distribute work
	eg.Go(func() error {
		defer close(work)

		for _, recipientItem := range recipientItems {
			select {
			case work <- workItem{recipientItem}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		return nil
	})

	// Spawn workers that'll concurrently work the queue
	for i := 0; i < numWorkers; i++ {
		eg.Go(func() error {
			for w := range work {
				providerItem, _ := m.ProviderBucket.Get(ctx, w.recipientItem.ShareReferenceId)
				if providerItem == nil {
					continue
				}

				share := providerItem.Share
				if revaShare.IsExpired(share) {
					if err := m.removeShare(ctx, share); err != nil {
						log.Error().Err(err).Msg("failed to unshare expired share")
					}

					if err := events.Publish(m.eventStream, events.ShareExpired{
						ShareOwner:     share.GetOwner(),
						ItemID:         share.GetResourceId(),
						ExpiredAt:      time.Unix(int64(share.GetExpiration().GetSeconds()), int64(share.GetExpiration().GetNanos())),
						GranteeUserID:  share.GetGrantee().GetUserId(),
						GranteeGroupID: share.GetGrantee().GetGroupId(),
					}); err != nil {
						log.Error().Err(err).Msg("failed to publish share expired event")
					}

					continue
				}

				if revaShare.IsGrantedToUser(share, user) {
					if revaShare.MatchesFiltersWithState(share, w.recipientItem.State, filters) {
						rs := &collaboration.ReceivedShare{
							Share:      share,
							State:      w.recipientItem.State,
							MountPoint: w.recipientItem.MountPoint,
						}

						select {
						case results <- rs:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
				}
			}
			return nil
		})
	}

	// Wait for things to settle down, then close results chan
	go func() {
		_ = eg.Wait() // error is checked later
		close(results)
	}()

	receivedShares := []*collaboration.ReceivedShare{}
	for receivedShare := range results {
		receivedShares = append(receivedShares, receivedShare)
	}

	if err := eg.Wait(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil, err
	}

	span.SetStatus(codes.Ok, "")

	return receivedShares, nil
}

func (m *Manager) convertShareToReceivedShare(ctx context.Context, userID string, share *collaboration.Share) *collaboration.ReceivedShare {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "convert")
	defer span.End()

	receivedShare := &collaboration.ReceivedShare{
		Share: share,
		State: collaboration.ShareState_SHARE_STATE_PENDING,
	}

	if userRecipientItem, _ := m.UserRecipientBucket.Get(ctx, userID, share.Id.GetOpaqueId()); userRecipientItem != nil {
		receivedShare.State = userRecipientItem.State
		receivedShare.MountPoint = userRecipientItem.MountPoint
	}

	return receivedShare
}

// GetReceivedShare returns the information for a received share.
func (m *Manager) GetReceivedShare(ctx context.Context, ref *collaboration.ShareReference) (*collaboration.ReceivedShare, error) {
	if err := m.initialize(ctx); err != nil {
		return nil, err
	}

	return m.getReceivedShare(ctx, ref)
}

func (m *Manager) getReceivedShare(ctx context.Context, shareReference *collaboration.ShareReference) (*collaboration.ReceivedShare, error) {
	log := appctx.GetLogger(ctx)

	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "getReceived")
	defer span.End()

	share, err := m.getShare(ctx, shareReference)
	if err != nil {
		return nil, err
	}

	user := ctxpkg.ContextMustGetUser(ctx)

	if !revaShare.IsGrantedToUser(share, user) {
		return nil, errtypes.NotFound(shareReference.String())
	}

	if revaShare.IsExpired(share) {
		if err := m.removeShare(ctx, share); err != nil {
			log.Error().Err(err).Msg("failed to unshare expired share")
		}

		if err := events.Publish(m.eventStream, events.ShareExpired{
			ShareOwner:     share.GetOwner(),
			ItemID:         share.GetResourceId(),
			ExpiredAt:      time.Unix(int64(share.GetExpiration().GetSeconds()), int64(share.GetExpiration().GetNanos())),
			GranteeUserID:  share.GetGrantee().GetUserId(),
			GranteeGroupID: share.GetGrantee().GetGroupId(),
		}); err != nil {
			log.Error().Err(err).Msg("failed to publish share expired event")
		}
	}

	return m.convertShareToReceivedShare(ctx, user.Id.GetOpaqueId(), share), nil
}

// UpdateReceivedShare updates the received share with share state.
func (m *Manager) UpdateReceivedShare(ctx context.Context, receivedShare *collaboration.ReceivedShare, fieldMask *field_mask.FieldMask) (*collaboration.ReceivedShare, error) {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "UpdateReceivedShare")
	defer span.End()

	if err := m.initialize(ctx); err != nil {
		return nil, err
	}

	rs, err := m.getReceivedShare(ctx, &collaboration.ShareReference{Spec: &collaboration.ShareReference_Id{Id: receivedShare.Share.Id}})
	if err != nil {
		return nil, err
	}

	for i := range fieldMask.Paths {
		switch fieldMask.Paths[i] {
		case "state":
			rs.State = receivedShare.State
		case "mount_point":
			rs.MountPoint = receivedShare.MountPoint
		default:
			return nil, errtypes.NotSupported("updating " + fieldMask.Paths[i] + " is not supported")
		}
	}

	// write back
	if err := m.UserRecipientBucket.Upsert(ctx, ctxpkg.ContextMustGetUser(ctx).GetId().GetOpaqueId(), rs); err != nil {
		return nil, err
	}

	return rs, nil
}

func shareIsRoutable(share *collaboration.Share) bool {
	return strings.Contains(share.Id.OpaqueId, managerShareID.IDDelimiter)
}

func updateShareID(share *collaboration.Share) {
	share.Id.OpaqueId = managerShareID.Encode(share.ResourceId.StorageId, share.ResourceId.SpaceId, share.Id.OpaqueId)
}

// Load imports shares and received shares from channels (e.g. during migration)
func (m *Manager) Load(ctx context.Context, shareChan <-chan *collaboration.Share, receivedShareChan <-chan revaShare.ReceivedShareWithUser) error {
	log := appctx.GetLogger(ctx)

	if err := m.initialize(ctx); err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		for share := range shareChan {
			if share == nil {
				continue
			}

			if !shareIsRoutable(share) {
				updateShareID(share)
			}

			if err := m.ProviderBucket.Upsert(context.Background(), share); err != nil {
				log.Error().Err(err).Interface("share", share).Msg("error persisting share")
			} else {
				log.Debug().Str("storageid", share.GetResourceId().GetStorageId()).Str("spaceID", share.GetResourceId().GetSpaceId()).Str("shareID", share.Id.OpaqueId).Msg("imported share")
			}

			if err := m.CreatorBucket.Upsert(ctx, share.GetCreator().GetOpaqueId(), share.Id.OpaqueId); err != nil {
				log.Error().Err(err).Interface("share", share).Msg("error persisting created cache")
			} else {
				log.Debug().Str("creatorid", share.GetCreator().GetOpaqueId()).Str("shareID", share.Id.OpaqueId).Msg("updated created cache")
			}
		}
		wg.Done()
	}()

	go func() {
		for share := range receivedShareChan {
			if share.ReceivedShare == nil {
				continue
			}

			if !shareIsRoutable(share.ReceivedShare.GetShare()) {
				updateShareID(share.ReceivedShare.GetShare())
			}

			switch share.ReceivedShare.Share.Grantee.Type {
			case provider.GranteeType_GRANTEE_TYPE_USER:
				if err := m.UserRecipientBucket.Upsert(context.Background(), share.ReceivedShare.GetShare().GetGrantee().GetUserId().GetOpaqueId(), share.ReceivedShare); err != nil {
					log.Error().
						Err(err).
						Interface("received share", share).
						Msg("error persisting received share for user")
				} else {
					log.Debug().
						Str("userID", share.ReceivedShare.GetShare().GetGrantee().GetUserId().GetOpaqueId()).
						Str("spaceID", share.ReceivedShare.GetShare().GetResourceId().GetSpaceId()).
						Str("shareID", share.ReceivedShare.GetShare().Id.OpaqueId).
						Msg("updated received share userdata")
				}
			case provider.GranteeType_GRANTEE_TYPE_GROUP:
				if err := m.GroupRecipientBucket.Upsert(
					context.Background(),
					share.ReceivedShare.GetShare().GetGrantee().GetGroupId().GetOpaqueId(),
					share.ReceivedShare.GetShare().GetId().GetOpaqueId(),
				); err != nil {
					log.Error().
						Err(err).
						Interface("received share", share).
						Msg("error persisting received share to group cache")
				} else {
					log.Debug().
						Str("groupID", share.ReceivedShare.GetShare().GetGrantee().GetGroupId().GetOpaqueId()).
						Str("shareID", share.ReceivedShare.GetShare().Id.OpaqueId).
						Msg("updated received share group cache")
				}
			}

		}
		wg.Done()
	}()

	wg.Wait()

	return nil
}

func (m *Manager) removeShare(ctx context.Context, s *collaboration.Share) error {
	ctx, span := appctx.GetTracerProvider(ctx).Tracer(tracerName).Start(ctx, "removeShare")
	defer span.End()

	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		return m.ProviderBucket.Delete(ctx, s.Id.OpaqueId)
	})

	eg.Go(func() error {
		return m.CreatorBucket.Delete(ctx, s.GetCreator().GetOpaqueId(), s.Id.OpaqueId)
	})

	eg.Go(func() error {
		return m.GroupRecipientBucket.Delete(ctx, s.GetGrantee().GetGroupId().GetOpaqueId(), s.Id.OpaqueId)
	})

	eg.Go(func() error {
		return m.UserRecipientBucket.Delete(ctx, s.GetGrantee().GetUserId().GetOpaqueId(), s.Id.OpaqueId)
	})

	return eg.Wait()
}
