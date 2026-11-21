import Foundation

func require(_ condition: @autoclosure () -> Bool, _ message: String) {
    if !condition() {
        fatalError(message)
    }
}
func close(_ lhs: Double, _ rhs: Double) -> Bool {
    abs(lhs - rhs) < 0.00001
}
let config = try JSONDecoder().decode(LibraryConfig.self, from: Data(contentsOf: URL(fileURLWithPath: CommandLine.arguments[1]))).validated()
try JSONEncoder().encode(config).write(to: URL(fileURLWithPath: CommandLine.arguments[2]), options: .atomic)
let items = config.media
var rule = config.playlist
var a = PlaylistTimeline.initial(key: "display-A", index: 0, displayCount: 2, items: items, rule: rule)!
let b = PlaylistTimeline.initial(key: "display-B", index: 1, displayCount: 2, items: items, rule: rule)!
require(a.videoID == items[0].id && b.videoID == items[1].id && close(b.positionSeconds, 61.5), "even initial staggering")
PlaylistTimeline.advance(&a, seconds: 6.99, items: items, rule: rule)
require(a.videoID == items[0].id, "must not switch when decoder has read ahead")
PlaylistTimeline.advance(&a, seconds: 0.01, items: items, rule: rule)
require(a.videoID == items[1].id && close(a.positionSeconds, 0), "switch on presentation boundary")
require(close(b.positionSeconds, 61.5), "display-A must not modify display-B")
let saved = try JSONEncoder().encode(b)
let restored = try JSONDecoder().decode(DisplayProgress.self, from: saved)
require(restored == b, "stable display progress round trip")
rule.advanceMode = "duration"
a = DisplayProgress(displayKey: "display-A", videoID: items[0].id)
PlaylistTimeline.advance(&a, seconds: 59, items: items, rule: rule)
require(a.videoID == items[0].id && close(a.positionSeconds, 3) && close(a.itemElapsedSeconds, 59), "short video loops until dwell deadline")
PlaylistTimeline.advance(&a, seconds: 1, items: items, rule: rule)
require(a.videoID == items[1].id && close(a.positionSeconds, 0), "short video deadline")
PlaylistTimeline.advance(&a, seconds: 60, items: items, rule: rule)
require(a.videoID == items[2].id, "long video cut at deadline")
a = b
PlaylistTimeline.reconcile(&a, oldOrder: rule.videoIDs, items: items, resetElapsed: true)
require(close(a.positionSeconds, b.positionSeconds) && a.itemElapsedSeconds == 0, "rule edit preserves position and resets dwell")
PlaylistTimeline.reconcile(&a, oldOrder: rule.videoIDs, items: [items[0], items[2]], resetElapsed: false)
require(a.videoID == items[2].id, "removed or bad item skips to next ordered item")
let held = a
PlaylistTimeline.reconcile(&a, oldOrder: rule.videoIDs, items: [], resetElapsed: false)
PlaylistTimeline.advance(&a, seconds: 20, items: [], rule: rule)
require(a == held, "empty list holds final position")
a = DisplayProgress(displayKey: "one", videoID: items[0].id)
require(PlaylistTimeline.advance(&a, seconds: 60, items: [items[0]], rule: rule), "single video dwell boundary must restart its item")
require(a.positionSeconds == 0, "single video position")
rule.advanceMode = "end"
PlaylistTimeline.advance(&a, seconds: 100_000_000, items: items, rule: rule)
require(a.positionSeconds.isFinite, "large deltas are bounded")
var bad = config; bad.playlist.intervalSeconds = 0
require((try? bad.validated()) == nil, "invalid interval rejected")
let key = SurfaceKey(displayID: 2, surfaceUUID: UUID())
var state = PlaybackState()
state = PlaybackReducer.reduce(state, .surfaceAcquired(key, displayID: 2, role: .desktop, choice: playlistChoiceID))
require(PlaybackReducer.targets(state)[key]?.policy == .full, "desktop runs without application pause policies")
state = PlaybackReducer.reduce(state, .screenLockChanged(true))
require(PlaybackReducer.targets(state)[key]?.policy == .full, "lock screen runs")
state = PlaybackReducer.reduce(state, .displaysAsleepChanged(true))
require(PlaybackReducer.targets(state)[key]?.policy == .paused, "system sleep releases resources")
state = PlaybackReducer.reduce(state, .displaysAsleepChanged(false))
require(PlaybackReducer.targets(state)[key]?.policy == .full, "wake resumes")
state = PlaybackReducer.reduce(state, .agentUpdate(key, mode: .default, activity: .suspended))
require(PlaybackReducer.targets(state)[key]?.policy == .paused, "system inactive surface suspended")
print("PASS: Swift timeline, lifecycle and Go → Swift contract")
var clock = PresentationClock()
require(clock.delta(rendererID: 1, epoch: 0, seconds: 50) == 0, "first presentation sample is baseline")
require(close(clock.delta(rendererID: 1, epoch: 0, seconds: 51), 1), "presentation clock advances")
require(clock.delta(rendererID: 1, epoch: 1, seconds: 120) == 0, "seek must not count as played time")
require(clock.delta(rendererID: 2, epoch: 0, seconds: 300) == 0, "surface switch must not count as played time")
state = PlaybackReducer.reduce(state, .surfaceInvalidated(key))
require(PlaybackReducer.targets(state)[key]?.policy == .paused, "invalidated surface releases playback immediately")
state = PlaybackReducer.reduce(state, .surfaceAcquired(key, displayID: 2, role: .desktop, choice: playlistChoiceID))
require(PlaybackReducer.targets(state)[key]?.policy == .full, "reacquired surface resumes")
print("PASS: presentation-clock epochs and surface reactivation")

let fixtureDirectory = URL(fileURLWithPath: CommandLine.arguments[1]).deletingLastPathComponent()
let goRuntime = try JSONDecoder().decode(RuntimeState.self, from: Data(contentsOf: fixtureDirectory.appendingPathComponent("go-runtime.json")))
require(goRuntime.displays[0].displayKey == "display-A" && goRuntime.displays[0].positionSeconds == 61.5, "Go runtime is read in seconds")
var swiftRuntime = RuntimeState()
var savedDisplay = b
savedDisplay.displayKey = "display-B"
swiftRuntime.displays = [savedDisplay]
try JSONEncoder().encode(swiftRuntime).write(to: fixtureDirectory.appendingPathComponent("runtime.json"), options: .atomic)
let display = CGSize(width: 2_560, height: 1_440)
require(GeometryVerdict.decide(laidOutSize: display, laidOutScale: 2, displaySize: display, displayScale: 2) == .matches, "matching display geometry stays")
require(GeometryVerdict.decide(laidOutSize: CGSize(width: 5_120, height: 2_880), laidOutScale: 1, displaySize: display, displayScale: 2) == .reframe(size: display, scale: 2), "pixel-sized destination is reframed")
require(GeometryVerdict.decide(laidOutSize: CGSize(width: 1_280, height: 720), laidOutScale: 4, displaySize: display, displayScale: 2) == .reframe(size: display, scale: 2), "scaled destination is reframed")
require(GeometryVerdict.decide(laidOutSize: CGSize(width: 1_920, height: 1_080), laidOutScale: 2, displaySize: display, displayScale: 2) == .differentPixels, "resolution change waits for system reacquire")
print("PASS: display geometry and Go ↔ Swift runtime progress")

// Pins: a display plays its one video instead of the playlist while it can.
let pins = config.playlist.pins ?? [:]
require(pins["display-A"] == items[2].id && pins["display-B"] == nil, "pins decode from the Go configuration")
let anyUsable: (String) -> Bool = { _ in true }
func displayItems(_ key: String, usable: (String) -> Bool) -> [MediaEntry] {
    config.playlist.items(for: key, media: items, playlist: items, usable: usable)
}
require(displayItems("display-A", usable: anyUsable).map(\.id) == [items[2].id], "pinned display plays its video only")
require(displayItems("display-B", usable: anyUsable) == items, "unpinned display plays the playlist")
require(displayItems("display-A", usable: { $0 != items[2].id }) == items, "an unusable pin falls back to the playlist")
var pinned = DisplayProgress(displayKey: "display-A", videoID: items[0].id, positionSeconds: 3, itemElapsedSeconds: 3)
PlaylistTimeline.reconcile(&pinned, oldOrder: config.playlist.videoIDs, items: [items[2]], resetElapsed: false)
require(pinned.videoID == items[2].id && pinned.positionSeconds == 0, "pinning moves a display to the start of its video")
PlaylistTimeline.advance(&pinned, seconds: 25, items: [items[2]], rule: config.playlist)
require(pinned.videoID == items[2].id && close(pinned.positionSeconds, 5), "a pinned video loops")
var badPins = config
badPins.playlist.pins = ["display-A": "44444444-4444-4444-8444-444444444444"]
require((try? badPins.validated()) == nil, "a pin to a missing video is rejected")
print("PASS: per-display pins")

/// Snapshot sizing: the smallest frame that still covers the display.
let hd = CGSize(width: 1_920, height: 1_080)
require(SnapshotSizing.maximumSize(videoSize: CGSize(width: 7_680, height: 4_320), displayPixels: CGSize(width: 1_000, height: 1_000)) == CGSize(width: 1_778, height: 1_000), "wide video on a narrow display is fitted by its height")
require(SnapshotSizing.maximumSize(videoSize: CGSize(width: 2_160, height: 3_840), displayPixels: CGSize(width: 1_440, height: 2_560)) == CGSize(width: 1_440, height: 2_560), "tall video on a tall display")
require(SnapshotSizing.maximumSize(videoSize: CGSize(width: -1_080, height: 1_920), displayPixels: hd) == CGSize(width: 1_080, height: 1_920), "a rotated size's signs are ignored; a video smaller than the display is unchanged")
require(SnapshotSizing.maximumSize(videoSize: CGSize(width: 3_840, height: 2_160), displayPixels: hd) == hd, "a 4K video on a 1080p display")
require(SnapshotSizing.maximumSize(videoSize: .zero, displayPixels: hd) == nil, "an empty video size is unknown")
require(SnapshotSizing.maximumSize(videoSize: hd, displayPixels: .zero) == nil, "an empty display size is unknown")
print("PASS: snapshot sizing")

/// Tick decisions: the pure leader and surface logic behind PlaylistController.tick.
func tickMedia(_ n: Int, duration: Double) -> MediaEntry {
    MediaEntry(id: "tick-\(n)", name: "T\(n)", filename: "t\(n).mp4", width: 1, height: 1, durationSeconds: duration, frameRate: 30, addedAtUnix: 0)
}
let tickA = tickMedia(1, duration: 10), tickB = tickMedia(2, duration: 20)
let tickURLs = [tickA.id: URL(fileURLWithPath: "/v/a.mp4"), tickB.id: URL(fileURLWithPath: "/v/b.mp4")]
func tickURL(_ id: String) -> URL? {
    tickURLs[id]
}
let tickRule = PlaylistSpec(videoIDs: [tickA.id, tickB.id])
func tickSample(_ media: MediaEntry, seconds: Double, epoch: Int = 0, ready: Bool = true, failedOn failed: MediaEntry? = nil, serial: Int = 0) -> RendererSample {
    RendererSample(url: tickURLs[media.id]!, seconds: seconds, epoch: epoch, ready: ready, failedURL: failed.map { tickURLs[$0.id]! }, failureSerial: serial)
}
func leaderTick(
    _ progress: inout DisplayProgress,
    _ sample: RendererSample?,
    playable: [MediaEntry] = [tickA, tickB],
    state: inout PlaylistTick.LeaderState,
    leaderID: Int = 1,
) -> PlaylistTick.LeaderOutcome {
    PlaylistTick.leaderStep(
        progress: &progress, playable: playable, rule: tickRule,
        leader: sample.map { PlaylistTick.Leader(id: leaderID, sample: $0) }, state: &state, urlFor: tickURL,
    )
}
var tickProgress = DisplayProgress(displayKey: "d", videoID: tickA.id, positionSeconds: 2)
var tickState = PlaylistTick.LeaderState()
var tickOut = leaderTick(&tickProgress, tickSample(tickA, seconds: 2.1), state: &tickState)
require(tickOut == PlaylistTick.LeaderOutcome() && close(tickProgress.positionSeconds, 2), "first leader sample is a baseline")
tickOut = leaderTick(&tickProgress, tickSample(tickA, seconds: 5.1), state: &tickState)
require(!tickOut.crossedBoundary && close(tickProgress.positionSeconds, 5), "leader clock delta advances the timeline")
tickOut = leaderTick(&tickProgress, tickSample(tickA, seconds: 10.1), state: &tickState)
require(tickOut.crossedBoundary && tickProgress.videoID == tickB.id && tickProgress.positionSeconds == 0, "boundary moves to the next item")
// Leader changed and drifted more than 0.25s: seek it back instead of advancing.
tickProgress = DisplayProgress(displayKey: "d", videoID: tickA.id, positionSeconds: 2)
tickState = PlaylistTick.LeaderState()
tickOut = leaderTick(&tickProgress, tickSample(tickA, seconds: 6), state: &tickState, leaderID: 7)
require(tickOut.resync == PlaylistTick.Play(url: tickURLs[tickA.id]!, position: 2, force: true) && !tickOut.crossedBoundary, "drifted new leader is resynced")
require(tickState.clock == nil && tickState.leaderID == 7 && close(tickProgress.positionSeconds, 2), "resync resets the clock and records the leader")
// Not ready, wrong url, no leader or nothing playable: no clock.
tickState.clock = PresentationClock()
_ = leaderTick(&tickProgress, tickSample(tickA, seconds: 2, ready: false), state: &tickState, leaderID: 7)
require(tickState.clock == nil, "an unready leader clears the clock")
tickState.clock = PresentationClock()
_ = leaderTick(&tickProgress, tickSample(tickB, seconds: 2), state: &tickState, leaderID: 7)
require(tickState.clock == nil, "a leader showing another video clears the clock")
_ = leaderTick(&tickProgress, nil, state: &tickState)
require(tickState.leaderID == nil, "no leader forgets the leader")
// A failed leader rejects the current video and leaves the timeline for the controller to reconcile.
tickProgress = DisplayProgress(displayKey: "d", videoID: tickB.id)
tickOut = leaderTick(&tickProgress, tickSample(tickB, seconds: 0, failedOn: tickB, serial: 1), state: &tickState)
require(tickOut.rejectID == tickB.id && tickProgress.videoID == tickB.id, "a failed leader rejects the current video")
// Surface: play on a new video or boundary, mark applied once.
let tickCurrent = DisplayProgress(displayKey: "d", videoID: tickB.id, positionSeconds: 3)
var surfaceOut = PlaylistTick.surfaceStep(current: tickCurrent, url: tickURLs[tickB.id]!, sample: tickSample(tickA, seconds: 0), applied: tickA.id, crossedBoundary: false)
require(surfaceOut.play == PlaylistTick.Play(url: tickURLs[tickB.id]!, position: 3, force: false) && surfaceOut.markApplied, "a surface on another video is told to play")
surfaceOut = PlaylistTick.surfaceStep(current: tickCurrent, url: tickURLs[tickB.id]!, sample: tickSample(tickB, seconds: 0), applied: tickA.id, crossedBoundary: false)
require(surfaceOut.play == nil && surfaceOut.markApplied, "a surface already showing the video is only marked")
surfaceOut = PlaylistTick.surfaceStep(current: tickCurrent, url: tickURLs[tickB.id]!, sample: tickSample(tickB, seconds: 0), applied: tickB.id, crossedBoundary: false)
require(surfaceOut.play == nil && !surfaceOut.markApplied, "an applied surface is left alone")
surfaceOut = PlaylistTick.surfaceStep(current: tickCurrent, url: tickURLs[tickB.id]!, sample: tickSample(tickB, seconds: 0), applied: tickB.id, crossedBoundary: true)
require(surfaceOut.play?.force == true && surfaceOut.markApplied, "a boundary forces every surface to restart")
print("PASS: playlist tick decisions")

// Failure recovery: a renderer failure only counts against the video it names, once.
// (1) Playlist [A, B]: B cannot be opened while A stays loaded. Only B is rejected.
tickProgress = DisplayProgress(displayKey: "d", videoID: tickB.id)
tickState = PlaylistTick.LeaderState()
let staleB = tickSample(tickA, seconds: 3, failedOn: tickB, serial: 1)
tickOut = leaderTick(&tickProgress, staleB, state: &tickState)
require(tickOut.rejectID == tickB.id, "a failed open of the current video rejects it")
tickProgress = DisplayProgress(displayKey: "d", videoID: tickA.id) // the controller reconciles to A
for _ in 0 ..< 3 {
    tickOut = leaderTick(&tickProgress, staleB, playable: [tickA], state: &tickState)
    require(tickOut.rejectID == nil && tickOut.retry == nil, "the failure left over from B must not reject A")
}
require(PlaylistTick.surfaceStep(current: tickProgress, url: tickURLs[tickA.id]!, sample: staleB, applied: tickB.id, crossedBoundary: false).play == nil, "A stays loaded without a restart")
// (2) Single item: the renderer failed, rebuilt itself and shows frames again. A is never rejected.
tickProgress = DisplayProgress(displayKey: "d", videoID: tickA.id, positionSeconds: 1)
tickState = PlaylistTick.LeaderState()
_ = leaderTick(&tickProgress, tickSample(tickA, seconds: 1), playable: [tickA], state: &tickState)
for step in 1 ... 5 {
    tickOut = leaderTick(&tickProgress, tickSample(tickA, seconds: 1 + Double(step) / 10, epoch: 1), playable: [tickA], state: &tickState)
    require(tickOut.rejectID == nil && tickOut.retry == nil, "a recovered renderer does not reject its video")
}
var failures = RenderFailureCounter()
let failingURL = tickURLs[tickA.id]!
require(!failures.failed(failingURL) && !failures.failed(failingURL), "two failures in a row are still recovered")
failures.presented()
require(!failures.failed(failingURL) && !failures.failed(failingURL), "a presented frame restarts the count")
require(failures.failed(failingURL), "the third failure in a row is final")
require(!failures.failed(tickURLs[tickB.id]!), "a failure on another asset starts a new count")
// (3) Single item A was rejected; a reload clears the rejection while the renderer still reports A failed.
// Expected: the failure is retried once (forced play of A at the current position), not rejected again,
// and the same stale failure is then ignored until the renderer reports a new one.
tickProgress = DisplayProgress(displayKey: "d", videoID: tickA.id, positionSeconds: 4)
tickState = PlaylistTick.LeaderState()
let failedA = tickSample(tickA, seconds: 4, failedOn: tickA, serial: 9)
tickOut = leaderTick(&tickProgress, failedA, playable: [tickA], state: &tickState)
require(tickOut.rejectID == tickA.id, "the first failure of A rejects it")
tickState.retryRequested = true // reload
tickOut = leaderTick(&tickProgress, failedA, playable: [tickA], state: &tickState)
require(tickOut.rejectID == nil && tickOut.retry == PlaylistTick.Play(url: tickURLs[tickA.id]!, position: 4, force: true), "a reload retries the failed video once")
tickOut = leaderTick(&tickProgress, failedA, playable: [tickA], state: &tickState)
require(tickOut == PlaylistTick.LeaderOutcome(), "the retry is not repeated while it is in flight")
tickOut = leaderTick(&tickProgress, tickSample(tickA, seconds: 4, failedOn: tickA, serial: 10), playable: [tickA], state: &tickState)
require(tickOut.rejectID == tickA.id, "a failed retry is a new failure and rejects again")
// (4) A failure on a URL other than the current video's is ignored.
tickProgress = DisplayProgress(displayKey: "d", videoID: tickA.id)
tickState = PlaylistTick.LeaderState()
tickOut = leaderTick(&tickProgress, tickSample(tickA, seconds: 0, failedOn: tickB, serial: 5), state: &tickState)
require(tickOut.rejectID == nil && tickOut.retry == nil, "a failure of another URL does not reject the current video")
print("PASS: renderer failure recovery decisions")
