package remote

import (
	"github.com/pkg/errors"
)

type DataOP[T any] func() (T, error)

type UpdateOP[T any] func(remoteItems T) error
type ResetOP func(err error) error

var (
	ErrMustPull = errors.New("remote is newer")
)

type Info struct {
	MustPull bool
	CanPull  bool
}
