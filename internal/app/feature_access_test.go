package app_test

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/apptest"
	"silo.agent/internal/catalog"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// callRPC calls a UI method by its descriptor with a request whose string
// fields fill sets, so one loop can reach every method.
func callRPC(hc *http.Client, base string, md protoreflect.MethodDescriptor, fill func(name string) string) error {
	req := dynamicpb.NewMessage(md.Input())
	fields := md.Input().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if fd.Kind() != protoreflect.StringKind || fd.IsList() || fd.IsMap() {
			continue
		}
		if v := fill(string(fd.Name())); v != "" {
			req.Set(fd, protoreflect.ValueOfString(v))
		}
	}
	init := connect.WithResponseInitializer(func(spec connect.Spec, msg any) error {
		if m, ok := msg.(*dynamicpb.Message); ok {
			*m = *dynamicpb.NewMessage(md.Output())
		}
		return nil
	})
	client := connect.NewClient[dynamicpb.Message, dynamicpb.Message](hc,
		base+"/"+string(md.Parent().FullName())+"/"+string(md.Name()), connect.WithSchema(md), init)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if md.IsStreamingServer() {
		st, err := client.CallServerStream(ctx, connect.NewRequest(req))
		if err != nil {
			return err
		}
		defer st.Close()
		for st.Receive() {
		}
		return st.Err()
	}
	_, err := client.CallUnary(ctx, connect.NewRequest(req))
	return err
}

// refusedBy reports whether err is the access layer's answer: NotFound with
// one of its own messages, or PermissionDenied.
func refusedBy(err error, notFound ...string) bool {
	var ce *connect.Error
	if !errors.As(err, &ce) {
		return false
	}
	if ce.Code() == connect.CodePermissionDenied {
		return true
	}
	for _, m := range notFound {
		if ce.Code() == connect.CodeNotFound && ce.Message() == m {
			return true
		}
	}
	return false
}

// TestEveryRPCChecksAccess holds access.RPCs to the service: every method is
// declared, and each one that acts on a Bot, a chat or a Workspace refuses a
// stranger with the access layer's own answer, never with a NotFound of the
// method's making that would hide a missing check. The owner is never refused.
func TestEveryRPCChecksAccess(t *testing.T) {
	h := apptest.New(t)
	methods := v1.File_silo_v1_ui_proto.Services().ByName("UI").Methods()
	byName := map[string]protoreflect.MethodDescriptor{}
	for i := 0; i < methods.Len(); i++ {
		md := methods.Get(i)
		byName[string(md.Name())] = md
		if _, ok := access.RPCs[string(md.Name())]; !ok {
			t.Errorf("%s has no line in access.RPCs: say who may call it", md.Name())
		}
	}
	for name := range access.RPCs {
		if byName[name] == nil {
			t.Errorf("access.RPCs names %s, which the UI service does not have", name)
		}
	}

	bot := h.CreateBot("Victim")
	chatID := h.FirstChat(bot.Id)
	approval := db.Approval{ID: ids.New(), BotID: bot.Id, Connector: "bot", Action: "soul", Status: "pending", CreatedAt: time.Now()}
	conn := db.Connector{ID: ids.New(), Kind: catalog.KindCustom, BotID: bot.Id, Name: "Mine", Transport: "http", HTTPURL: "http://127.0.0.1:1/mcp"}
	drive := db.Drive{ID: ids.New(), BotID: bot.Id, Name: "box", Template: "webdav"}
	for _, row := range []any{&approval, &conn, &drive} {
		if err := h.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	fill := func(method string) func(string) string {
		return func(field string) string {
			switch field {
			case "bot_id":
				return bot.Id
			case "chat_id":
				return chatID
			case "space_id":
				return bot.SpaceId
			case "scope":
				if access.RPCs[method] == access.SpaceOrLibrary {
					return "space"
				}
			case "id":
				switch method {
				case "DecideApproval":
					return approval.ID
				case "UpdateConnector":
					return conn.ID
				case "SaveDrive", "DeleteDrive", "BeginDriveAuth", "PickDriveOptions", "BrowseDrive":
					return drive.ID
				case "RenameChat", "DeleteChat":
					return chatID
				}
				switch access.RPCs[method] {
				case access.SpaceView, access.SpaceBuild, access.SpaceAdmin, access.SpaceOwn:
					return bot.SpaceId
				}
				return bot.Id
			}
			return ""
		}
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)

	_, stranger, _ := h.Person("stranger@test.local")
	for _, name := range names {
		var want []string
		switch access.RPCs[name] {
		case access.BotUse, access.BotManage, access.BotAdminister, access.ChatRead, access.ChatWrite, access.AdminOrBotManage:
			want = []string{access.NoBot}
		case access.SpaceView, access.SpaceBuild, access.SpaceAdmin, access.SpaceOwn, access.SpaceOrLibrary:
			want = []string{access.NoSpace}
		case access.InstanceAdmin:
			want = []string{}
		default:
			continue
		}
		err := callRPC(stranger, h.URL, byName[name], fill(name))
		if !refusedBy(err, want...) {
			t.Errorf("%s (%s) let a stranger through: %v", name, access.RPCs[name], err)
		}
	}

	// The owner manages the Bot and owns the Workspace: no call of theirs is
	// the access layer's refusal. Deleting goes last, so it misses nothing.
	owner := func(name string) {
		if err := callRPC(h.HTTP, h.URL, byName[name], fill(name)); refusedBy(err, access.NoBot, access.NoChat, access.NoSpace) {
			t.Errorf("%s refused the Bot's own manager: %v", name, err)
		}
	}
	for _, name := range names {
		switch {
		case name == "DeleteBot", name == "DeleteSpace":
		case access.RPCs[name] == access.Public, access.RPCs[name] == access.SignedIn, access.RPCs[name] == access.InstanceAdmin:
		default:
			owner(name)
		}
	}
	owner("DeleteBot")
}
