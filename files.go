package dropbox

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// WriteMode determines what to do if the file already exists.
type WriteMode string

// Supported write modes.
const (
	WriteModeAdd       WriteMode = "add"
	WriteModeOverwrite           = "overwrite"
)

// Files do dropbox files manage
type Files struct {
	*Client
}

// Metadata for a file or folder.
type Metadata struct {
	Tag            string           `json:".tag"`
	Name           string           `json:"name"`
	PathLower      string           `json:"path_lower"`
	PathDisplay    string           `json:"path_display"`
	ClientModified time.Time        `json:"client_modified"`
	ServerModified time.Time        `json:"server_modified"`
	Rev            string           `json:"rev"`
	Size           uint64           `json:"size"`
	ID             string           `json:"id"`
	MediaInfo      *MediaInfo       `json:"media_info,omitempty"`
	SharingInfo    *FileSharingInfo `json:"sharing_info,omitempty"`
	ContentHash    string           `json:"content_hash,omitempty"`
}

// Dimensions specifies the dimensions of a photo or video.
type Dimensions struct {
	Width  uint64 `json:"width"`
	Height uint64 `json:"height"`
}

// GPSCoordinates specifies the GPS coordinate of a photo or video.
type GPSCoordinates struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// PhotoMetadata specifies metadata for a photo.
type PhotoMetadata struct {
	Dimensions *Dimensions     `json:"dimensions,omitempty"`
	Location   *GPSCoordinates `json:"location,omitempty"`
	TimeTaken  time.Time       `json:"time_taken,omitempty"`
}

// VideoMetadata specifies metadata for a video.
type VideoMetadata struct {
	Dimensions *Dimensions     `json:"dimensions,omitempty"`
	Location   *GPSCoordinates `json:"location,omitempty"`
	TimeTaken  time.Time       `json:"time_taken,omitempty"`
	Duration   uint64          `json:"duration,omitempty"`
}

// MediaMetadata provides metadata for a photo or video.
type MediaMetadata struct {
	Photo *PhotoMetadata `json:"photo,omitempty"`
	Video *VideoMetadata `json:"video,omitempty"`
}

// MediaInfo provides additional information for a photo or video file.
type MediaInfo struct {
	Pending  bool           `json:"pending"`
	Metadata *MediaMetadata `json:"metadata,omitempty"`
}

// FileSharingInfo for a file which is contained in a shared folder.
type FileSharingInfo struct {
	ReadOnly             bool   `json:"read_only"`
	ParentSharedFolderID string `json:"parent_shared_folder_id"`
	ModifiedBy           string `json:"modified_by,omitempty"`
}

// GetMetadataInput request input.
type GetMetadataInput struct {
	Path             string `json:"path"`
	IncludeMediaInfo bool   `json:"include_media_info"`
}

// GetMetadataOutput request output.
type GetMetadataOutput struct {
	Metadata
}

// ListFolderInput request input.
type ListFolderInput struct {
	Path             string `json:"path"`
	Recursive        bool   `json:"recursive"`
	IncludeMediaInfo bool   `json:"include_media_info"`
	IncludeDeleted   bool   `json:"include_deleted"`
}

// ListFolderOutput request output.
type ListFolderOutput struct {
	Cursor  string `json:"cursor"`
	HasMore bool   `json:"has_more"`
	Entries []*Metadata
}

// ListFolderContinueInput request input.
type ListFolderContinueInput struct {
	Cursor string `json:"cursor"`
}

// DownloadInput request input.
type DownloadInput struct {
	Path string `json:"path"`
}

// DownloadOutput request output.
type DownloadOutput struct {
	Body   io.ReadCloser
	Length int64
}

// UploadInput request input.
type UploadInput struct {
	Path           string    `json:"path"`
	Mode           WriteMode `json:"mode"`
	AutoRename     bool      `json:"autorename"`
	Mute           bool      `json:"mute"`
	ClientModified string    `json:"client_modified,omitempty"`
	Reader         io.Reader `json:"-"`
}

// UploadOutput request output.
type UploadOutput struct {
	Metadata
}

// GetMetadata returns the metadata for a file or folder.
func (c *Files) GetMetadata(in *GetMetadataInput) (out *GetMetadataOutput, err error) {
	body, err := c.call("/files/get_metadata", in)
	if err != nil {
		return
	}
	defer body.Close()
	err = json.NewDecoder(body).Decode(&out)
	return
}

// ListFolder returns the metadata for a file or folder.
func (c *Files) ListFolder(in *ListFolderInput) (out *ListFolderOutput, err error) {
	in.Path = normalizePath(in.Path)
	body, err := c.call("/files/list_folder", in)
	if err != nil {
		return
	}
	defer body.Close()
	err = json.NewDecoder(body).Decode(&out)
	return
}

// ListFolderContinue pagenates using the cursor from ListFolder.
func (c *Files) ListFolderContinue(in *ListFolderContinueInput) (out *ListFolderOutput, err error) {
	body, err := c.call("/files/list_folder/continue", in)
	if err != nil {
		return
	}
	defer body.Close()
	err = json.NewDecoder(body).Decode(&out)
	return
}

// Download a file.
func (c *Files) Download(in *DownloadInput) (out *DownloadOutput, err error) {
	body, l, err := c.download("/files/download", in, nil)
	if err != nil {
		return
	}
	out = &DownloadOutput{body, l}
	return
}

// Upload a file smaller than 150MB.
func (c *Files) Upload(in *UploadInput) (out *UploadOutput, err error) {
	body, _, err := c.download("/files/upload", in, in.Reader)
	if err != nil {
		return
	}
	defer body.Close()
	err = json.NewDecoder(body).Decode(&out)
	return
}

// Stream get download stream
func (c *Files) Stream(in *DownloadInput, reqFilter func(r http.Header)) (*http.Response, error) {
	return c.pipe("/files/download", in, reqFilter)
}

func normalizePath(s string) string {
	if s == "/" {
		return ""
	}
	return s
}
