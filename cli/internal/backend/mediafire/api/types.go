package api

import "fmt"

const (
	CodeSessionInvalid = 105
	CodeFileUnknown    = 110
	CodeFileMissing    = 111
	CodeFileTooBig     = 282
)

type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("mediafire: %s (%d)", e.Message, e.Code)
}

type Base struct {
	Result  string `json:"result"`
	Code    int    `json:"error"`
	Message string `json:"message"`
}

func (b *Base) Err() error {
	if b.Result == "Success" {
		return nil
	}
	return &Error{Code: b.Code, Message: b.Message}
}

type SessionToken struct {
	Base
	SessionToken string `json:"session_token"`
}

type Folder struct {
	FolderKey  string `json:"folderkey"`
	Name       string `json:"name"`
	CreatedUTC string `json:"created_utc"`
}

type File struct {
	QuickKey   string `json:"quickkey"`
	Filename   string `json:"filename"`
	Size       string `json:"size"`
	Hash       string `json:"hash"`
	CreatedUTC string `json:"created_utc"`
}

type FolderContent struct {
	Base
	FolderContent struct {
		Folders    []Folder `json:"folders"`
		Files      []File   `json:"files"`
		MoreChunks string   `json:"more_chunks"`
	} `json:"folder_content"`
}

type Links struct {
	Base
	Links []struct {
		QuickKey       string `json:"quickkey"`
		DirectDownload string `json:"direct_download"`
	} `json:"links"`
}

type UserInfo struct {
	Base
	UserInfo struct {
		UsedStorageSize string `json:"used_storage_size"`
		StorageLimit    string `json:"storage_limit"`
	} `json:"user_info"`
}

type FolderCreate struct {
	Base
	FolderKey string `json:"folder_key"`
}

type FileCopy struct {
	Base
	NewQuickKeys []string `json:"new_quickkeys"`
}

type UploadCheck struct {
	Base
	HashExists           string `json:"hash_exists"`
	InAccount            string `json:"in_account"`
	FileExists           string `json:"file_exists"`
	DifferentHash        string `json:"different_hash"`
	DuplicateQuickKey    string `json:"duplicate_quickkey"`
	AvailableSpace       string `json:"available_space"`
	StorageLimitExceeded string `json:"storage_limit_exceeded"`
	ResumableUpload      struct {
		AllUnitsReady string `json:"all_units_ready"`
		NumberOfUnits string `json:"number_of_units"`
		UnitSize      string `json:"unit_size"`
		UploadKey     string `json:"upload_key"`
		Bitmap        struct {
			Count string   `json:"count"`
			Words []string `json:"words"`
		} `json:"bitmap"`
	} `json:"resumable_upload"`
}

type Upload struct {
	Base
	Doupload struct {
		Key         string `json:"key"`
		QuickKey    string `json:"quickkey"`
		Result      string `json:"result"`
		Status      string `json:"status"`
		Description string `json:"description"`
		FileError   string `json:"fileerror"`
		Hash        string `json:"hash"`
		Size        string `json:"size"`
	} `json:"doupload"`
}

type Instant struct {
	Base
	QuickKey string `json:"quickkey"`
}

type DeviceStatus struct {
	Base
	DeviceRevision string `json:"device_revision"`
}

type ChangedFile struct {
	QuickKey        string `json:"quickkey"`
	Filename        string `json:"filename"`
	ParentFolderKey string `json:"parent_folderkey"`
	Revision        string `json:"revision"`
	CreatedUTC      string `json:"created_utc"`
}

type ChangedFolder struct {
	FolderKey       string `json:"folderkey"`
	ParentFolderKey string `json:"parent_folderkey"`
	Revision        string `json:"revision"`
}

type ChangeSet struct {
	Files   []ChangedFile   `json:"files"`
	Folders []ChangedFolder `json:"folders"`
}

type Changes struct {
	Base
	DeviceRevision   string    `json:"device_revision"`
	ChangesListBlock string    `json:"changes_list_block"`
	Updated          ChangeSet `json:"updated"`
	Deleted          ChangeSet `json:"deleted"`
}
