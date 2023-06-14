package bucket

import (
	"github.com/cs3org/reva/v2/pkg/share/manager/jsoncs3/shareid"
)

type CreatorItem struct {
	IdentityID       string // either userID or groupID
	ShareReferenceId string
}

func (ci CreatorItem) StorageID() string {
	storageID, _, _ := shareid.Decode(ci.ShareReferenceId)
	return storageID
}

func (ci CreatorItem) SpaceID() string {
	_, spaceID, _ := shareid.Decode(ci.ShareReferenceId)
	return spaceID
}

func (ci CreatorItem) ShareID() string {
	_, _, shareID := shareid.Decode(ci.ShareReferenceId)
	return shareID
}

func (ci CreatorItem) StorageSpaceID() string {
	return ci.StorageID() + shareid.IDDelimiter + ci.SpaceID()
}
