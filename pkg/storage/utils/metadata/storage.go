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

package metadata

import (
	"context"
)

//go:generate mockery -name Storage

// Storage is the interface to maintain metadata in a storage
type Storage interface {
	Backend() string

	Init(ctx context.Context, name string) (err error)
	SimpleUpload(ctx context.Context, uploadpath string, content []byte) error
	SimpleDownload(ctx context.Context, path string) ([]byte, error)
	Delete(ctx context.Context, path string) error

	ReadDir(ctx context.Context, path string) ([]string, error)

	CreateSymlink(ctx context.Context, oldname, newname string) error
	ResolveSymlink(ctx context.Context, name string) (string, error)

	MakeDirIfNotExist(ctx context.Context, name string) error
}