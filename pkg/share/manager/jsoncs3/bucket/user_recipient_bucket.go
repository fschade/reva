package bucket

import (
	"context"
	"path/filepath"

	cs3SharingCollaboration "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	"github.com/pkg/errors"

	"github.com/cs3org/reva/v2/pkg/share/manager/jsoncs3/remote"
	"github.com/cs3org/reva/v2/pkg/storage/utils/metadata"
)

func NewUserRecipientBucket(s metadata.Storage) UserRecipientBucket {
	return UserRecipientBucket{
		manager: remote.NewManager[[]*UserRecipientItem](s),
	}
}

type UserRecipientBucket struct {
	cache   Cache[*UserRecipientItem]
	manager remote.Manager[[]*UserRecipientItem]
}

func (urb *UserRecipientBucket) Upsert(ctx context.Context, userID string, share *cs3SharingCollaboration.ReceivedShare) error {
	jsonPath := filepath.Join("/users", userID, "received.json")

	r, mu, err := urb.manager.Get(ctx, jsonPath)
	if err != nil {
		return err
	}

	dataOP := func() ([]*UserRecipientItem, error) {
		item := &UserRecipientItem{
			UserID:           userID,
			ShareReferenceId: share.Share.Id.OpaqueId,
			State:            share.State,
			MountPoint:       share.MountPoint,
		}

		urb.cache.Upsert(userID+share.Share.Id.OpaqueId, item)

		return urb.cache.Find(userID), nil
	}

	updateOP := func(remoteItems []*UserRecipientItem) error {

		for _, localItem := range urb.cache.Find(userID) {
			urb.cache.Delete(localItem.UserID + localItem.ShareReferenceId)
		}

		for _, remoteItem := range remoteItems {
			urb.cache.Upsert(remoteItem.UserID+remoteItem.ShareReferenceId, remoteItem)
		}

		return nil
	}

	resetOP := func(err error) error {
		urb.cache.Delete(userID + share.Share.Id.OpaqueId)

		return err
	}

	mu.Lock()
	defer mu.Unlock()

	if err = r.Push(ctx, dataOP); errors.Is(err, remote.ErrMustPull) {
		return r.Merge(ctx, dataOP, updateOP, resetOP)
	}

	return err
}

func (urb *UserRecipientBucket) Get(ctx context.Context, userID, shareReferenceId string) (*UserRecipientItem, error) {
	jsonPath := filepath.Join("/users", userID, "received.json")

	r, mu, err := urb.manager.Get(ctx, jsonPath)
	if err != nil {
		return nil, err
	}

	updateOP := func(remoteItems []*UserRecipientItem) error {
		for _, localItem := range urb.cache.Find(userID) {
			urb.cache.Delete(localItem.UserID + localItem.ShareReferenceId)
		}

		for _, remoteItem := range remoteItems {
			urb.cache.Upsert(remoteItem.UserID+remoteItem.ShareReferenceId, remoteItem)
		}

		return nil
	}

	mu.RLock()
	defer mu.RUnlock()

	err = r.Pull(ctx, updateOP)
	if err != nil {
		return nil, err
	}

	item, ok := urb.cache.Get(userID + shareReferenceId)
	if !ok {
		return nil, nil
	}

	return item, nil
}

func (urb *UserRecipientBucket) Delete(ctx context.Context, userID, shareReferenceId string) error {
	jsonPath := filepath.Join("/users", userID, "received.json")

	r, mu, err := urb.manager.Get(ctx, jsonPath)
	if err != nil {
		return err
	}

	dataOP := func() ([]*UserRecipientItem, error) {
		urb.cache.Delete(userID + shareReferenceId)

		return urb.cache.Find(userID), nil
	}

	updateOP := func(remoteItems []*UserRecipientItem) error {
		for _, localItem := range urb.cache.Find(userID) {
			urb.cache.Delete(localItem.UserID + localItem.ShareReferenceId)
		}

		for _, remoteItem := range remoteItems {
			urb.cache.Upsert(remoteItem.UserID+remoteItem.ShareReferenceId, remoteItem)
		}

		return nil
	}

	resetOP := func(err error) error {
		// fixme: add item back
		return err
	}

	mu.Lock()
	defer mu.Unlock()

	if err = r.Push(ctx, dataOP); errors.Is(err, remote.ErrMustPull) {
		return r.Merge(ctx, dataOP, updateOP, resetOP)
	}

	return err
}

func (urb *UserRecipientBucket) Find(ctx context.Context, userID string) ([]*UserRecipientItem, error) {
	jsonPath := filepath.Join("/users", userID, "received.json")

	r, mu, err := urb.manager.Get(ctx, jsonPath)
	if err != nil {
		return nil, err
	}

	updateOP := func(remoteItems []*UserRecipientItem) error {
		for _, localItem := range urb.cache.Find(userID) {
			urb.cache.Delete(localItem.UserID + localItem.ShareReferenceId)
		}

		for _, remoteItem := range remoteItems {
			urb.cache.Upsert(remoteItem.UserID+remoteItem.ShareReferenceId, remoteItem)
		}

		return nil
	}

	mu.RLock()
	defer mu.RUnlock()

	err = r.Pull(ctx, updateOP)
	if err != nil {
		return nil, err
	}

	return urb.cache.Find(userID), nil
}
