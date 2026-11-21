import Foundation

/// What a renderer reports about its presentation clock, as plain values.
/// `VideoRenderer.ClockSample` converts to this so the tick logic stays testable.
struct RendererSample: Sendable, Equatable {
    var url: URL
    var seconds: Double
    var epoch: Int
    var ready: Bool
    var failed: Bool
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
        if sample.failed {
            outcome.rejectID = progress.videoID
            state.clock = nil
            return outcome
        }
        guard sample.ready, let url = urlFor(progress.videoID), sample.url == url else {
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
