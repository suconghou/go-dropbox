package dropbox

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"
)

// Client dropbox client
type Client struct {
	*Config
	Files *Files
}

// File implements an io.ReadWriteCloser for Dropbox files.
type File struct {
	Name    string
	closed  bool
	writing bool
	reader  io.ReadCloser
	pipeR   *io.PipeReader
	pipeW   *io.PipeWriter
	c       *Client
}

// download the file.
func (f *File) download() error {
	out, err := f.c.Files.Download(&DownloadInput{f.Name})
	if err != nil {
		if strings.HasPrefix(err.Error(), "path/not_found/") {
			return &os.PathError{Op: "open", Path: f.Name, Err: syscall.ENOENT}
		}
		return err
	}

	f.reader = out.Body
	return nil
}

// Read implements io.Reader
func (f *File) Read(b []byte) (int, error) {
	if f.reader == nil {
		if err := f.download(); err != nil {
			return 0, err
		}
	}
	return f.reader.Read(b)
}

// Write implements io.Writer.
func (f *File) Write(b []byte) (int, error) {
	if !f.writing {
		f.writing = true
		go func() {
			_, err := f.c.Files.Upload(&UploadInput{
				Mode:   WriteModeOverwrite,
				Path:   f.Name,
				Mute:   true,
				Reader: f.pipeR,
			})

			f.pipeR.CloseWithError(err)
		}()
	}
	return f.pipeW.Write(b)
}

// Close implements io.Closer.
func (f *File) Close() error {
	if f.closed {
		return &os.PathError{Op: "close", Path: f.Name, Err: syscall.EINVAL}
	}
	f.closed = true
	if f.writing {
		if err := f.pipeW.Close(); err != nil {
			return err
		}
	}
	if f.reader != nil {
		if err := f.reader.Close(); err != nil {
			return err
		}
	}
	return nil
}

// FileInfo wraps Dropbox file MetaData to implement os.FileInfo.
type FileInfo struct {
	meta *Metadata
}

// Name of the file.
func (f *FileInfo) Name() string {
	return f.meta.Name
}

// Size of the file.
func (f *FileInfo) Size() int64 {
	return int64(f.meta.Size)
}

// IsDir returns true if the file is a directory.
func (f *FileInfo) IsDir() bool {
	return f.meta.Tag == "folder"
}

// Sys is not implemented.
func (f *FileInfo) Sys() interface{} {
	return nil
}

// ModTime returns the modification time.
func (f *FileInfo) ModTime() time.Time {
	return f.meta.ServerModified
}

// Mode returns the file mode flags.
func (f *FileInfo) Mode() os.FileMode {
	var m os.FileMode
	if f.IsDir() {
		m |= os.ModeDir
	}
	return m
}

// New dropbox client
func New(token string) *Client {
	c := &Client{Config: NewConfig(token)}
	c.Files = &Files{c}
	return c
}

func (c *Client) call(path string, in interface{}) (io.ReadCloser, error) {
	url := "https://api.dropboxapi.com/2" + path
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	r, _, err := c.do(req)
	return r, err
}

// download style endpoint.
func (c *Client) download(path string, in interface{}, r io.Reader) (io.ReadCloser, int64, error) {
	url := "https://content.dropboxapi.com/2" + path
	body, err := json.Marshal(in)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequest(http.MethodPost, url, r)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("Dropbox-API-Arg", string(body))
	if r != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	return c.do(req)
}

func (c *Client) pipe(path string, in interface{}, filter func(r http.Header)) (*http.Response, error) {
	url := "https://content.dropboxapi.com/2" + path
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("Dropbox-API-Arg", string(body))
	if filter != nil {
		filter(req.Header)
	}
	return c.HTTPClient.Do(req)
}

func (c *Client) do(req *http.Request) (io.ReadCloser, int64, error) {
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	if res.StatusCode < 400 {
		return res.Body, res.ContentLength, err
	}
	defer res.Body.Close()
	e := &Error{
		Status:     http.StatusText(res.StatusCode),
		StatusCode: res.StatusCode,
	}
	kind := res.Header.Get("Content-Type")
	if strings.Contains(kind, "text/plain") {
		if b, err := io.ReadAll(res.Body); err == nil {
			e.Summary = string(b)
			return nil, 0, e
		}
		return nil, 0, err
	}
	if err := json.NewDecoder(res.Body).Decode(e); err != nil {
		return nil, 0, err
	}
	return nil, 0, e
}

// Stat returns file and directory meta-data for `name`.
func (c *Client) Stat(name string) (os.FileInfo, error) {
	out, err := c.Files.GetMetadata(&GetMetadataInput{
		Path: name,
	})
	if err != nil {
		return nil, err
	}
	return &FileInfo{&out.Metadata}, nil
}

// ListN returns entries in dir `name`. Up to `n` entries, or all when `n` <= 0.
func (c *Client) ListN(name string, n int) (list []os.FileInfo, err error) {
	var cursor string
	if n <= 0 {
		n = -1
	}
	for {
		var out *ListFolderOutput
		if cursor == "" {
			if out, err = c.Files.ListFolder(&ListFolderInput{Path: name}); err != nil {
				return
			}
			cursor = out.Cursor
		} else {
			if out, err = c.Files.ListFolderContinue(&ListFolderContinueInput{cursor}); err != nil {
				return
			}
			cursor = out.Cursor
		}
		if err != nil {
			return
		}
		for _, ent := range out.Entries {
			list = append(list, &FileInfo{ent})
		}
		if n >= 0 && len(list) >= n {
			list = list[:n]
			break
		}
		if !out.HasMore {
			break
		}
	}
	if n >= 0 && len(list) == 0 {
		err = io.EOF
		return
	}
	return
}

// List returns all entries in dir `name`.
func (c *Client) List(name string) ([]os.FileInfo, error) {
	return c.ListN(name, 0)
}

// ListFilter returns all entries in dir `name` filtered by `filter`.
func (c *Client) ListFilter(name string, filter func(info os.FileInfo) bool) (ret []os.FileInfo, err error) {
	ents, err := c.ListN(name, 0)
	if err != nil {
		return
	}
	for _, ent := range ents {
		if filter(ent) {
			ret = append(ret, ent)
		}
	}
	return
}

// ListFolders returns all folders in dir `name`.
func (c *Client) ListFolders(name string) ([]os.FileInfo, error) {
	return c.ListFilter(name, func(info os.FileInfo) bool {
		return info.IsDir()
	})
}

// ListFiles returns all files in dir `name`.
func (c *Client) ListFiles(name string) ([]os.FileInfo, error) {
	return c.ListFilter(name, func(info os.FileInfo) bool {
		return !info.IsDir()
	})
}

// Open returns a File for reading and writing.
func (c *Client) Open(name string) *File {
	r, w := io.Pipe()
	return &File{
		Name:  name,
		c:     c,
		pipeR: r,
		pipeW: w,
	}
}

// Read returns the contents of `name`.
func (c *Client) Read(name string) ([]byte, error) {
	f := c.Open(name)
	defer f.Close()
	return io.ReadAll(f)
}

// GetStream return http response
func (c *Client) GetStream(name string, reqFilter func(r http.Header)) (*http.Response, error) {
	return c.Files.Stream(&DownloadInput{name}, reqFilter)
}
