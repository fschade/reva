package bucket

import (
	"github.com/cs3org/reva/v2/pkg/share/manager/jsoncs3/remote"
	"github.com/cs3org/reva/v2/pkg/storage/utils/metadata"
)

type GroupRecipientBucket struct {
	CreatorBucket
}

func NewGroupRecipientBucket(s metadata.Storage) GroupRecipientBucket {
	return GroupRecipientBucket{
		CreatorBucket{
			fileNamespace: "groups",
			fileName:      "received.json",
			cache:         Cache[*CreatorItem]{},
			manager:       remote.NewManager[[]*CreatorItem](s),
		},
	}
}
