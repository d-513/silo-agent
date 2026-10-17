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
	"silo.agent/internal/app/chats"
	"silo.agent/internal/db"
	"silo.agent/internal/skills"
)

func versionProto(v *db.ArtifactVersion) *v1.ArtifactVersion {
	out := &v1.ArtifactVersion{
		N: int32(v.N), Kind: v.Kind, Name: v.Name, Size: v.Size, Pinned: v.Blob != "", Gone: v.Gone,
		Network: v.Network, Entry: v.Entry, CreatedAt: v.CreatedAt.Format(time.RFC3339), RunId: v.RunID,
	}
	if v.Preview != "" {
		var p Preview
		if json.Unmarshal([]byte(v.Preview), &p) == nil {
			out.PreviewText, out.PreviewImage, out.PreviewMore = p.Text, p.Image, p.More
		}
	}
	return out
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

// ListArtifacts returns a Bot's live artifacts, newest update first, with all
// of their versions and where each card came from.
func (s *Service) ListArtifacts(ctx context.Context, req *connect.Request[v1.ListArtifactsRequest]) (*connect.Response[v1.ListArtifactsResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	live, saved := s.liveState(b.ID)
	var rows []db.Artifact
	s.db.Where("bot_id = ?", b.ID).Order("updated_at desc, created_at desc").Find(&rows)
	kept := make([]db.Artifact, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, a := range rows {
		if live[a.ID] && a.UpdatedAt.Year() > 1970 {
			kept = append(kept, a)
			ids = append(ids, a.ID)
		}
	}
	out := &v1.ListArtifactsResponse{}
	if len(kept) == 0 {
		return connect.NewResponse(out), nil
	}
	var allVers []db.ArtifactVersion
	s.db.Where("artifact_id IN ?", ids).Order("n").Find(&allVers)
	byArt := make(map[string][]*v1.ArtifactVersion, len(kept))
	for i := range allVers {
		v := &allVers[i]
		byArt[v.ArtifactID] = append(byArt[v.ArtifactID], versionProto(v))
	}
	type srcInfo struct {
		kind, name, autoID string
	}
	sources := map[string]srcInfo{}
	for _, a := range kept {
		vers := byArt[a.ID]
		if len(vers) == 0 {
			continue
		}
		src, ok := sources[a.ChatID]
		if !ok && a.ChatID != "" {
			kind, name := chats.Source(s.db, a.ChatID)
			var c db.Chat
			s.db.Select("automation_id").Where("id = ?", a.ChatID).Limit(1).Find(&c)
			src = srcInfo{kind: kind, name: name, autoID: c.AutomationID}
			sources[a.ChatID] = src
		}
		out.Artifacts = append(out.Artifacts, &v1.ArtifactItem{
			Id:           a.ID,
			Type:         a.Type,
			Title:        a.Title,
			Path:         a.Path,
			ChatId:       a.ChatID,
			AutomationId: src.autoID,
			SourceKind:   src.kind,
			SourceName:   src.name,
			CreatedAt:    a.CreatedAt.Format(time.RFC3339),
			UpdatedAt:    a.UpdatedAt.Format(time.RFC3339),
			Versions:     vers,
			Saved:        int32(saved[a.ID]),
		})
	}
	return connect.NewResponse(out), nil
}

// DeleteArtifact removes an artifact card, its versions, any public links to
// it, and its pinned bytes on the Bot's machine.
func (s *Service) DeleteArtifact(ctx context.Context, req *connect.Request[v1.DeleteArtifactRequest]) (*connect.Response[v1.DeleteArtifactResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	var art db.Artifact
	if res := s.db.Where("id = ? AND bot_id = ?", req.Msg.GetId(), b.ID).Limit(1).Find(&art); res.Error != nil {
		return nil, res.Error
	} else if res.RowsAffected == 0 || art.UpdatedAt.Year() <= 1970 {
		return nil, connect.NewError(connect.CodeNotFound, errNotFound)
	}
	var shares []db.ArtifactShare
	s.db.Where("bot_id = ? AND artifact_id = ?", b.ID, art.ID).Find(&shares)
	for i := range shares {
		s.removeShareFiles(shares[i].ID)
	}
	s.db.Where("bot_id = ? AND artifact_id = ?", b.ID, art.ID).Delete(&db.ArtifactShare{})
	s.db.Where("bot_id = ? AND version_id IN (?)", b.ID, s.db.Model(&db.ArtifactVersion{}).Select("id").Where("artifact_id = ?", art.ID)).Delete(&db.ArtifactView{})

	var evs []db.RunEvent
	s.db.Where("kind = ? AND run_id IN (?)", "artifact", s.db.Model(&db.Run{}).Select("id").Where("bot_id = ?", b.ID)).Find(&evs)
	var dropEvs []string
	for _, ev := range evs {
		var info struct {
			ArtifactID string `json:"artifact_id"`
		}
		if json.Unmarshal([]byte(ev.Body), &info) == nil && info.ArtifactID == art.ID {
			dropEvs = append(dropEvs, ev.ID)
		}
	}
	if len(dropEvs) > 0 {
		s.db.Where("id IN ?", dropEvs).Delete(&db.RunEvent{})
	}
	s.db.Model(&art).Updates(map[string]any{"path": "deleted:" + art.ID, "updated_at": time.Unix(0, 0)})
	s.SweepBot(ctx, b.ID)
	return connect.NewResponse(&v1.DeleteArtifactResponse{}), nil
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
	runID := strings.TrimSpace(req.Msg.GetRunId())
	if runID == "" {
		runID = v.RunID
	}
	if runID != "" {
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
