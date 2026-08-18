import AppKit
import ConnectorCore
import Foundation

enum MenuAction: Equatable {
    case refresh, reloadConfig, openConfig, openLog
    case pause(Int), pauseUntilNextDayStart, resume, quit, showStatus, showWarning
}

protocol MenuBackend: Sendable {
    var state: BackendState { get async }
    func launch(executableURL: URL, arguments: [String]) async throws
    func send(method: String, params: JSONValue?) async throws
    func takeResponses() async -> [BackendResponse]
    func takePlans() async -> [PlanPayload]
    func takeNotifications() async -> [NotifyPayload]
    func shutdown() async
}

extension BackendClient: MenuBackend {}

@MainActor
protocol MenuSystem: Sendable {
    func open(path: String)
    func terminate()
}

@MainActor
struct DefaultMenuSystem: MenuSystem {
    func open(path: String) { NSWorkspace.shared.open(URL(fileURLWithPath: path)) }
    func terminate() { NSApplication.shared.terminate(nil) }
}

@MainActor
final class MenuController: ObservableObject {
    @Published private(set) var state: BackendState = .starting
    @Published private(set) var status: StatusPayload?
    @Published private(set) var lastUpdated: Date?
    @Published private(set) var recentWarning = ""

    private let backend: any MenuBackend
    private let executor = MacPlanExecutor()
    private let watcher = LaunchWatcher()
    private let system: any MenuSystem
    private let heartbeat: BrowserPolicyHeartbeat
    private var timer: Timer?
    private var paths: [String: String] = [:]

    init(
        backend: any MenuBackend = BackendClient(),
        system: any MenuSystem = DefaultMenuSystem(),
        heartbeat: BrowserPolicyHeartbeat = BrowserPolicyHeartbeat()
    ) {
        self.backend = backend
        self.system = system
        self.heartbeat = heartbeat
    }

    func perform(_ action: MenuAction) {
        switch action {
        case .refresh: refreshNow()
        case .reloadConfig: reloadConfig()
        case .openConfig: openConfig()
        case .openLog: openLog()
        case .pause(let seconds): pause(for: seconds)
        case .pauseUntilNextDayStart: pauseUntilNextDayStart()
        case .resume: resume()
        case .quit: quit()
        case .showStatus, .showWarning: synchronize()
        }
    }

    func start() {
        // Why: Register hosts here instead of a per-user CLI. Reason: CLI-less users
        // cannot run install-firefox-native-host.sh, and the manifest path must follow the running app.
        if !NativeMessagingInstaller.registerFromAppBundle() {
            recentWarning = "ブラウザ拡張の Host を登録できません"
        }
        Task { await startBackend() }
    }

    func refreshNow() {
        send("refresh_now")
    }

    func reloadConfig() {
        heartbeat.stop()
        send("reload_config")
    }

    func pause(for seconds: Int) {
        heartbeat.stop()
        send("pause", params: .object(["duration_seconds": .number(Double(seconds))]))
    }

    func pauseUntilNextDayStart() {
        let calendar = Calendar.current
        let now = Date()
        guard let tomorrow = calendar.date(byAdding: .day, value: 1, to: now),
              let deadline = calendar.date(bySettingHour: Constants.pauseNextDayStartHour, minute: 0, second: 0, of: tomorrow) else {
            recentWarning = "一時停止の期限を計算できませんでした"
            return
        }
        heartbeat.stop()
        send("pause", params: .object(["until": .string(ISO8601DateFormatter().string(from: deadline))]))
    }

    func resume() {
        send("resume")
    }

    func openConfig() {
        openPath("config")
    }

    func openLog() {
        openPath("log")
    }

    func quit() {
        timer?.invalidate()
        watcher.stop()
        heartbeat.stop()
        Task {
            await backend.shutdown()
            system.terminate()
        }
    }

    private func startBackend() async {
        guard let executableURL = Bundle.main.url(forResource: "tcc-local-connector-backend", withExtension: nil) else {
            heartbeat.stop()
            state = .backendDown
            recentWarning = "同梱バックエンドが見つかりません"
            return
        }
        do {
            try await backend.launch(executableURL: executableURL, arguments: ["serve", "--stdio"])
            synchronize()
            timer = Timer.scheduledTimer(withTimeInterval: TimeInterval(Constants.menuRefreshIntervalSeconds), repeats: true) { [weak self] _ in
                Task { @MainActor in
                    self?.synchronize()
                }
            }
        } catch {
            heartbeat.stop()
            state = .backendDown
            recentWarning = "バックエンドを起動できません"
        }
    }

    private func synchronize() {
        Task {
            do {
                try await backend.send(method: "status", params: nil)
                try await backend.send(method: "config_paths", params: nil)
                let receivedHealthyStatus = await consumeBackendMessages()
                state = await backend.state
                if !receivedHealthyStatus || state != .running {
                    heartbeat.stop()
                }
            } catch {
                heartbeat.stop()
                state = .backendDown
                recentWarning = "バックエンドへ要求を送信できません"
            }
        }
    }

    private func send(_ method: String, params: JSONValue? = nil) {
        Task {
            do {
                try await backend.send(method: method, params: params)
            } catch {
                heartbeat.stop()
                state = .backendDown
                recentWarning = "バックエンドへ要求を送信できません"
            }
        }
    }

    @discardableResult
    private func consumeBackendMessages() async -> Bool {
        var receivedHealthyStatus = false
        var responseFailed = false
        for notification in await backend.takeNotifications() {
            recentWarning = notification.message.isEmpty ? notification.code : notification.message
        }
        for response in await backend.takeResponses() {
            if let error = response.error {
                responseFailed = true
                recentWarning = error.message.isEmpty ? error.code : error.message
                continue
            }
            guard let result = response.result, let payload = try? JSONEncoder().encode(result) else {
                responseFailed = true
                continue
            }
            if let decoded = try? JSONDecoder().decode(StatusPayload.self, from: payload) {
                status = decoded
                lastUpdated = Date()
                if isHealthyStatus(decoded) {
                    receivedHealthyStatus = true
                } else {
                    responseFailed = true
                }
            } else if let decoded = try? JSONDecoder().decode([String: String].self, from: payload), decoded["config"] != nil {
                paths = decoded
            } else {
                responseFailed = true
            }
        }
        for plan in await backend.takePlans() {
            watcher.update(enforceStopBundleIDs: plan.dryRun ? [] : Set(plan.enforceStopBundleIDs))
            let outcomes = await executor.execute(plan)
            let results = Dictionary(uniqueKeysWithValues: zip(plan.actions, outcomes).map { action, outcome in
                (action.id, actionResult(outcome))
            })
            let values = plan.actions.map { action in
                JSONValue.object(["action_id": .string(action.id), "status": .string(results[action.id]?.status ?? "failed"), "code": results[action.id]?.code.map(JSONValue.string) ?? .null])
            }
            do {
                try await backend.send(method: "report_actions", params: .object(["cycle_id": .number(Double(plan.cycleID)), "results": .array(values)]))
            } catch {
                state = .backendDown
                recentWarning = "アクション結果を報告できません"
            }
        }
        if responseFailed {
            heartbeat.stop()
            return false
        }
        if receivedHealthyStatus {
            return heartbeat.startOrRefresh()
        }
        return false
    }

    func processPendingMessagesForTesting() async {
        await consumeBackendMessages()
    }

    private func isHealthyStatus(_ payload: StatusPayload) -> Bool {
        switch payload.state {
        case "active", "fetching": return payload.parseOK
        case "degraded": return true
        default: return false
        }
    }

    private func actionResult(_ outcome: ActionOutcome) -> (status: String, code: String?) {
        switch outcome {
        case .accepted: return ("accepted", nil)
        case .skipped: return ("skipped", nil)
        case .rejected(let code) where code == "quit_refused": return ("refused", code)
        case .rejected(let code) where code == "timeout": return ("timeout", code)
        case .rejected(let code): return ("failed", code)
        }
    }

    private func openPath(_ name: String) {
        guard let path = paths[name] else {
            recentWarning = "パスを取得中です"
            send("config_paths")
            return
        }
        system.open(path: path)
    }
}
