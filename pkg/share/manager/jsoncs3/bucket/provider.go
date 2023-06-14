package bucket

import (
	"encoding/json"

	cs3SharingCollaboration "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	cs3StorageProvider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"

	"github.com/cs3org/reva/v2/pkg/share/manager/jsoncs3/shareid"
)

type ProviderItem struct {
	Share *cs3SharingCollaboration.Share
}

func (pi *ProviderItem) UnmarshalJSON(data []byte) error {
	tmp := struct {
		Share json.RawMessage
	}{}

	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}

	userShare := &cs3SharingCollaboration.Share{
		Grantee: &cs3StorageProvider.Grantee{Id: &cs3StorageProvider.Grantee_UserId{}},
	}
	err = json.Unmarshal(tmp.Share, userShare) // is this a user share?
	if err == nil && userShare.Grantee.Type == cs3StorageProvider.GranteeType_GRANTEE_TYPE_USER {
		pi.Share = userShare
		return nil
	}

	groupShare := &cs3SharingCollaboration.Share{
		Grantee: &cs3StorageProvider.Grantee{Id: &cs3StorageProvider.Grantee_GroupId{}},
	}
	err = json.Unmarshal(tmp.Share, groupShare)
	if err != nil {
		return err
	}

	pi.Share = groupShare

	return nil
}

func (pi *ProviderItem) StorageID() string {
	storageID, _, _ := shareid.Decode(pi.Share.Id.OpaqueId)
	return storageID
}

func (pi *ProviderItem) SpaceID() string {
	_, spaceID, _ := shareid.Decode(pi.Share.Id.OpaqueId)
	return spaceID
}

func (pi *ProviderItem) ShareID() string {
	_, _, shareID := shareid.Decode(pi.Share.Id.OpaqueId)
	return shareID
}
