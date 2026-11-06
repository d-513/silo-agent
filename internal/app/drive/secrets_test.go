package drive

import (
	"testing"

	"silo.agent/internal/db"
	"silo.agent/internal/db/dbtest"
)

// The add form owns the user's answers and a sign-in or refresh owns the
// dynamic values: an edit made from an older read does not write back the
// token rclone refreshed meanwhile, and a removed drive is not put back.
func TestEditSecretsKeepsTheOtherPart(t *testing.T) {
	gdb := dbtest.New(t)
	s := &Service{db: gdb}
	old := mustJSONString(driveSecrets{User: map[string]string{"key": "a"}, Dynamic: map[string]string{"token": "T1"}})
	gdb.Create(&db.Drive{ID: "d1", SpaceID: "s1", Name: "files", Template: "s3", SecretsJSON: old})

	var loaded db.Drive
	gdb.First(&loaded, "id = ?", "d1")
	// A sidecar reports a refresh after the form loaded the row.
	if _, err := s.editSecrets("d1", nil, func(sec *driveSecrets) { sec.Dynamic["token"] = "T2" }); err != nil {
		t.Fatal(err)
	}
	user := decodeSecrets(loaded.SecretsJSON).User
	user["key"] = "b"
	if _, err := s.editSecrets("d1", nil, func(sec *driveSecrets) { sec.User = user }); err != nil {
		t.Fatal(err)
	}
	var got db.Drive
	gdb.First(&got, "id = ?", "d1")
	sec := decodeSecrets(got.SecretsJSON)
	if sec.User["key"] != "b" || sec.Dynamic["token"] != "T2" {
		t.Fatalf("secrets = %s", got.SecretsJSON)
	}

	gdb.Delete(&db.Drive{}, "id = ?", "d1")
	if _, err := s.editSecrets("d1", nil, func(sec *driveSecrets) { sec.Dynamic["token"] = "T3" }); err != errDriveGone {
		t.Fatalf("editing a removed drive: %v", err)
	}
	var n int64
	gdb.Model(&db.Drive{}).Where("id = ?", "d1").Count(&n)
	if n != 0 {
		t.Fatal("a removed drive came back")
	}
}
