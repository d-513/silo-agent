import Foundation

// Typed calls for the chat's newer parts: slash commands, a long thread's older pages, setup
// offers, and the Bot's proactivity.
extension SiloClient {
    // MARK: - Slash commands and paging

    public func listCommands(botID: String) async throws -> [Silo_V1_Command] {
        try unwrap(await ui.listCommands(request: .with { $0.botID = botID }, headers: [:])).commands
    }

    /// The page of events before `beforeEventID`; `more` says there is another before it.
    public func listChatEvents(botID: String, chatID: String, beforeEventID: String) async throws -> Silo_V1_ListChatEventsResponse {
        try unwrap(await ui.listChatEvents(request: .with {
            $0.botID = botID
            $0.chatID = chatID
            $0.beforeEventID = beforeEventID
        }, headers: [:]))
    }

    // MARK: - Setup offers

    /// `outcome` is `done` or `skipped`; `rest` also skips the other open offers of that turn.
    public func resolveSetupOffer(botID: String, id: String, outcome: String, rest: Bool = false) async throws -> Silo_V1_SetupOffer {
        try unwrap(await ui.resolveSetupOffer(request: .with {
            $0.botID = botID
            $0.id = id
            $0.outcome = outcome
            $0.rest = rest
        }, headers: [:]))
    }

    // MARK: - Proactivity

    /// Sets how much the Bot does unasked (`off`, `quiet`, `active`); it also puts the Heartbeat on
    /// that level's schedule. The other fields are sent as the Bot has them now.
    public func setProactivity(_ bot: Silo_V1_Bot, level: String) async throws -> Silo_V1_Bot {
        // ListBots leaves SOUL and the output policy out: send what the Bot has now, not that.
        let bot = try await getBot(bot.id)
        return try unwrap(await ui.updateBot(request: .with {
            $0.id = bot.id
            $0.name = bot.name
            $0.description_p = bot.description_p
            $0.soul = bot.soul
            $0.autoApprove = bot.autoApprove
            $0.model = bot.model
            $0.proactivity = level
        }, headers: [:]))
    }

    // MARK: - Read aloud

    /// Readies text to be read aloud and answers where it streams from (`url` is single use).
    /// `model`, `voice` and `speed` override the Bot's own, for a preview.
    public func prepareSpeech(botID: String, text: String, model: String = "", voice: String = "", speed: Double = 0) async throws -> URL {
        let response = try unwrap(await ui.prepareSpeech(request: .with {
            $0.botID = botID
            $0.text = text
            $0.model = model
            $0.voice = voice
            $0.speed = speed
        }, headers: [:]))
        guard let url = URL(string: host + response.url) else { throw SiloError.server("The voice did not answer.") }
        return url
    }

    /// The server this client talks to.
    public var address: String { host }
}
