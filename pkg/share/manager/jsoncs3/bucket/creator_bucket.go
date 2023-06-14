package bucket

import (
	"context"
	"path/filepath"

	"github.com/pkg/errors"

	"github.com/cs3org/reva/v2/pkg/share/manager/jsoncs3/remote"
	"github.com/cs3org/reva/v2/pkg/storage/utils/metadata"
)

func NewCreatorBucket(s metadata.Storage) CreatorBucket {
	return CreatorBucket{
		fileNamespace: "users",
		fileName:      "created.json",
		manager:       remote.NewManager[[]*CreatorItem](s),
	}
}

type CreatorBucket struct {
	fileNamespace string
	fileName      string
	cache         Cache[*CreatorItem]
	manager       remote.Manager[[]*CreatorItem]
}

func (cb *CreatorBucket) Upsert(ctx context.Context, identityID, shareReferenceId string) error {
	jsonPath := filepath.Join("/", cb.fileNamespace, identityID, cb.fileName)

	r, mu, err := cb.manager.Get(ctx, jsonPath)
	if err != nil {
		return err
	}

	dataOP := func() ([]*CreatorItem, error) {
		item := &CreatorItem{
			IdentityID:       identityID,
			ShareReferenceId: shareReferenceId,
		}

		cb.cache.Upsert(identityID+shareReferenceId, item)

		return cb.cache.Find(identityID), nil
	}

	updateOP := func(remoteItems []*CreatorItem) error {
		for _, localItem := range cb.cache.Find(identityID) {
			cb.cache.Delete(localItem.IdentityID + localItem.ShareReferenceId)
		}

		for _, remoteItem := range remoteItems {
			cb.cache.Upsert(remoteItem.IdentityID+remoteItem.ShareReferenceId, remoteItem)
		}

		return nil
	}

	resetOP := func(err error) error {
		cb.cache.Delete(identityID + shareReferenceId)

		return err
	}

	mu.Lock()
	defer mu.Unlock()

	if err = r.Push(ctx, dataOP); errors.Is(err, remote.ErrMustPull) {
		return r.Merge(ctx, dataOP, updateOP, resetOP)
	}

	return err
}

func (cb *CreatorBucket) Find(ctx context.Context, identityID string) ([]*CreatorItem, error) {
	jsonPath := filepath.Join("/", cb.fileNamespace, identityID, cb.fileName)

	r, mu, err := cb.manager.Get(ctx, jsonPath)
	if err != nil {
		return nil, err
	}

	updateOP := func(remoteItems []*CreatorItem) error {
		for _, localItem := range cb.cache.Find(identityID) {
			cb.cache.Delete(localItem.IdentityID + localItem.ShareReferenceId)
		}

		for _, remoteItem := range remoteItems {
			cb.cache.Upsert(remoteItem.IdentityID+remoteItem.ShareReferenceId, remoteItem)
		}

		return nil
	}

	mu.RLock()
	defer mu.RUnlock()

	err = r.Pull(ctx, updateOP)
	if err != nil {
		return nil, err
	}

	return cb.cache.Find(identityID), nil
}

func (cb *CreatorBucket) Delete(ctx context.Context, identityID, shareReferenceId string) error {
	jsonPath := filepath.Join("/", cb.fileNamespace, identityID, cb.fileName)

	r, mu, err := cb.manager.Get(ctx, jsonPath)
	if err != nil {
		return err
	}

	dataOP := func() ([]*CreatorItem, error) {
		cb.cache.Delete(identityID + shareReferenceId)

		return cb.cache.Find(identityID), nil
	}

	updateOP := func(remoteItems []*CreatorItem) error {
		for _, localItem := range cb.cache.Find(identityID) {
			cb.cache.Delete(localItem.IdentityID + localItem.ShareReferenceId)
		}

		for _, remoteItem := range remoteItems {
			cb.cache.Upsert(remoteItem.IdentityID+remoteItem.ShareReferenceId, remoteItem)
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
