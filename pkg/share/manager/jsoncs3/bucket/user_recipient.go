package bucket

import (
	"time"

	cs3SharingCollaboration "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	cs3StorageProvider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"

	"github.com/cs3org/reva/v2/pkg/share/manager/jsoncs3/shareid"
)

type UserRecipientItem struct {
	UserID           string
	ShareReferenceId string
	State            cs3SharingCollaboration.ShareState
	MountPoint       *cs3StorageProvider.Reference
	Mtime            time.Time // todo
}

func (uri UserRecipientItem) StorageID() string {
	storageID, _, _ := shareid.Decode(uri.ShareReferenceId)
	return storageID
}

func (uri UserRecipientItem) SpaceID() string {
	_, spaceID, _ := shareid.Decode(uri.ShareReferenceId)
	return spaceID
}

func (uri UserRecipientItem) ShareID() string {
	_, _, shareID := shareid.Decode(uri.ShareReferenceId)
	return shareID
}

func (uri UserRecipientItem) StorageSpaceID() string {
	return uri.StorageID() + shareid.IDDelimiter + uri.SpaceID()
}
