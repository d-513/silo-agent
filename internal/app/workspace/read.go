package workspace

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/channels"
)

// File is one workspace file as the worker read it. Data is its bytes (up to
// the limit asked for); Binary means they hold a NUL; Truncated that the file
// goes on past them.
type File struct {
	Name      string
	Size      int64
	Data      []byte
	Binary    bool
	Truncated bool
}

// Mime is the file's type by its name, octet-stream when unknown.
func (f File) Mime() string {
	if m := MIME(f.Name); m != "" {
		return m
	}
	return "application/octet-stream"
}

// Read reads one workspace file through the worker: the one reader every
// preview, download and attachment goes through. limit 0 is the worker's
// preview budget (2 MB); PresentLimit is the most it will hand over.
func (s *Service) Read(ctx context.Context, botID, rel string, limit int64) (File, error) {
	rel = Rel(rel)
	if rel == "" {
		return File{}, errors.New("path required")
	}
	raw, err := s.Call(ctx, botID, &v1.Cmd{Body: &v1.Cmd_BrowseFile{BrowseFile: &v1.BrowseFileCmd{Path: rel, Limit: limit}}})
	if err != nil {
		return File{}, err
	}
	var row struct {
		Name      string `json:"name"`
		Content   string `json:"content"`
		Data      string `json:"data"`
		Binary    bool   `json:"binary"`
		Truncated bool   `json:"truncated"`
		Size      int64  `json:"size"`
	}
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		return File{}, err
	}
	var data []byte
	if row.Data != "" {
		if data, err = base64.StdEncoding.DecodeString(row.Data); err != nil {
			return File{}, err
		}
	}
	if len(data) == 0 && row.Content != "" {
		data = []byte(row.Content)
	}
	f := File{Name: row.Name, Size: row.Size, Data: data, Binary: row.Binary, Truncated: row.Truncated}
	if f.Name == "" {
		f.Name = path.Base(rel)
	}
	return f, nil
}

// Attachment reads one workspace file as a channel attachment.
func (s *Service) Attachment(ctx context.Context, botID, rel string) (channels.Attachment, error) {
	f, err := s.Read(ctx, botID, rel, PresentLimit)
	if err != nil {
		return channels.Attachment{}, err
	}
	if len(f.Data) == 0 {
		return channels.Attachment{}, errors.New("file is empty")
	}
	return channels.Attachment{Name: f.Name, Mime: f.Mime(), Data: f.Data}, nil
}
