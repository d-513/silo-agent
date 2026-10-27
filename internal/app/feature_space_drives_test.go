package app_test

import (
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
)

// A drive is its Workspace's: added once, mounted in the Bots it is granted
// to, read-only in one of them when its grant says so, and taking it from one
// Bot or deleting that Bot leaves the drive and the other Bots' mounts.
func TestDriveIsGrantedToBots(t *testing.T) {
	h := driveHarness(t)
	keeper := h.CreateBot("Keeper")
	reader, err := h.Client.CreateBot(h.Ctx(), rq(&v1.CreateBotRequest{Name: "Reader", SpaceId: keeper.GetSpaceId()}))
	if err != nil {
		t.Fatal(err)
	}
	draft, err := saveDrive(h, &v1.SaveDriveRequest{BotId: keeper.GetId(), Template: "s3", Draft: true, Options: map[string]string{
		"access_key_id": "AKIA", "secret_access_key": "s3cret", "region": "eu-west-1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := saveDrive(h, &v1.SaveDriveRequest{Id: draft.GetId(), Name: "files", Options: map[string]string{"access_key_id": "AKIA", "region": "eu-west-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if d.GetSpaceId() != keeper.GetSpaceId() || len(d.GetUsedBy()) != 1 || d.GetUsedBy()[0] != keeper.GetId() || d.GetLentBy() != h.Email {
		t.Fatalf("saved drive: %+v", d)
	}
	waitDrive(t, h, keeper.GetId(), d.GetId(), "mounted")

	if _, err := h.Client.GrantDrive(h.Ctx(), rq(&v1.GrantDriveRequest{BotId: reader.Msg.GetId(), DriveId: d.GetId(), ReadOnly: true})); err != nil {
		t.Fatal(err)
	}
	mounts := func(botID string) []db.Drive { return h.App.Drives.BotDrives(botID) }
	if got := mounts(reader.Msg.GetId()); len(got) != 1 || !got[0].ReadOnly {
		t.Fatalf("Reader mounts %+v, want the drive read-only", got)
	}
	if got := mounts(keeper.GetId()); len(got) != 1 || got[0].ReadOnly {
		t.Fatalf("Keeper mounts %+v, want it writable", got)
	}
	listed, err := h.Client.ListSpaceDrives(h.Ctx(), rq(&v1.ListSpaceDrivesRequest{SpaceId: keeper.GetSpaceId()}))
	if err != nil || len(listed.Msg.GetDrives()) != 1 || len(listed.Msg.GetDrives()[0].GetUsedBy()) != 2 {
		t.Fatalf("the hub: %v %v", listed, err)
	}

	if _, err := h.Client.RevokeDrive(h.Ctx(), rq(&v1.RevokeDriveRequest{BotId: reader.Msg.GetId(), DriveId: d.GetId()})); err != nil {
		t.Fatal(err)
	}
	if len(mounts(reader.Msg.GetId())) != 0 || len(mounts(keeper.GetId())) != 1 {
		t.Fatal("revoking from Reader touched Keeper, or missed Reader")
	}

	// Deleting a Bot drops its grant, not the Workspace's drive.
	if _, err := h.Client.GrantDrive(h.Ctx(), rq(&v1.GrantDriveRequest{BotId: reader.Msg.GetId(), DriveId: d.GetId()})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Client.DeleteBot(h.Ctx(), rq(&v1.GetBotRequest{Id: reader.Msg.GetId()})); err != nil {
		t.Fatal(err)
	}
	var drives, grants int64
	h.DB.Model(&db.Drive{}).Where("id = ?", d.GetId()).Count(&drives)
	h.DB.Model(&db.BotDrive{}).Where("bot_id = ?", reader.Msg.GetId()).Count(&grants)
	if drives != 1 || grants != 0 {
		t.Fatalf("after deleting Reader: %d drives, %d of its grants", drives, grants)
	}

	// Deleting the drive takes it from every Bot.
	if _, err := h.Client.DeleteDrive(h.Ctx(), rq(&v1.DeleteDriveRequest{Id: d.GetId()})); err != nil {
		t.Fatal(err)
	}
	if len(mounts(keeper.GetId())) != 0 {
		t.Fatal("Keeper still mounts a deleted drive")
	}
}

func TestDriveGrantsStayInSpace(t *testing.T) {
	h := driveHarness(t)
	home := h.CreateBot("Home")
	other, _ := h.Client.CreateSpace(h.Ctx(), rq(&v1.CreateSpaceRequest{Name: "Other"}))
	away, err := h.Client.CreateBot(h.Ctx(), rq(&v1.CreateBotRequest{Name: "Away", SpaceId: other.Msg.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	draft, _ := saveDrive(h, &v1.SaveDriveRequest{BotId: home.GetId(), Template: "s3", Draft: true, Options: map[string]string{
		"access_key_id": "AKIA", "secret_access_key": "s3cret", "region": "eu-west-1",
	}})
	d, err := saveDrive(h, &v1.SaveDriveRequest{Id: draft.GetId(), Name: "files", Options: map[string]string{"access_key_id": "AKIA", "region": "eu-west-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Client.GrantDrive(h.Ctx(), rq(&v1.GrantDriveRequest{BotId: away.Msg.GetId(), DriveId: d.GetId()})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("GrantDrive across workspaces: %v", err)
	}
	if _, err := saveDrive(h, &v1.SaveDriveRequest{Id: d.GetId(), Name: "files", BotIds: []string{away.Msg.GetId()}, Options: map[string]string{"access_key_id": "AKIA", "region": "eu-west-1"}}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("SaveDrive giving it across workspaces: %v", err)
	}
	if len(h.App.Drives.BotDrives(away.Msg.GetId())) != 0 {
		t.Fatal("a Bot of another workspace mounts the drive")
	}
}
