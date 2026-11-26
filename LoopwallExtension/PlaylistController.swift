import AppKit
import CoreGraphics
import Foundation

/// One logical session per stable display UUID. Remote layers stay per-surface.
/// The serial queue owns sessions, clock observations and the progress file.
final class PlaylistController: @unchecked Sendable {
    static let shared = PlaylistController()
    private let queue = DispatchQueue(label: "local.lumiaqian.loopwall.playlist")
    private var config = LibraryConfig()
    private var sessions: [String: DisplayProgress] = [:]
    private var displayKeys: [UInt32: String] = [:]
    private var rejected = Set<String>()
    private var leaderStates: [String: PlaylistTick.LeaderState] = [:]
    private var persistedRule: String?
    private var applied: [Int: String] = [:]
    private var timer: (any DispatchSourceTimer)?
    private var tickMilliseconds = PlaylistCadence.presentingTickMilliseconds
    private var writeBackoff = PlaylistCadence.WriteBackoff()
    private var lastSave = Date.distantPast
    private var lastWritten: PlaylistCadence.RuntimeStructure?
    /// Set by a display reconfiguration; `refreshDisplays` clears it.
    private var displaysDirty = true
    private var lastDisplayRefresh = Date.distantPast
    /// The power notifications may not reach this process; re-read power every few seconds too.
    private var lastPowerPoll = Date.distantPast
    private var activeKeys = Set<String>()
    private var lastOpenIDs = Set<String>()
    private var errors: [String] = []
    private var powerSavingActive = false
    private let awake = LockDisplayAssertion()

    private init() {
        if let data = try? Data(contentsOf: Self.runtimeURL),
           let saved = try? JSONDecoder().decode(RuntimeState.self, from: data), saved.schemaVersion == 1 {
            persistedRule = saved.ruleSignature
            for progress in saved.displays where progress.positionSeconds.isFinite && progress.itemElapsedSeconds.isFinite {
                sessions[progress.displayKey] = progress
            }
        }
    }
    private static var runtimeURL: URL {
        VideoLibrary.documents.appendingPathComponent("runtime.json")
    }
    /// Built in `reloadOnQueue` and refreshed by `refreshIndex`; the tick never stats files.
    private var index = MediaIndex()
    private var items: [MediaEntry] {
        config.playlist.videoIDs.compactMap { id in usable(id) ? index.entry(for: id) : nil }
    }
    private func usable(_ id: String) -> Bool {
        !rejected.contains(id) && index.url(for: id) != nil
    }
    private func refreshIndex() {
        index.refresh(now: Date()) { VideoLibrary.shared.availableURL(for: $0) }
    }
    /// What display `key` plays: its pinned video, or the playlist.
    private func items(for key: String) -> [MediaEntry] {
        config.playlist.items(for: key, media: config.media, playlist: items, usable: usable)
    }

    func start() {
        queue.async { [self] in
            reloadOnQueue()
            let timer = DispatchSource.makeTimerSource(queue: queue)
            timer.schedule(deadline: .now(), repeating: .milliseconds(tickMilliseconds))
            timer.setEventHandler { [weak self] in self?.tick() }
            self.timer = timer; timer.resume()
        }
    }
    func reload() {
        queue.async { [self] in reloadOnQueue(); tick(); save() }
    }
    /// A display was added, removed or reconfigured: re-read the display list on the next tick.
    func displaysChanged() {
        queue.async { [self] in displaysDirty = true }
    }
    func lifecycleChanged() {
        queue.async { [self] in tick(); save() }
    }
    func checkpoint() {
        queue.sync { tick(); save() }
    }
    func mediaFailed(_ id: String?, message: String) {
        guard let id else { return }
        queue.async { [self] in refreshIndex(); reject(id, message: message); tick(); save() }
    }
    private func reject(_ id: String, message: String) {
        guard rejected.insert(id).inserted else { return }
        errors.append("无法解码：\(index.entry(for: id)?.name ?? id)（\(message)）")
        extensionLog("[Playlist] Rejected media \(id): \(message)")
    }

    /// Previews use the first item and never create or advance a display session.
    func selection(displayID: UInt32, preview: Bool) -> DisplayProgress? {
        queue.sync {
            let playable = items
            guard !preview else { return playable.first.map { DisplayProgress(displayKey: "preview", videoID: $0.id) } }
            if displayKeys[displayID] == nil {
                refreshDisplays()
            }
            guard let key = displayKeys[displayID] else { return nil }
            ensureSession(key: key, items: items(for: key))
            return sessions[key]
        }
    }
    private func reloadOnQueue() {
        let old = config
        config = VideoLibrary.shared.config
        index = MediaIndex(media: config.media, now: Date()) { VideoLibrary.shared.availableURL(for: $0) }
        rejected.removeAll(); errors.removeAll()
        let signature = "\(config.playlist.advanceMode):\(config.playlist.intervalSeconds)"
        let ruleChanged = persistedRule.map { $0 != signature } ?? (old.playlist.advanceMode != config.playlist.advanceMode || old.playlist.intervalSeconds != config.playlist.intervalSeconds)
        persistedRule = signature
        PlaybackStore.post(.powerSavingSettingChanged(config.playlist.powerSaving ?? false))
        // Each display against its own items, so a pinned one keeps its place.
        for key in sessions.keys {
            PlaylistTimeline.reconcile(&sessions[key]!, oldOrder: old.playlist.videoIDs, items: items(for: key), resetElapsed: ruleChanged)
        }
        for key in leaderStates.keys {
            leaderStates[key]?.retryRequested = true
            if ruleChanged {
                leaderStates[key]?.clock = nil
            }
        }
    }
    private func refreshDisplays() {
        displaysDirty = false
        lastDisplayRefresh = Date()
        var count: UInt32 = 0
        CGGetActiveDisplayList(0, nil, &count)
        var ids = [CGDirectDisplayID](repeating: 0, count: Int(count))
        CGGetActiveDisplayList(count, &ids, &count)
        for id in ids {
            if let uuid = CGDisplayCreateUUIDFromDisplayID(id)?.takeRetainedValue() {
                displayKeys[id] = CFUUIDCreateString(nil, uuid) as String
            }
        }
    }
    private func ensureSession(key: String, items: [MediaEntry]) {
        guard sessions[key] == nil else { return }
        // Order comes from the system's current active display list, not names.
        var count: UInt32 = 0
        CGGetActiveDisplayList(0, nil, &count)
        var ids = [CGDirectDisplayID](repeating: 0, count: Int(count))
        CGGetActiveDisplayList(count, &ids, &count)
        let keys = ids.compactMap { displayKeys[$0] }
        sessions[key] = PlaylistTimeline.initial(
            key: key,
            index: keys.firstIndex(of: key) ?? 0,
            displayCount: keys.count,
            items: items,
            rule: config.playlist,
        )
    }

    private func tick() {
        if index.isStale(now: Date()) {
            refreshIndex()
        }
        if PlaylistCadence.displayRefreshDue(dirty: displaysDirty, lastRefresh: lastDisplayRefresh, now: Date()) {
            refreshDisplays()
        }
        if Date().timeIntervalSince(lastPowerPoll) >= 5 {
            lastPowerPoll = Date()
            PowerMonitor.refresh() // posts only when the conditions changed
        }
        let state = PlaybackStore.snapshot
        let playlist = items
        var tally = TickTally()
        let grouped = Dictionary(grouping: state.surfaces.filter { $0.value.role == .desktop }, by: { displayKeys[$0.key.displayID] ?? "" })
        for (key, surfaces) in grouped where !key.isEmpty {
            tickDisplay(key: key, surfaces: surfaces, state: state, tally: &tally)
        }
        refreshPreviews(playlist: playlist, open: &tally.open)
        applied = applied.filter { tally.seenRenderers.contains($0.key) }
        activeKeys = tally.active; lastOpenIDs = tally.open
        powerSavingActive = state.pausedForPower
        adaptTickRate(presenting: !tally.active.isEmpty, displaysAsleep: state.displaysAsleep)
        awake.update(
            enabled: config.playlist.keepAwakeWhenLockedOnAC,
            locked: state.screenLocked,
            active: tally.playing,
            asleep: state.displaysAsleep,
        )
        if PlaylistCadence.shouldWriteRuntime(current: runtimeStructure(), written: lastWritten, lastWrite: lastSave, now: Date()) {
            save()
        }
    }

    /// What one tick learns across displays and surfaces.
    private struct TickTally {
        var playing = false
        var active = Set<String>()
        var open = Set<String>()
        var seenRenderers = Set<Int>()
    }

    /// One display's share of a tick: advance its timeline from the leader's clock, then
    /// apply the result to each of its surfaces.
    private func tickDisplay(key: String, surfaces: [(key: SurfaceKey, value: Surface)], state: PlaybackState, tally: inout TickTally) {
        var playable = items(for: key)
        ensureSession(key: key, items: playable)
        guard var progress = sessions[key] else { return }
        PlaylistTimeline.reconcile(&progress, oldOrder: config.playlist.videoIDs, items: playable, resetElapsed: false)
        let crossedBoundary = stepLeader(key: key, surfaces: surfaces, state: state, progress: &progress, playable: &playable, tally: &tally)
        sessions[key] = progress
        for entry in surfaces {
            applyToSurface(key: key, entry: entry, state: state, playable: playable, crossedBoundary: crossedBoundary, tally: &tally)
        }
    }

    /// Step the display's timeline from its leader renderer (an actually presented desktop
    /// surface; preview clocks are never leaders). Returns whether the step crossed a boundary.
    private func stepLeader(
        key: String,
        surfaces: [(key: SurfaceKey, value: Surface)],
        state: PlaybackState,
        progress: inout DisplayProgress,
        playable: inout [MediaEntry],
        tally: inout TickTally,
    ) -> Bool {
        let registry = SurfaceRegistry.shared
        // Prefer an actually presented surface; preview clocks are never leaders.
        let visible = surfaces.filter { CGDisplayIsActive($0.key.displayID) != 0 && PlaybackReducer.target(for: $0.value, in: state).policy == .full }
        let leader = visible.sorted { $0.value.generation > $1.value.generation }
            .compactMap { registry.surface(for: $0.key)?.renderer }.first
        if !visible.isEmpty {
            tally.active.insert(key)
            tally.playing = tally.playing || !playable.isEmpty
        }
        var leaderState = leaderStates[key] ?? PlaylistTick.LeaderState()
        let outcome = PlaylistTick.leaderStep(
            progress: &progress,
            playable: playable,
            rule: config.playlist,
            leader: leader.map { PlaylistTick.Leader(id: $0.debugID, sample: $0.clockSample().rendererSample) },
            state: &leaderState,
            urlFor: { index.url(for: $0) },
        )
        leaderStates[key] = leaderState
        if let id = outcome.rejectID {
            reject(id, message: "原生解码失败")
            playable = items(for: key)
            PlaylistTimeline.reconcile(&progress, oldOrder: config.playlist.videoIDs, items: playable, resetElapsed: true)
        }
        if let leader, let play = outcome.resync ?? outcome.retry {
            leader.play(play.url, at: play.position, force: play.force)
        }
        return outcome.crossedBoundary
    }

    /// Apply the display's current item and playback target to one surface.
    private func applyToSurface(
        key: String,
        entry: (key: SurfaceKey, value: Surface),
        state: PlaybackState,
        playable: [MediaEntry],
        crossedBoundary: Bool,
        tally: inout TickTally,
    ) {
        let (surfaceKey, surface) = entry
        guard let renderer = SurfaceRegistry.shared.surface(for: surfaceKey)?.renderer else {
            attachRenderer(to: surfaceKey, progress: sessions[key])
            return
        }
        tally.seenRenderers.insert(renderer.debugID)
        let sample = renderer.clockSample()
        // A removed media file stays on disk until every native reader lets go.
        for url in sample.openURLs {
            tally.open.insert(url.deletingLastPathComponent().lastPathComponent)
        }
        if playable.isEmpty {
            renderer.apply(.paused)
            return
        }
        PlaylistTimeline.reconcile(&sessions[key]!, oldOrder: config.playlist.videoIDs, items: playable, resetElapsed: false)
        guard let current = sessions[key], let url = index.url(for: current.videoID) else { return }
        let decision = PlaylistTick.surfaceStep(
            current: current,
            url: url,
            sample: sample.rendererSample,
            applied: applied[renderer.debugID],
            crossedBoundary: crossedBoundary,
        )
        if let play = decision.play {
            renderer.play(play.url, at: play.position, force: play.force)
        }
        if decision.markApplied {
            applied[renderer.debugID] = current.videoID
        }
        let target = CGDisplayIsActive(surfaceKey.displayID) == 0 ? .paused : PlaybackReducer.target(for: surface, in: state)
        renderer.apply(target)
    }

    /// Previews follow the playlist's first item.
    private func refreshPreviews(playlist: [MediaEntry], open: inout Set<String>) {
        for surfaceKey in SurfaceRegistry.shared.surfaceKeys {
            guard let surface = SurfaceRegistry.shared.surface(for: surfaceKey), surface.isPreview, let renderer = surface.renderer else { continue }
            for url in renderer.clockSample().openURLs {
                open.insert(url.deletingLastPathComponent().lastPathComponent)
            }
            // Keep previews current after edits, without creating any persistent session.
            if let first = playlist.first, let url = index.url(for: first.id) {
                renderer.play(url, at: 0)
            } else {
                renderer.apply(.paused)
            }
        }
    }
    /// Reschedule the timer only when the rate actually changes.
    private func adaptTickRate(presenting: Bool, displaysAsleep: Bool) {
        let wanted = PlaylistCadence.tickMilliseconds(presenting: presenting, displaysAsleep: displaysAsleep)
        guard wanted != tickMilliseconds else { return }
        tickMilliseconds = wanted
        timer?.schedule(deadline: .now() + .milliseconds(wanted), repeating: .milliseconds(wanted))
    }
    /// Reattach after an empty initial list becomes playable or a corrupt first
    /// asset was rejected. The registry claim prevents racing the XPC acquire task.
    private func attachRenderer(to key: SurfaceKey, progress: DisplayProgress?) {
        guard let progress, let url = index.url(for: progress.videoID),
              !items(for: progress.displayKey).isEmpty, let surface = SurfaceRegistry.shared.surface(for: key),
              SurfaceRegistry.shared.claimRendererCreate(for: key, root: surface.rootLayer) else { return }
        let root = SendableBox(value: surface.rootLayer)
        Task {
            do {
                let renderer = try await VideoRenderer.create(rootLayer: root.value, videoURL: url, position: progress.positionSeconds)
                guard SurfaceRegistry.shared.finishRendererCreate(for: key, root: root.value, renderer: renderer, videoID: playlistChoiceID) else { renderer.stop(); return }
                await PlaybackStore.shared.follow(renderer, surface: key)
                await renderer.start()
            } catch {
                guard SurfaceRegistry.shared.finishRendererCreate(for: key, root: root.value) else { return }
                self.mediaFailed(progress.videoID, message: error.localizedDescription)
            }
        }
    }

    private func runtimeErrors() -> [String] {
        var lines = errors
        if items.isEmpty {
            lines.append("播放列表为空或没有可播放的视频，保留末帧")
        }
        if let line = PlaylistCadence.unverifiedCallerError(count: CallerValidation.unverifiedCount) {
            lines.append(line)
        }
        return lines
    }
    private func runtimeStructure() -> PlaylistCadence.RuntimeStructure {
        let state = PlaybackStore.snapshot
        return PlaylistCadence.RuntimeStructure(
            appliedRevision: config.revision,
            activeDisplayKeys: activeKeys.sorted(),
            openVideoIDs: lastOpenIDs.sorted(),
            videoIDs: sessions.mapValues(\.videoID),
            errors: runtimeErrors(),
            keepAwakeActive: awake.isActive,
            powerSavingActive: powerSavingActive,
            displaysAsleep: state.displaysAsleep,
            screenLocked: state.screenLocked,
        )
    }

    private func save() {
        guard writeBackoff.allowsWrite(at: Date()) else { return }
        SessionPruning.prune(&sessions, active: activeKeys, now: Date().timeIntervalSince1970)
        let structure = runtimeStructure()
        let runtime = RuntimeState(
            appliedRevision: structure.appliedRevision,
            ruleSignature: persistedRule,
            updatedAtUnix: Date().timeIntervalSince1970,
            displays: sessions.values.sorted { $0.displayKey < $1.displayKey },
            activeDisplayKeys: structure.activeDisplayKeys,
            openVideoIDs: structure.openVideoIDs,
            keepAwakeActive: structure.keepAwakeActive,
            powerSavingActive: structure.powerSavingActive ? true : nil,
            displaysAsleep: structure.displaysAsleep ? true : nil,
            screenLocked: structure.screenLocked ? true : nil,
            errors: structure.errors,
        )
        do {
            try JSONEncoder().encode(runtime).write(to: Self.runtimeURL, options: .atomic)
            lastSave = Date()
            lastWritten = structure
        } catch {
            writeBackoff.failed(at: Date())
            extensionLog("[Playlist] Cannot save progress: \(error)")
        }
    }
}
