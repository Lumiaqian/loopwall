import Foundation

/// How often the playlist queue does its periodic work. Pure, so the intervals
/// and the rules around them are testable; the controller owns the state.
nonisolated enum PlaylistCadence {
    /// Re-read the display list at least this often even without a reconfiguration event.
    static let displayRefreshInterval: TimeInterval = 5

    /// The display list is refreshed when a reconfiguration marked it dirty, or as a
    /// safety net every `displayRefreshInterval`.
    static func displayRefreshDue(dirty: Bool, lastRefresh: Date, now: Date) -> Bool {
        dirty || now.timeIntervalSince(lastRefresh) >= displayRefreshInterval
    }

    /// Tick period while a desktop surface is presented, and while nothing is.
    static let presentingTickMilliseconds = 100
    static let idleTickMilliseconds = 1_000

    /// 10 Hz only while some desktop surface is actually presented and the displays
    /// are awake; otherwise the tick only has to notice that something started.
    static func tickMilliseconds(presenting: Bool, displaysAsleep: Bool) -> Int {
        presenting && !displaysAsleep ? presentingTickMilliseconds : idleTickMilliseconds
    }

    /// runtime.json is rewritten at once when its structure changes and otherwise as a
    /// heartbeat only. The Go side treats a report as fresh for `RuntimeStaleAfter`
    /// (three heartbeats; keep both in step with internal/catalog).
    static let runtimeHeartbeat: TimeInterval = 10

    /// Everything in runtime.json except the playback positions and the timestamp.
    struct RuntimeStructure: Equatable {
        var appliedRevision: Int64
        var activeDisplayKeys: [String]
        var openVideoIDs: [String]
        /// Display key → the video it plays.
        var videoIDs: [String: String]
        var errors: [String]
        var keepAwakeActive: Bool
        var powerSavingActive = false
    }

    static func shouldWriteRuntime(current: RuntimeStructure, written: RuntimeStructure?, lastWrite: Date, now: Date) -> Bool {
        current != written || now.timeIntervalSince(lastWrite) >= runtimeHeartbeat
    }

    struct WriteBackoff {
        private var retryAfter = Date.distantPast

        func allowsWrite(at now: Date) -> Bool {
            now >= retryAfter
        }

        mutating func failed(at now: Date) {
            retryAfter = now.addingTimeInterval(runtimeHeartbeat)
        }
    }

    /// The runtime error line for connections accepted without a verified peer, if any.
    static func unverifiedCallerError(count: Int) -> String? {
        count > 0 ? "有 \(count) 次连接无法验证来源，已放行（系统接口变化？）" : nil
    }
}
