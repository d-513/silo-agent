package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/db"
	"silo.agent/internal/skills"
)

func versionProto(v *db.ArtifactVersion) *v1.ArtifactVersion {
	return &v1.ArtifactVersion{
		N: int32(v.N), Kind: v.Kind, Name: v.Name, Size: v.Size, Pinned: v.Blob != "", Gone: v.Gone,
		Network: v.Network, Entry: v.Entry, CreatedAt: v.CreatedAt.Format(time.RFC3339),
	}
}

func rpcErr(err error) error {
	switch {
	case errors.Is(err, errNotFound), errors.Is(err, errNoFile):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, ErrGone):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, ErrAltered):
		return connect.NewError(connect.CodeDataLoss, err)
	}
	return err
}

// GetArtifact is a card's versions and, for the asked one, its files.
func (s *Service) GetArtifact(ctx context.Context, req *connect.Request[v1.GetArtifactRequest]) (*connect.Response[v1.GetArtifactResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	art, v, err := s.version(b.ID, req.Msg.GetArtifactId(), int(req.Msg.GetVersion()))
	if err != nil {
		return nil, rpcErr(err)
	}
	out := &v1.GetArtifactResponse{Id: art.ID, Type: art.Type, Title: art.Title, Path: art.Path, Version: versionProto(v), Apps: s.contentOn(), Shares: s.contentOn()}
	var all []db.ArtifactVersion
	s.db.Where("artifact_id = ?", art.ID).Order("n").Find(&all)
	for i := range all {
		out.Versions = append(out.Versions, versionProto(&all[i]))
	}
	if v.Kind == "tree" {
		if v.Blob == "" {
			if names, err := s.skills.PullTree(ctx, b.ID, art.Path); err == nil {
				for p, data := range names {
					out.Entries = append(out.Entries, &v1.ArtifactEntry{Path: p, Size: int64(len(data))})
				}
			}
		} else if !v.Gone {
			m, err := s.manifestOf(ctx, b.ID, v.Blob)
			if err != nil {
				return nil, rpcErr(err)
			}
			for _, e := range m.Entries {
				out.Entries = append(out.Entries, &v1.ArtifactEntry{Path: e.Path, Size: e.Size})
			}
		}
	}
	return connect.NewResponse(out), nil
}

// ReadArtifactFile is one file of a version, as it was pinned.
func (s *Service) ReadArtifactFile(ctx context.Context, req *connect.Request[v1.ReadArtifactFileRequest]) (*connect.Response[v1.ReadArtifactFileResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	art, v, err := s.version(b.ID, req.Msg.GetArtifactId(), int(req.Msg.GetVersion()))
	if err != nil {
		return nil, rpcErr(err)
	}
	var name string
	var data []byte
	switch {
	case v.Kind != "tree":
		name, data, err = s.versionFile(ctx, art, v)
	case v.Blob == "":
		p := strings.Trim(req.Msg.GetPath(), "/")
		if !safeRel(p) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("bad path"))
		}
		f, ferr := s.ws.Read(ctx, b.ID, art.Path+"/"+p, maxFileBytes)
		name, data, err = f.Name, f.Data, ferr
	default:
		p := strings.Trim(req.Msg.GetPath(), "/")
		if p == "" {
			p = v.Entry
		}
		name, data, err = s.fileOf(ctx, v, p)
	}
	if err != nil {
		return nil, rpcErr(err)
	}
	if name == "" {
		name = path.Base(art.Path)
	}
	return connect.NewResponse(&v1.ReadArtifactFileResponse{Name: name, Data: data, Size: int64(len(data))}), nil
}

// OpenArtifactApp is the address of one app version for the signed-in owner.
func (s *Service) OpenArtifactApp(ctx context.Context, req *connect.Request[v1.OpenArtifactAppRequest]) (*connect.Response[v1.OpenArtifactAppResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	art, v, err := s.version(b.ID, req.Msg.GetArtifactId(), int(req.Msg.GetVersion()))
	if err != nil {
		return nil, rpcErr(err)
	}
	if art.Type != TypeApp || v.Kind != "tree" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("this artifact is not an app"))
	}
	if v.Blob == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("this app was too large to keep, so it cannot run here: download it instead"))
	}
	if v.Gone {
		return nil, connect.NewError(connect.CodeNotFound, ErrGone)
	}
	sess := access.Session(ctx)
	if sess == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("sign in again"))
	}
	u, err := s.appURL(art, v, b.UserID, sess.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&v1.OpenArtifactAppResponse{Url: u}), nil
}

func (s *Service) shareProto(sh *db.ArtifactShare) *v1.ArtifactShare {
	out := &v1.ArtifactShare{Id: sh.ID, ArtifactId: sh.ArtifactID, Title: sh.Title, Type: sh.Type, Url: s.shareURL(sh), CreatedAt: sh.CreatedAt.Format(time.RFC3339)}
	var v db.ArtifactVersion
	if s.db.Where("id = ?", sh.VersionID).Limit(1).Find(&v).RowsAffected == 1 {
		out.Version = int32(v.N)
	}
	if sh.LastUsedAt != nil {
		out.LastUsedAt = sh.LastUsedAt.Format(time.RFC3339)
	}
	return out
}

// ShareArtifact makes a public link to one version. It is the owner's button
// only: no tool and no rule lets a Bot publish.
func (s *Service) ShareArtifact(ctx context.Context, req *connect.Request[v1.ShareArtifactRequest]) (*connect.Response[v1.ArtifactShare], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	art, v, err := s.version(b.ID, req.Msg.GetArtifactId(), int(req.Msg.GetVersion()))
	if err != nil {
		return nil, rpcErr(err)
	}
	var have db.ArtifactShare
	if s.db.Where("version_id = ?", v.ID).Limit(1).Find(&have).RowsAffected == 1 {
		return connect.NewResponse(s.shareProto(&have)), nil
	}
	sh, err := s.share(ctx, art, v)
	if err != nil {
		if errors.Is(err, ErrGone) || errors.Is(err, ErrAltered) {
			return nil, rpcErr(err)
		}
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(s.shareProto(sh)), nil
}

func (s *Service) ListArtifactShares(ctx context.Context, req *connect.Request[v1.ListArtifactSharesRequest]) (*connect.Response[v1.ListArtifactSharesResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	q := s.db.Where("bot_id = ?", b.ID)
	if id := req.Msg.GetArtifactId(); id != "" {
		q = q.Where("artifact_id = ?", id)
	}
	var rows []db.ArtifactShare
	q.Order("created_at, id").Find(&rows)
	out := &v1.ListArtifactSharesResponse{Available: s.contentOn()}
	for i := range rows {
		out.Shares = append(out.Shares, s.shareProto(&rows[i]))
	}
	return connect.NewResponse(out), nil
}

func (s *Service) DeleteArtifactShare(ctx context.Context, req *connect.Request[v1.DeleteArtifactShareRequest]) (*connect.Response[v1.DeleteArtifactShareResponse], error) {
	sh, err := access.OwnBotRow[db.ArtifactShare](ctx, s.db, req.Msg.GetBotId(), req.Msg.GetId(), "shared link")
	if err != nil {
		return nil, err
	}
	if err := s.db.Delete(sh).Error; err != nil {
		return nil, err
	}
	s.removeShareFiles(sh.ID)
	return connect.NewResponse(&v1.DeleteArtifactShareResponse{}), nil
}

// SaveSkill is the Save skill action on a skill card: it installs the version
// the card shows as a personal skill and emits the card again as saved.
func (s *Service) SaveSkill(ctx context.Context, req *connect.Request[v1.SaveSkillRequest]) (*connect.Response[v1.SaveSkillResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	art, v, err := s.version(b.ID, req.Msg.GetArtifactId(), int(req.Msg.GetVersion()))
	if err != nil {
		return nil, rpcErr(err)
	}
	if art.Type != TypeSkill {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("this artifact is not a skill"))
	}
	files, err := s.versionTree(ctx, art, v)
	if err != nil {
		return nil, rpcErr(err)
	}
	m, err := skills.Parse(string(files["SKILL.md"]))
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if _, err := s.skills.InstallFiles(b, files); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if runID := strings.TrimSpace(req.Msg.GetRunId()); runID != "" {
		info := Info{
			Type: TypeSkill, ArtifactID: art.ID, Version: v.N, Name: m.Name, Title: titleOr(art.Title, m.Name), Path: art.Path,
			Scope: skills.KindPersonal, Status: "saved", Size: v.Size, Kind: v.Kind, Pinned: v.Blob != "", Files: len(files),
		}
		if v.Preview != "" {
			_ = json.Unmarshal([]byte(v.Preview), &info.Preview)
		}
		s.emit(b.ID, runID, info)
	}
	return connect.NewResponse(&v1.SaveSkillResponse{Name: m.Name}), nil
}
