package remote

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/cs3org/reva/v2/pkg/errtypes"
	"github.com/cs3org/reva/v2/pkg/storage/utils/metadata"
	"github.com/cs3org/reva/v2/pkg/utils"
)

type Resource[T any] struct {
	path     string
	storage  metadata.Storage
	lastPull time.Time
	pullTTL  time.Duration
}

func (r *Resource[T]) Merge(ctx context.Context, dataOP DataOP[T], updateOP UpdateOP[T], resetOP ResetOP) error {
	remoteItems, err := r.pull(ctx)
	if err != nil {
		return err
	}

	if err := updateOP(remoteItems); err != nil {
		return err
	}

	data, err := dataOP()
	if err != nil {
		return err
	}

	err = r.push(ctx, data)
	if err != nil {
		return resetOP(err)
	}

	return nil
}

func (r *Resource[T]) Push(ctx context.Context, dataOP DataOP[T]) error {
	info, err := r.Info(ctx)
	if err != nil {
		return err
	}

	if info.MustPull {
		return ErrMustPull
	}

	data, err := dataOP()
	if err != nil {
		return err
	}

	return r.push(ctx, data)
}

func (r *Resource[T]) push(ctx context.Context, t T) error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}

	if err = r.storage.Upload(ctx, metadata.UploadRequest{
		Path:              r.path,
		Content:           data,
		IfUnmodifiedSince: r.lastPull,
	}); err != nil {
		return err
	}

	return nil
}

func (r *Resource[T]) Pull(ctx context.Context, updateOP UpdateOP[T]) error {
	info, err := r.Info(ctx)
	if err != nil {
		return err
	}

	if !info.CanPull && !info.MustPull {
		return nil
	}

	data, err := r.pull(ctx)
	if err != nil {
		return err
	}

	return updateOP(data)
}

func (r *Resource[T]) pull(ctx context.Context) (T, error) {
	var t T

	data, err := r.storage.SimpleDownload(ctx, r.path)
	if err != nil {
		return t, err
	}

	err = json.Unmarshal(data, &t)
	if err != nil {
		return t, err
	}

	r.lastPull = time.Now()

	return t, nil
}

func (r *Resource[T]) Info(ctx context.Context) (Info, error) {
	info := Info{}

	stat, err := r.storage.Stat(ctx, r.path)

	if err != nil {
		if _, ok := err.(errtypes.NotFound); ok {
			return info, nil
		}

		if _, ok := err.(*os.PathError); ok {
			return info, nil
		}

		return info, err
	}

	if r.lastPull.IsZero() {
		info.MustPull = true
		info.CanPull = true
	} else {
		info.MustPull = utils.TSToTime(stat.Mtime).After(r.lastPull)
		info.CanPull = time.Now().Add(r.pullTTL).After(r.lastPull)
	}

	return info, nil
}
