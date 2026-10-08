package app_test

import (
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/apptest"
	"silo.agent/internal/auth"
	"silo.agent/internal/db"
)

// freshCode is a code the server has not seen: it forgets the last used time
// step first, so a test need not wait 30 seconds between sign-ins.
func freshCode(t *testing.T, h *apptest.H, email, secret string) string {
	t.Helper()
	h.DB.Model(&db.User{}).Where("email = ?", email).Update("totp_step", 0)
	c, err := auth.TOTPCode(secret, auth.TOTPStepAt(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// enrol turns two-factor on for a signed-in user and returns the secret and
// the recovery codes.
func enrol(t *testing.T, h *apptest.H, c silov1connect.UIClient, email, password string) (string, []string) {
	t.Helper()
	start, err := c.StartTOTP(h.Ctx(), rq(&v1.StartTOTPRequest{Password: password}))
	if err != nil {
		t.Fatalf("StartTOTP: %v", err)
	}
	done, err := c.ConfirmTOTP(h.Ctx(), rq(&v1.ConfirmTOTPRequest{Code: freshCode(t, h, email, start.Msg.GetSecret())}))
	if err != nil {
		t.Fatalf("ConfirmTOTP: %v", err)
	}
	return start.Msg.GetSecret(), done.Msg.GetCodes()
}

func TestTwoFactorEnrolment(t *testing.T) {
	h := apptest.New(t)
	ann, _ := join(t, h, "ann@test.local", "first-password")

	if _, err := ann.StartTOTP(h.Ctx(), rq(&v1.StartTOTPRequest{Password: "wrong-password"})); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("StartTOTP with a wrong password: %v", err)
	}
	if _, err := ann.ConfirmTOTP(h.Ctx(), rq(&v1.ConfirmTOTPRequest{Code: "000000"})); code(err) != connect.CodeFailedPrecondition {
		t.Fatalf("ConfirmTOTP before StartTOTP: %v", err)
	}
	start, err := ann.StartTOTP(h.Ctx(), rq(&v1.StartTOTPRequest{Password: "first-password"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(start.Msg.GetUrl(), "otpauth://totp/Silo:ann@test.local?") || !strings.HasPrefix(start.Msg.GetQr(), "data:image/png;base64,") || start.Msg.GetSecret() == "" {
		t.Fatalf("StartTOTP: %+v", start.Msg)
	}
	// Nothing is on until a code proves the app has the secret.
	if me, _ := whoAmI(ann); me.GetTotp() {
		t.Fatal("two-factor on before it was confirmed")
	}
	if _, res, err := signInAs(h, "ann@test.local", "first-password", ""); err != nil || res.GetNeedsCode() {
		t.Fatalf("sign-in during setup: %+v %v", res, err)
	}
	if _, err := ann.ConfirmTOTP(h.Ctx(), rq(&v1.ConfirmTOTPRequest{Code: "000000"})); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("a wrong code: %v", err)
	}
	done, err := ann.ConfirmTOTP(h.Ctx(), rq(&v1.ConfirmTOTPRequest{Code: freshCode(t, h, "ann@test.local", start.Msg.GetSecret())}))
	if err != nil || len(done.Msg.GetCodes()) != 10 || !done.Msg.GetUser().GetTotp() {
		t.Fatalf("ConfirmTOTP: %+v %v", done, err)
	}
	seen := map[string]bool{}
	for _, c := range done.Msg.GetCodes() {
		if len(c) != 11 || c[5] != '-' || seen[c] {
			t.Fatalf("recovery code %q", c)
		}
		seen[c] = true
	}
	// Only hashes of the recovery codes are kept.
	var rows []db.RecoveryCode
	h.DB.Find(&rows)
	for _, r := range rows {
		if seen[r.ID] || len(rows) != 10 {
			t.Fatalf("recovery rows: %+v", rows)
		}
	}
	if _, err := ann.StartTOTP(h.Ctx(), rq(&v1.StartTOTPRequest{Password: "first-password"})); code(err) != connect.CodeFailedPrecondition {
		t.Fatalf("StartTOTP when already on: %v", err)
	}
}

func TestSignInWithTwoFactor(t *testing.T) {
	h := apptest.New(t)
	ann, _ := join(t, h, "ann@test.local", "first-password")
	secret, recovery := enrol(t, h, ann, "ann@test.local", "first-password")

	// The password alone signs nobody in; it only earns the question.
	c, res, err := signInAs(h, "ann@test.local", "first-password", "")
	if err != nil || !res.GetNeedsCode() || res.GetUser() != nil {
		t.Fatalf("password only: %+v %v", res, err)
	}
	if _, err := whoAmI(c); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("signed in without a code: %v", err)
	}
	// A wrong password never reaches the code question.
	if _, res, err := signInAs(h, "ann@test.local", "wrong-password", ""); code(err) != connect.CodeUnauthenticated || res != nil {
		t.Fatalf("wrong password: %+v %v", res, err)
	}
	if _, _, err := signInAs(h, "ann@test.local", "first-password", "000000"); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("wrong code: %v", err)
	}

	good := freshCode(t, h, "ann@test.local", secret)
	c, res, err = signInAs(h, "ann@test.local", "first-password", good)
	if err != nil || res.GetNeedsCode() || res.GetUser().GetEmail() != "ann@test.local" {
		t.Fatalf("with a code: %+v %v", res, err)
	}
	if _, err := whoAmI(c); err != nil {
		t.Fatal(err)
	}
	// A code that was just used does not work a second time.
	if _, _, err := signInAs(h, "ann@test.local", "first-password", good); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("a replayed code: %v", err)
	}

	// A recovery code stands in for the phone, once, however it is typed.
	typed := " " + strings.ToUpper(strings.ReplaceAll(recovery[0], "-", " ")) + " "
	if _, _, err := signInAs(h, "ann@test.local", "first-password", typed); err != nil {
		t.Fatalf("recovery code: %v", err)
	}
	if _, _, err := signInAs(h, "ann@test.local", "first-password", recovery[0]); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("a used recovery code: %v", err)
	}
	// Someone else's recovery code is no good.
	bob, _ := join(t, h, "bob@test.local", "bobs-password")
	_, bobCodes := enrol(t, h, bob, "bob@test.local", "bobs-password")
	if _, _, err := signInAs(h, "ann@test.local", "first-password", bobCodes[0]); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("another user's recovery code: %v", err)
	}
}

func TestTwoFactorCodesAreThrottled(t *testing.T) {
	h := apptest.New(t)
	ann, _ := join(t, h, "ann@test.local", "first-password")
	secret, _ := enrol(t, h, ann, "ann@test.local", "first-password")
	for i := range 5 {
		if _, _, err := signInAs(h, "ann@test.local", "first-password", "000000"); code(err) != connect.CodeUnauthenticated {
			t.Fatalf("wrong code %d: %v", i+1, err)
		}
	}
	if _, _, err := signInAs(h, "ann@test.local", "first-password", freshCode(t, h, "ann@test.local", secret)); code(err) != connect.CodeResourceExhausted {
		t.Fatalf("after five wrong codes: %v", err)
	}
}

func TestTwoFactorOffAndNewRecoveryCodes(t *testing.T) {
	h := apptest.New(t)
	ann, u := join(t, h, "ann@test.local", "first-password")
	secret, old := enrol(t, h, ann, "ann@test.local", "first-password")

	if _, err := ann.NewRecoveryCodes(h.Ctx(), rq(&v1.NewRecoveryCodesRequest{Code: "000000"})); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("NewRecoveryCodes with a wrong code: %v", err)
	}
	fresh, err := ann.NewRecoveryCodes(h.Ctx(), rq(&v1.NewRecoveryCodesRequest{Code: freshCode(t, h, "ann@test.local", secret)}))
	if err != nil || len(fresh.Msg.GetCodes()) != 10 {
		t.Fatalf("NewRecoveryCodes: %+v %v", fresh, err)
	}
	if _, _, err := signInAs(h, "ann@test.local", "first-password", old[0]); code(err) != connect.CodeUnauthenticated {
		t.Fatalf("a replaced recovery code: %v", err)
	}
	if _, _, err := signInAs(h, "ann@test.local", "first-password", fresh.Msg.GetCodes()[0]); err != nil {
		t.Fatalf("a new recovery code: %v", err)
	}

	if _, err := ann.DisableTOTP(h.Ctx(), rq(&v1.DisableTOTPRequest{Code: "000000"})); code(err) != connect.CodeInvalidArgument {
		t.Fatalf("DisableTOTP with a wrong code: %v", err)
	}
	off, err := ann.DisableTOTP(h.Ctx(), rq(&v1.DisableTOTPRequest{Code: fresh.Msg.GetCodes()[1]}))
	if err != nil || off.Msg.GetTotp() {
		t.Fatalf("DisableTOTP: %+v %v", off, err)
	}
	if _, res, err := signInAs(h, "ann@test.local", "first-password", ""); err != nil || res.GetNeedsCode() {
		t.Fatalf("sign-in with two-factor off: %+v %v", res, err)
	}
	if n := count(h, &db.RecoveryCode{}, "user_id = ?", u.GetId()); n != 0 {
		t.Fatalf("%d recovery codes left", n)
	}
	if _, err := ann.DisableTOTP(h.Ctx(), rq(&v1.DisableTOTPRequest{Code: "000000"})); code(err) != connect.CodeFailedPrecondition {
		t.Fatalf("DisableTOTP when off: %v", err)
	}

	// A lost phone: the admin turns it off.
	enrol(t, h, ann, "ann@test.local", "first-password")
	res, err := h.Client.UpdateUser(h.Ctx(), rq(&v1.UpdateUserRequest{Id: u.GetId(), ResetTotp: true}))
	if err != nil || res.Msg.GetTotp() {
		t.Fatalf("admin reset: %+v %v", res, err)
	}
	if _, res, err := signInAs(h, "ann@test.local", "first-password", ""); err != nil || res.GetNeedsCode() {
		t.Fatalf("sign-in after the admin reset: %+v %v", res, err)
	}
}
