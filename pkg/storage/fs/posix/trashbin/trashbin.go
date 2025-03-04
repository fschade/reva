// Copyright 2018-2024 CERN
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

package trashbin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	typesv1beta1 "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/lookup"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/options"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
)

var (
	tracer trace.Tracer
)

func init() {
	tracer = otel.Tracer("github.com/cs3org/reva/pkg/storage/fs/posix/trashbin")
}

type Trashbin struct {
	fs  storage.FS
	o   *options.Options
	p   Permissions
	lu  *lookup.Lookup
	log *zerolog.Logger
}

// trashNode is a helper struct to make trash items available for manipulation in the metadata backend
type trashNode struct {
	spaceID string
	id      string
	path    string
}

func (tn *trashNode) GetSpaceID() string {
	return tn.spaceID
}

func (tn *trashNode) GetID() string {
	return tn.id
}

func (tn *trashNode) InternalPath() string {
	return tn.path
}

const (
	trashHeader = `[Trash Info]`
	timeFormat  = "2006-01-02T15:04:05"
)

type Permissions interface {
	AssembleTrashPermissions(ctx context.Context, n *node.Node) (*provider.ResourcePermissions, error)
}

// New returns a new Trashbin
func New(o *options.Options, p Permissions, lu *lookup.Lookup, log *zerolog.Logger) (*Trashbin, error) {
	return &Trashbin{
		o:   o,
		p:   p,
		lu:  lu,
		log: log,
	}, nil
}

func (tb *Trashbin) writeInfoFile(trashPath, id, path string) error {
	c := trashHeader
	c += "\nPath=" + path
	c += "\nDeletionDate=" + time.Now().Format(timeFormat)

	return os.WriteFile(filepath.Join(trashPath, "info", id+".trashinfo"), []byte(c), 0644)
}

func (tb *Trashbin) readInfoFile(trashPath, id string) (string, *typesv1beta1.Timestamp, error) {
	c, err := os.ReadFile(filepath.Join(trashPath, "info", id+".trashinfo"))
	if err != nil {
		return "", nil, err
	}

	var (
		path string
		ts   *typesv1beta1.Timestamp
	)

	for _, line := range strings.Split(string(c), "\n") {
		if strings.HasPrefix(line, "DeletionDate=") {
			t, err := time.ParseInLocation(timeFormat, strings.TrimSpace(strings.TrimPrefix(line, "DeletionDate=")), time.Local)
			if err != nil {
				return "", nil, err
			}
			ts = utils.TimeToTS(t)
		}
		if strings.HasPrefix(line, "Path=") {
			path = strings.TrimPrefix(line, "Path=")
		}
	}

	return path, ts, nil
}

// Setup the trashbin
func (tb *Trashbin) Setup(fs storage.FS) error {
	if tb.fs != nil {
		return nil
	}

	tb.fs = fs
	return nil
}

func trashRootForNode(n *node.Node) string {
	return filepath.Join(n.SpaceRoot.InternalPath(), ".Trash")
}

func (tb *Trashbin) MoveToTrash(ctx context.Context, n *node.Node, path string) error {
	key := uuid.New().String()
	trashPath := trashRootForNode(n)

	err := os.MkdirAll(filepath.Join(trashPath, "info"), 0755)
	if err != nil {
		return err
	}
	err = os.MkdirAll(filepath.Join(trashPath, "files"), 0755)
	if err != nil {
		return err
	}

	relPath := strings.TrimPrefix(path, n.SpaceRoot.InternalPath())
	relPath = strings.TrimPrefix(relPath, "/")
	err = tb.writeInfoFile(trashPath, key, relPath)
	if err != nil {
		return err
	}

	// purge metadata
	if err = tb.lu.IDCache.DeleteByPath(ctx, path); err != nil {
		return err
	}
	err = tb.lu.MetadataBackend().Purge(ctx, n)
	if err != nil {
		return err
	}

	itemTrashPath := filepath.Join(trashPath, "files", key+".trashitem")
	return os.Rename(path, itemTrashPath)
}

// ListRecycle returns the list of available recycle items
// ref -> the space (= resourceid), key -> deleted node id, relativePath = relative to key
func (tb *Trashbin) ListRecycle(ctx context.Context, spaceID string, key, relativePath string) ([]*provider.RecycleItem, error) {
	_, span := tracer.Start(ctx, "ListRecycle")
	defer span.End()

	trashRoot := filepath.Join(tb.lu.InternalPath(spaceID, spaceID), ".Trash")
	base := filepath.Join(trashRoot, "files")

	var originalPath string
	var ts *typesv1beta1.Timestamp
	if key != "" {
		// this is listing a specific item/folder
		base = filepath.Join(base, key+".trashitem", relativePath)
		var err error
		originalPath, ts, err = tb.readInfoFile(trashRoot, key)
		if err != nil {
			return nil, err
		}
		originalPath = filepath.Join(originalPath, relativePath)
	}

	items := []*provider.RecycleItem{}
	entries, err := os.ReadDir(filepath.Clean(base))
	if err != nil {
		switch err.(type) {
		case *os.PathError:
			return items, nil
		default:
			return nil, err
		}
	}

	for _, entry := range entries {
		var fi os.FileInfo
		var entryOriginalPath string
		var entryKey string
		if strings.HasSuffix(entry.Name(), ".trashitem") {
			entryKey = strings.TrimSuffix(entry.Name(), ".trashitem")
			entryOriginalPath, ts, err = tb.readInfoFile(trashRoot, entryKey)
			if err != nil {
				continue
			}

			fi, err = entry.Info()
			if err != nil {
				continue
			}
		} else {
			fi, err = os.Stat(filepath.Join(base, entry.Name()))
			entryKey = entry.Name()
			entryOriginalPath = filepath.Join(originalPath, entry.Name())
			if err != nil {
				continue
			}
		}

		item := &provider.RecycleItem{
			Key:  filepath.Join(key, relativePath, entryKey),
			Size: uint64(fi.Size()),
			Ref: &provider.Reference{
				ResourceId: &provider.ResourceId{
					SpaceId:  spaceID,
					OpaqueId: spaceID,
				},
				Path: entryOriginalPath,
			},
			DeletionTime: ts,
		}
		if entry.IsDir() {
			item.Type = provider.ResourceType_RESOURCE_TYPE_CONTAINER
		} else {
			item.Type = provider.ResourceType_RESOURCE_TYPE_FILE
		}

		items = append(items, item)
	}

	return items, nil
}

// RestoreRecycleItem restores the specified item
func (tb *Trashbin) RestoreRecycleItem(ctx context.Context, spaceID string, key, relativePath string, restoreRef *provider.Reference) error {
	_, span := tracer.Start(ctx, "RestoreRecycleItem")
	defer span.End()

	trashRoot := filepath.Join(tb.lu.InternalPath(spaceID, spaceID), ".Trash")
	trashPath := filepath.Clean(filepath.Join(trashRoot, "files", key+".trashitem", relativePath))

	// TODO why can we not use NodeFromResource here? It will use walk path. Do trashed items have a problem with that?
	restoreBaseNode, err := tb.lu.NodeFromID(ctx, restoreRef.GetResourceId())
	if err != nil {
		return err
	}
	restorePath := filepath.Join(restoreBaseNode.InternalPath(), restoreRef.GetPath())
	// TODO the decomposed trash also checks the permissions on the restore node

	_, id, _, err := tb.lu.MetadataBackend().IdentifyPath(ctx, trashPath)
	if err != nil {
		return err
	}

	// update parent id in case it was restored to a different location
	_, parentID, _, err := tb.lu.MetadataBackend().IdentifyPath(ctx, filepath.Dir(restorePath))
	if err != nil {
		return err
	}
	if len(parentID) == 0 {
		return fmt.Errorf("trashbin: parent id not found for %s", restorePath)
	}

	trashNode := &trashNode{spaceID: spaceID, id: id, path: trashPath}
	err = tb.lu.MetadataBackend().Set(ctx, trashNode, prefixes.ParentidAttr, []byte(parentID))
	if err != nil {
		return err
	}

	// restore the item
	err = os.Rename(trashPath, restorePath)
	if err != nil {
		return err
	}
	if err := tb.lu.CacheID(ctx, spaceID, string(id), restorePath); err != nil {
		tb.log.Error().Err(err).Str("spaceID", spaceID).Str("id", string(id)).Str("path", restorePath).Msg("trashbin: error caching id")
	}

	// cleanup trash info
	if relativePath == "." || relativePath == "/" {
		return os.Remove(filepath.Join(trashRoot, "info", key+".trashinfo"))
	} else {
		return nil
	}
}

// PurgeRecycleItem purges the specified item, all its children and all their revisions
func (tb *Trashbin) PurgeRecycleItem(ctx context.Context, ref *provider.Reference, key, relativePath string) error {
	n, err := tb.lu.NodeFromResource(ctx, ref)
	if err != nil {
		return err
	}

	// check permissions of deleted node
	rp, err := tb.p.AssembleTrashPermissions(ctx, n)
	switch {
	case err != nil:
		return err
	case !rp.PurgeRecycle:
		if rp.Stat {
			return errtypes.PermissionDenied(key)
		}
		return errtypes.NotFound(key)
	}

	trashRoot := trashRootForNode(n)
	err = os.RemoveAll(filepath.Clean(filepath.Join(trashRoot, "files", key+".trashitem", relativePath)))
	if err != nil {
		return err
	}

	cleanPath := filepath.Clean(relativePath)
	if cleanPath == "." || cleanPath == "/" {
		return os.Remove(filepath.Join(trashRoot, "info", key+".trashinfo"))
	}
	return nil
}

// EmptyRecycle empties the trash
func (tb *Trashbin) EmptyRecycle(ctx context.Context, ref *provider.Reference) error {
	n, err := tb.lu.NodeFromResource(ctx, ref)
	if err != nil {
		return err
	}
	// check permissions of deleted node
	rp, err := tb.p.AssembleTrashPermissions(ctx, n)
	switch {
	case err != nil:
		return err
	case !rp.ListRecycle && !rp.PurgeRecycle:
		if rp.Stat {
			return errtypes.PermissionDenied(n.ID)
		}
		return errtypes.NotFound(n.ID)
	}

	trashRoot := trashRootForNode(n)
	err = os.RemoveAll(filepath.Clean(filepath.Join(trashRoot, "files")))
	if err != nil {
		return err
	}
	return os.RemoveAll(filepath.Clean(filepath.Join(trashRoot, "info")))
}
