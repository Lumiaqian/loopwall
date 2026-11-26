import Foundation

/// What a renderer reports about its presentation clock, as plain values.
/// `VideoRenderer.ClockSample` converts to this so the tick logic stays testable.
struct RendererSample: Sendable, Equatable {
    var url: URL
    var seconds: Double
    var epoch: Int
    var ready: Bool
    /// The video the renderer gave up on. It stays set until a frame is shown again,
    /// so it can outlive the moment it mattered: only the current video's URL counts.
    var failedURL: URL?
    /// Distinguishes one failure from the next, so a stale `failedURL` is not re-handled.
    var failureSerial: Int
}

/// Tells the renderer when to stop recovering from a decoder error on its own.
/// A `.failed` renderer is first rebuilt in place; only `limit` failures in a row on
/// the same asset, with no presented frame or finished loop between them, are final.
struct RenderFailureCounter {
    static let limit = 3
    private var url: URL?
    private var count = 0

    /// Returns true once recovery should stop and the failure be reported.
    mutating func failed(_ url: URL) -> Bool {
        count = self.url == url ? count + 1 : 1
        self.url = url
        return count >= Self.limit
    }
    mutating func presented() {
        url = nil; count = 0
    }
}

/// The pure decisions behind `PlaylistController.tick`. Side effects (registry,
/// renderer calls, rejecting, saving) stay in the controller, which applies the
/// outcomes returned here.
nonisolated enum PlaylistTick {
    struct Leader: Sendable {
        let id: Int
        let sample: RendererSample
    }

    /// Per-display memory carried between ticks.
    struct LeaderState {
        var clock: PresentationClock?
        var leaderID: Int?
        /// The last failure acted on (rejected, or retried after a reload).
        var handledFailure: Int?
        /// Set by a reload: a failure already acted on is retried once instead of re-rejected.
        var retryRequested = false
    }

    struct Play: Equatable, Sendable {
        let url: URL
        let position: Double
        let force: Bool
    }

    struct LeaderOutcome: Equatable, Sendable {
        /// The video the controller must reject; it then re-reconciles with reset elapsed.
        var rejectID: String?
        /// The leader drifted from the timeline: seek it back.
        var resync: Play?
        /// A reload cleared the rejection of a video the renderer failed on: play it again.
        var retry: Play?
        var crossedBoundary = false
    }

    /// Follow the display's leader renderer: reject on failure, resync after a
    /// leader change, otherwise advance the timeline by the presentation clock delta.
    /// `progress` must already be reconciled against `playable`.
    static func leaderStep(
        progress: inout DisplayProgress,
        playable: [MediaEntry],
        rule: PlaylistSpec,
        leader: Leader?,
        state: inout LeaderState,
        urlFor: (String) -> URL?,
    ) -> LeaderOutcome {
        var outcome = LeaderOutcome()
        guard let leader, !playable.isEmpty else {
            state.clock = nil; state.leaderID = nil
            return outcome
        }
        let sample = leader.sample
        let url = urlFor(progress.videoID)
        // Only a failure of the current video counts; a failure of another URL is stale here.
        if let failed = sample.failedURL, failed == url {
            state.clock = nil
            if state.retryRequested {
                state.retryRequested = false
                state.handledFailure = sample.failureSerial
                outcome.retry = Play(url: failed, position: progress.positionSeconds, force: true)
            } else if state.handledFailure != sample.failureSerial {
                state.handledFailure = sample.failureSerial
                outcome.rejectID = progress.videoID
            }
            return outcome
        }
        state.retryRequested = false
        guard sample.ready, let url, sample.url == url else {
            state.clock = nil
            return outcome
        }
        let mediaDuration = playable.first { $0.id == progress.videoID }?.durationSeconds ?? 1
        let actual = sample.seconds.truncatingRemainder(dividingBy: mediaDuration)
        let distance = abs(actual - progress.positionSeconds)
        if state.leaderID != leader.id, min(distance, mediaDuration - distance) > 0.25 {
            outcome.resync = Play(url: url, position: progress.positionSeconds, force: true)
            state.clock = nil
        } else {
            var clock = state.clock ?? PresentationClock()
            let delta = clock.delta(rendererID: leader.id, epoch: sample.epoch, seconds: sample.seconds)
            state.clock = clock
            outcome.crossedBoundary = PlaylistTimeline.advance(&progress, seconds: delta, items: playable, rule: rule)
        }
        state.leaderID = leader.id
        return outcome
    }

    struct SurfaceDecision: Equatable, Sendable {
        var play: Play?
        /// Record that this renderer now shows `current.videoID`.
        var markApplied: Bool
    }

    /// Whether a surface's renderer must be told to play `current`.
    static func surfaceStep(
        current: DisplayProgress,
        url: URL,
        sample: RendererSample,
        applied: String?,
        crossedBoundary: Bool,
    ) -> SurfaceDecision {
        guard crossedBoundary || applied != current.videoID else { return SurfaceDecision(play: nil, markApplied: false) }
        let play = crossedBoundary || sample.url != url
            ? Play(url: url, position: current.positionSeconds, force: crossedBoundary)
            : nil
        return SurfaceDecision(play: play, markApplied: true)
    }
}

/// Reopen the asset once after a transient track-load failure before rejecting it.
@MainActor
func retryRendererCreation<T>(_ load: @MainActor () async throws -> T) async throws -> T {
    do {
        return try await load()
    } catch {
        try await Task.sleep(for: .milliseconds(250))
        return try await load()
    }
}
