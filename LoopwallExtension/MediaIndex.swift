import Foundation

/// ID → entry and ID → file URL maps for the playlist queue. The 10 Hz tick reads
/// these instead of stat-ing every video on every call; a file that appears or
/// disappears is noticed by `refresh`, which the controller runs on reload, on a
/// media failure and every `recheckInterval`.
struct MediaIndex {
    static let recheckInterval: TimeInterval = 10

    private var entries: [String: MediaEntry] = [:]
    /// Only the entries whose file was available at the last `refresh`.
    private var urls: [String: URL] = [:]
    private var checkedAt = Date.distantPast

    init() {}

    init(media: [MediaEntry], now: Date, urlFor: (MediaEntry) -> URL?) {
        entries = Dictionary(media.map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
        refresh(now: now, urlFor: urlFor)
    }

    func entry(for id: String) -> MediaEntry? {
        entries[id]
    }
    func url(for id: String) -> URL? {
        urls[id]
    }
    func isStale(now: Date) -> Bool {
        now.timeIntervalSince(checkedAt) >= Self.recheckInterval
    }

    /// Re-check which files exist. Returns true when the set of available videos changed.
    @discardableResult
    mutating func refresh(now: Date, urlFor: (MediaEntry) -> URL?) -> Bool {
        var found: [String: URL] = [:]
        for entry in entries.values {
            found[entry.id] = urlFor(entry)
        }
        let changed = found != urls
        urls = found
        checkedAt = now
        return changed
    }
}
