package bucket

import (
	"context"
	"path/filepath"

	cs3SharingCollaboration "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	"github.com/pkg/errors"

	"github.com/cs3org/reva/v2/pkg/share/manager/jsoncs3/remote"
	"github.com/cs3org/reva/v2/pkg/share/manager/jsoncs3/shareid"
	"github.com/cs3org/reva/v2/pkg/storage/utils/metadata"
)

func NewProviderBucket(s metadata.Storage) ProviderBucket {
	return ProviderBucket{
		manager: remote.NewManager[[]*ProviderItem](s),
	}
}

type ProviderBucket struct {
	cache   Cache[*ProviderItem]
	manager remote.Manager[[]*ProviderItem]
}

func (pb *ProviderBucket) Upsert(ctx context.Context, share *cs3SharingCollaboration.Share) error {
	storageID, spaceID, _ := shareid.Decode(share.Id.OpaqueId)
	jsonPath := filepath.Join("/storages", storageID, spaceID+".json")

	r, mu, err := pb.manager.Get(ctx, jsonPath)
	if err != nil {
		return err
	}

	dataOP := func() ([]*ProviderItem, error) {
		item := &ProviderItem{
			Share: share,
		}

		pb.cache.Upsert(share.Id.OpaqueId, item)

		return pb.cache.Find(storageID + shareid.IDDelimiter + spaceID), nil
	}

	updateOP := func(remoteItems []*ProviderItem) error {
		for _, localItem := range pb.cache.Find(storageID + shareid.IDDelimiter + spaceID) {
			pb.cache.Delete(localItem.Share.Id.OpaqueId)
		}

		for _, remoteItem := range remoteItems {
			pb.cache.Upsert(remoteItem.Share.Id.OpaqueId, remoteItem)
		}

		return nil
	}

	resetOP := func(err error) error {
		pb.cache.Delete(share.Id.OpaqueId)

		return err
	}

	mu.Lock()
	defer mu.Unlock()

	if err = r.Push(ctx, dataOP); errors.Is(err, remote.ErrMustPull) {
		return r.Merge(ctx, dataOP, updateOP, resetOP)
	}

	return err
}

func (pb *ProviderBucket) Get(ctx context.Context, shareReferenceId string) (*ProviderItem, error) {
	storageID, spaceID, _ := shareid.Decode(shareReferenceId)
	jsonPath := filepath.Join("/storages", storageID, spaceID+".json")

	r, mu, err := pb.manager.Get(ctx, jsonPath)
	if err != nil {
		return nil, err
	}

	updateOP := func(remoteItems []*ProviderItem) error {
		for _, localItem := range pb.cache.Find(storageID + shareid.IDDelimiter + spaceID) {
			pb.cache.Delete(localItem.Share.Id.OpaqueId)
		}

		for _, remoteItem := range remoteItems {
			pb.cache.Upsert(remoteItem.Share.Id.OpaqueId, remoteItem)
		}

		return nil
	}

	mu.RLock()
	defer mu.RUnlock()

	err = r.Pull(ctx, updateOP)
	if err != nil {
		return nil, err
	}

	item, ok := pb.cache.Get(shareReferenceId)
	if !ok {
		return nil, nil
	}

	return item, nil
}

func (pb *ProviderBucket) Delete(ctx context.Context, shareReferenceId string) error {
	storageID, spaceID, _ := shareid.Decode(shareReferenceId)
	jsonPath := filepath.Join("/storages", storageID, spaceID+".json")

	r, mu, err := pb.manager.Get(ctx, jsonPath)
	if err != nil {
		return err
	}

	dataOP := func() ([]*ProviderItem, error) {
		pb.cache.Delete(shareReferenceId)

		return pb.cache.Find(storageID + shareid.IDDelimiter + spaceID), nil
	}

	updateOP := func(remoteItems []*ProviderItem) error {
		for _, localItem := range pb.cache.Find(storageID + shareid.IDDelimiter + spaceID) {
			pb.cache.Delete(localItem.Share.Id.OpaqueId)
		}

		for _, remoteItem := range remoteItems {
			pb.cache.Upsert(remoteItem.Share.Id.OpaqueId, remoteItem)
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

func (pb *ProviderBucket) Find(ctx context.Context, storageID, spaceID string) ([]*ProviderItem, error) {
	jsonPath := filepath.Join("/storages", storageID, spaceID+".json")

	r, mu, err := pb.manager.Get(ctx, jsonPath)
	if err != nil {
		return nil, err
	}

	updateOP := func(remoteItems []*ProviderItem) error {
		for _, localItem := range pb.cache.Find(storageID + shareid.IDDelimiter + spaceID) {
			pb.cache.Delete(localItem.Share.Id.OpaqueId)
		}

		for _, remoteItem := range remoteItems {
			pb.cache.Upsert(remoteItem.Share.Id.OpaqueId, remoteItem)
		}

		return nil
	}

	mu.RLock()
	defer mu.RUnlock()

	err = r.Pull(ctx, updateOP)
	if err != nil {
		return nil, err
	}

	return pb.cache.Find(storageID + shareid.IDDelimiter + spaceID), nil
}
