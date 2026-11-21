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
}
