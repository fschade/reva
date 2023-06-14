package bucket

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"sync"
	"time"

	"github.com/cs3org/reva/v2/pkg/errtypes"
	"github.com/cs3org/reva/v2/pkg/storage/utils/metadata"
	"github.com/cs3org/reva/v2/pkg/syncx"
	"github.com/cs3org/reva/v2/pkg/utils"
)

type StoreController struct {
	fileStores syncx.Map[string, *FileStore]
	storage    metadata.Storage
}

func (sc *StoreController) Get(ctx context.Context, filePath string) (*FileStore, error) {
	fs, ok := sc.fileStores.Load(filePath)
	if !ok {
		fs = &FileStore{
			filePath: filePath,
			storage:  sc.storage,
		}

		err := sc.storage.MakeDirIfNotExist(ctx, path.Dir(filePath))
		if err != nil {
			return nil, err
		}

		sc.fileStores.Store(filePath, fs)
	}

	return fs, nil
}

type FileStore struct {
	filePath string
	storage  metadata.Storage
	lastPull time.Time
	mu       sync.RWMutex
}

func (fs *FileStore) mtime(ctx context.Context) (time.Time, error) {
	info, err := fs.storage.Stat(ctx, fs.filePath)
	if err != nil {
		mtime := time.Time{}

		if _, ok := err.(errtypes.NotFound); ok {
			return mtime, nil
		}
		if _, ok := err.(*os.PathError); ok {
			return mtime, nil
		}

		return mtime, err
	}

	return utils.TSToTime(info.Mtime), nil
}

func (fs *FileStore) Push(ctx context.Context, v any) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	if err = fs.storage.Upload(ctx, metadata.UploadRequest{
		Path:              fs.filePath,
		Content:           data,
		IfUnmodifiedSince: fs.lastPull,
	}); err != nil {
		return err
	}

	return nil
}

func (fs *FileStore) Pull(ctx context.Context, v any) error {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if time.Now().Before(fs.lastPull.Add(1 * time.Second)) {
		return nil
	}

	currentMtime, err := fs.mtime(ctx)
	if err != nil {
		return err
	}

	if currentMtime.IsZero() {
		return nil
	}

	if !fs.lastPull.IsZero() && currentMtime.Before(fs.lastPull) {
		return nil
	}

	data, err := fs.storage.SimpleDownload(ctx, fs.filePath)
	if err != nil {
		return err
	}

	err = json.Unmarshal(data, v)
	if err != nil {
		return err
	}

	newMtime, err := fs.mtime(ctx)
	if err != nil {
		return err
	}

	fs.lastPull = newMtime

	return nil
}
