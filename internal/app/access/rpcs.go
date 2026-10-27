package access

// RPCNeed is who may call a UI RPC.
type RPCNeed string

const (
	// Public needs no session.
	Public RPCNeed = "public"
	// SignedIn is any signed-in user, acting on their own things.
	SignedIn RPCNeed = "signed-in"
	// InstanceAdmin is an admin of the installation.
	InstanceAdmin RPCNeed = "admin"
	// BotUse, BotManage and BotAdminister are Need on the request's Bot.
	BotUse        RPCNeed = "bot:use"
	BotManage     RPCNeed = "bot:manage"
	BotAdminister RPCNeed = "bot:administer"
	// ChatRead and ChatWrite are ChatNeed on the request's chat.
	ChatRead  RPCNeed = "chat:read"
	ChatWrite RPCNeed = "chat:write"
	// SpaceView to SpaceOwn are SpaceNeed on the request's Workspace.
	SpaceView  RPCNeed = "space:view"
	SpaceBuild RPCNeed = "space:build"
	SpaceAdmin RPCNeed = "space:admin"
	SpaceOwn   RPCNeed = "space:own"
	// AdminOrBotManage is an instance admin for the installation's rows (the
	// library, the whole audit) and BotManage for a Bot's.
	AdminOrBotManage RPCNeed = "admin|bot:manage"
	// SpaceOrLibrary is a Workspace's things (scope "space": SpaceBuild to
	// list and change, SpaceView to read a file) or the site library's (any
	// user reads it, an instance admin changes it).
	SpaceOrLibrary RPCNeed = "space|library"
	// SpaceOrBot acts on a Workspace's connection either through a Bot the
	// caller manages (bot_id) or in the Workspace itself (space_id, Build).
	SpaceOrBot RPCNeed = "space|bot:manage"
	// AdminOrSpace is a library preset's (an instance admin's) or a
	// Workspace connection's (who added it, or a Workspace admin).
	AdminOrSpace RPCNeed = "admin|space"
)

// RPCs declares who may call each UI RPC. The handlers enforce it themselves
// (Bot, Chat, Space, RequireAdmin); this is the list to review, and
// TestEveryRPCChecksAccess fails on an RPC missing from it and calls each one
// as people who must be refused. A new RPC is a line here.
var RPCs = map[string]RPCNeed{
	"SignIn":                   Public,
	"SignOut":                  SignedIn,
	"Me":                       SignedIn,
	"AuthOptions":              Public,
	"GetInvite":                Public,
	"AcceptInvite":             Public,
	"ChangeEmail":              SignedIn,
	"SetLanguage":              SignedIn,
	"ChangePassword":           SignedIn,
	"ListSessions":             SignedIn,
	"RevokeSession":            SignedIn,
	"RevokeOtherSessions":      SignedIn,
	"StartTOTP":                SignedIn,
	"ConfirmTOTP":              SignedIn,
	"DisableTOTP":              SignedIn,
	"NewRecoveryCodes":         SignedIn,
	"ListUsers":                InstanceAdmin,
	"UpdateUser":               InstanceAdmin,
	"DeleteUser":               InstanceAdmin,
	"CreateInvite":             InstanceAdmin,
	"DeleteInvite":             InstanceAdmin,
	"CheckOIDC":                InstanceAdmin,
	"ListSpaces":               SignedIn,
	"GetSpace":                 SpaceView,
	"CreateSpace":              SignedIn,
	"UpdateSpace":              SpaceAdmin,
	"DeleteSpace":              SpaceOwn,
	"TransferSpace":            SpaceOwn,
	"ListSpacePeople":          SpaceView,
	"SetSpaceRole":             SpaceAdmin,
	"PlanSpaceRemoval":         SpaceView, // yourself; another person needs SpaceAdmin
	"RemoveSpacePerson":        SpaceView, // leaving; removing someone needs SpaceAdmin
	"CreateSpaceInvite":        SpaceAdmin,
	"ListSpaceInvites":         SpaceAdmin,
	"DeleteSpaceInvite":        SpaceAdmin,
	"GetSpaceInvite":           Public,
	"JoinSpace":                Public, // signed in joins; signed out makes an account only with spaces.invite_signup
	"ListBotPeople":            BotUse,
	"AddBotPerson":             BotManage,
	"SetBotRole":               BotManage,
	"RemoveBotPerson":          BotUse,    // leaving; taking someone else off needs BotAdminister
	"SetLendShared":            SpaceView, // and only the lender
	"ListSpaceRules":           SpaceBuild,
	"SetSpaceRule":             SpaceAdmin,
	"DeleteSpaceRule":          SpaceAdmin,
	"UpdateSpacePolicy":        SpaceAdmin,
	"ListSpaceActivity":        SpaceAdmin,
	"PlanMoveBot":              BotManage, // and SpaceBuild in the target
	"MoveBot":                  BotManage, // and SpaceBuild in the target
	"ListBots":                 SignedIn,
	"CreateBot":                SpaceBuild,
	"UpdateBot":                BotManage,
	"GetBot":                   BotUse,
	"GetContainer":             BotUse,
	"StartBot":                 BotUse,
	"StopBot":                  BotManage,
	"ResetContainer":           BotManage,
	"SetDesktopPaused":         BotUse,
	"ListBotContainers":        BotManage,
	"RemoveBotContainers":      BotManage,
	"ListAdminContainers":      InstanceAdmin,
	"AdminContainerAction":     InstanceAdmin,
	"RemoveAdminBotContainers": InstanceAdmin,
	"DeleteBot":                BotAdminister,
	"ListMemories":             BotUse,
	"SearchMemories":           BotUse,
	"DeleteMemory":             BotManage,
	"ListKnowledge":            BotUse,
	"AddKnowledgeFolder":       BotManage,
	"RemoveKnowledgeFolder":    BotManage,
	"SyncKnowledge":            BotUse,
	"SearchKnowledge":          BotUse,
	"ListFeed":                 BotUse,
	"MarkFeedRead":             BotUse,
	"DeleteFeedPost":           BotManage,
	"QuoteFeedPost":            BotUse,
	"ListDashboard":            BotUse,
	"MarkDashboardRead":        BotUse,
	"DismissDashboardCard":     BotUse,
	"SubmitDashboardCard":      BotUse,
	"ResolveSetupOffer":        BotUse,
	"RefreshWidget":            BotUse,
	"SetWidgetState":           BotUse,
	"RunWidgetAction":          BotUse,
	"ArrangeDashboard":         BotManage,
	"RemoveWidget":             BotManage,
	"AskDashboard":             BotUse,
	"ListTunnels":              BotUse,
	"CreateTunnel":             BotManage,
	"UpdateTunnel":             BotManage,
	"DeleteTunnel":             BotManage,
	"ListMail":                 BotUse,
	"GetMail":                  BotUse,
	"DeleteMail":               BotManage,
	"UpdateMailbox":            BotManage,
	"RotateMailbox":            BotManage,
	"ListAutomations":          BotUse,
	"CreateAutomation":         BotManage,
	"UpdateAutomation":         BotManage,
	"DeleteAutomation":         BotManage,
	"RunAutomation":            BotManage,
	"ListChats":                BotUse,
	"CreateChat":               BotUse,
	"RenameChat":               ChatWrite,
	"DeleteChat":               ChatRead,
	"SetChatModel":             ChatWrite,
	"SetChatThinking":          ChatWrite,
	"ListModels":               BotUse,
	"Transcribe":               BotUse,
	"Send":                     ChatWrite,
	"StopRun":                  ChatRead,
	"StreamRun":                ChatRead,
	"ListChatEvents":           ChatRead,
	"ListCommands":             BotUse,
	"EditMessage":              ChatWrite,
	"DeleteMessage":            ChatWrite,
	"DivergeChat":              ChatWrite,
	"CompactChat":              ChatWrite,
	"CollectMemories":          ChatWrite,
	"ListSubagents":            ChatRead,
	"GetSubagent":              BotUse,
	"StopSubagent":             BotUse,
	"GetTaskboard":             ChatRead,
	"ClearTaskboard":           ChatWrite,
	"ListSecrets":              BotManage,
	"AddSecret":                BotManage,
	"UpdateSecret":             BotManage,
	"DeleteSecret":             BotManage,
	"ListApprovals":            BotUse,
	"DecideApproval":           BotUse,
	"ListRules":                BotManage,
	"SetRule":                  BotManage,
	"ListOperatorRules":        InstanceAdmin,
	"SetOperatorRule":          InstanceAdmin,
	"DeleteOperatorRule":       InstanceAdmin,
	"ListChanges":              BotUse,
	"ListChangeFiles":          BotUse,
	"GetChangePatch":           BotUse,
	"RestoreChange":            BotUse,
	"ListDriveChanges":         BotUse,
	"ListFiles":                BotUse,
	"ReadFile":                 BotUse,
	"Mkdir":                    BotUse,
	"RemoveFile":               BotUse,
	"PutFile":                  BotUse,
	"GetSettings":              InstanceAdmin,
	"PutSettings":              InstanceAdmin,
	"SetModels":                InstanceAdmin,
	"ListProviderModels":       InstanceAdmin,
	"SetConnectorVars":         InstanceAdmin,
	"SetAutoenableConnectors":  InstanceAdmin,
	"ListAuditEvents":          AdminOrBotManage,
	"AuditSummary":             AdminOrBotManage,
	"MarkAuditRead":            BotManage,
	"ListTraffic":              AdminOrBotManage,
	"GetBotNetwork":            BotManage,
	"AddHostPattern":           BotManage,
	"RemoveHostPattern":        BotManage,
	"SetOtherProtocols":        BotManage,
	"GetEgressStatus":          InstanceAdmin,
	"RotateEgressCA":           InstanceAdmin,
	"ListLLMLogs":              InstanceAdmin,
	"ListConnectors":           SignedIn,
	"CreateConnector":          InstanceAdmin,
	"UpdateConnector":          AdminOrSpace,
	"DeleteConnector":          AdminOrSpace,
	"ListBotConnectors":        BotUse,
	"AttachConnector":          BotManage,
	"CreateBotConnector":       SpaceOrBot,
	"DetachConnector":          BotAdminister,
	"ListSpaceConnectors":      SpaceBuild,
	"GrantConnector":           BotManage,
	"RevokeConnector":          BotAdminister,
	"RefreshBotConnector":      BotManage,
	"StartConnectorAuth":       SpaceOrBot,
	"SeedConnectors":           InstanceAdmin,
	"SeedSkills":               InstanceAdmin,
	"ListSkills":               SpaceOrLibrary,
	"InstallSkill":             SpaceOrLibrary,
	"DeleteSkill":              SpaceOrLibrary,
	"ListBotSkills":            BotUse,
	"SetBotSkill":              BotManage,
	"ListSkillFiles":           SpaceOrLibrary,
	"ReadSkillFile":            SpaceOrLibrary,
	"SaveSkill":                BotUse,
	"ListArtifacts":            BotUse,
	"DeleteArtifact":           BotManage,
	"GetArtifact":              BotUse,
	"ReadArtifactFile":         BotUse,
	"OpenArtifactApp":          BotUse,
	"ShareArtifact":            BotManage,
	"ListArtifactShares":       BotManage,
	"DeleteArtifactShare":      BotManage,
	"ListChannelAdapters":      SignedIn,
	"ListBotChannels":          BotUse,
	"CreateChannel":            BotManage,
	"UpdateChannel":            BotManage,
	"DeleteChannel":            BotManage,
	"ChannelAction":            BotManage,
	"ListDriveTemplates":       SignedIn,
	"ListDrives":               BotUse,
	"SaveDrive":                SpaceOrBot,
	"ListSpaceDrives":          SpaceBuild,
	"GrantDrive":               BotManage,
	"RevokeDrive":              BotAdminister,
	"DeleteDrive":              SpaceBuild,
	"BeginDriveAuth":           SpaceBuild,
	"PickDriveOptions":         SpaceBuild,
	"BrowseDrive":              SpaceBuild,
	"GetDriveSettings":         InstanceAdmin,
	"PutDriveSettings":         InstanceAdmin,
}
