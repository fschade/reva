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

package loader

import (
	// Load core share manager drivers.
	_ "github.com/opencloud-eu/reva/v2/pkg/publicshare/manager/cs3"
	_ "github.com/opencloud-eu/reva/v2/pkg/publicshare/manager/json"
	_ "github.com/opencloud-eu/reva/v2/pkg/publicshare/manager/memory"
	_ "github.com/opencloud-eu/reva/v2/pkg/publicshare/manager/owncloudsql"
	// Add your own here
)
