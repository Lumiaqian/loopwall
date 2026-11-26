import Foundation
import os
import QuartzCore

/// The resources behind one hosted surface: its remote `CAContext`, the root layer
/// WallpaperAgent composites, and the renderer drawing into it.
struct HostedSurface: @unchecked Sendable {
    let caContext: AnyObject // CAContext (private class, hold as AnyObject)
    let contextId: UInt32
    let rootLayer: CALayer
    var renderer: VideoRenderer?
    let displayID: UInt32?
    var videoID: String?
    /// Set from the acquire's `isPreview` flag. `hasLiveRenderer(onDisplay:isPreview:)`
    /// filters on it so a preview-first / desktop-second boot doesn't take the desktop
    /// acquire for a switch (which would defer its reply and leave the desktop black).
    let isPreview: Bool
    /// The geometry (points) and backing scale the layer tree was last laid out for,
    /// so a re-acquire at a new resolution re-frames the layers.
    var destSize: CGSize
    var scaleFactor: CGFloat
    /// A `VideoRenderer.create` is in flight for this surface. Stops a racing acquire
    /// from building a duplicate renderer on the same root layer.
    var creationClaim = RendererClaim()
}

/// Owns every hosted surface's resources, keyed by surface. Playback decisions live in
/// `PlaybackStore`; this is only the mechanism the acquire/invalidate protocol drives.
final class SurfaceRegistry: Sendable {
    static let shared = SurfaceRegistry()

    private struct Contents: @unchecked Sendable {
        var surfaces: [SurfaceKey: HostedSurface] = [:]
        /// WallpaperID UUID → surface, learned at acquire, so `invalidate(UUID)` and
        /// `update(UUID)` resolve their surface.
        var keyForWallpaperUUID: [UUID: SurfaceKey] = [:]
        var cacheDirectoryURL: URL?
    }

    private let lock = OSAllocatedUnfairLock(initialState: Contents())

    private init() {}

    // MARK: - Surfaces

    func surface(for key: SurfaceKey) -> HostedSurface? {
        lock.withLock { $0.surfaces[key] }
    }

    func install(_ surface: HostedSurface, for key: SurfaceKey) {
        lock.withLock { $0.surfaces[key] = surface }
    }

    /// Every hosted surface's key, for a pass over all surfaces outside the lock.
    var surfaceKeys: [SurfaceKey] {
        lock.withLock { Array($0.surfaces.keys) }
    }

    /// Claim the right to create this surface's renderer. True only when the surface
    /// has no renderer and no create in flight, in which case a create is marked
    /// pending. This is what keeps racing desktop + preview acquires to one renderer.
    func claimRendererCreate(for key: SurfaceKey, root: CALayer) -> Bool {
        let identity = ObjectIdentifier(root)
        let claimed = lock.withLock { contents -> Bool in
            guard var surface = contents.surfaces[key], surface.renderer == nil,
                  surface.creationClaim.reserve(root: identity, expected: ObjectIdentifier(surface.rootLayer)) else { return false }
            contents.surfaces[key] = surface
            return true
        }
        traceLog("  [claimRendererCreate] \(key) → \(claimed ? "CLAIMED" : "denied (renderer exists or create pending)")")
        return claimed
    }

    /// A completion owns only its original layer tree. Check identity and consume
    /// the pending claim together so an old completion cannot change a new surface.
    func finishRendererCreate(for key: SurfaceKey, root: CALayer, renderer: VideoRenderer? = nil, videoID: String? = nil) -> Bool {
        let identity = ObjectIdentifier(root)
        return lock.withLock { contents in
            guard var surface = contents.surfaces[key], surface.creationClaim.finish(root: identity) else { return false }
            if let renderer {
                surface.renderer = renderer
                surface.videoID = videoID
            }
            contents.surfaces[key] = surface
            return true
        }
    }

    /// Record a surface's geometry, returning the surface only when it changed since
    /// the layers were last laid out.
    func updateGeometryIfChanged(destSize: CGSize, scaleFactor: CGFloat, for key: SurfaceKey) -> HostedSurface? {
        lock.withLock { contents -> HostedSurface? in
            guard var surface = contents.surfaces[key] else { return nil }
            if surface.destSize == destSize, surface.scaleFactor == scaleFactor {
                return nil
            }
            surface.destSize = destSize
            surface.scaleFactor = scaleFactor
            contents.surfaces[key] = surface
            return surface
        }
    }

    /// Record the choice a surface shows after an in-place `switchVideo`.
    func updateVideoID(_ videoID: String?, for key: SurfaceKey) {
        lock.withLock { $0.surfaces[key]?.videoID = videoID }
    }

    /// Run `body` for each renderer, outside the lock.
    func forEachRenderer(_ body: (VideoRenderer) -> Void) {
        let renderers = lock.withLock { $0.surfaces.values.compactMap(\.renderer) }
        renderers.forEach(body)
    }

    /// Stop one surface's renderer and invalidate its context, leaving the others
    /// playing. Returns whether there was anything to tear down.
    @discardableResult
    func tearDown(_ key: SurfaceKey) -> Bool {
        PlaylistController.shared.checkpoint()
        guard let removed = lock.withLock({ $0.surfaces.removeValue(forKey: key) }) else { return false }
        removed.renderer?.stop()
        invalidateRemoteContext(removed.caContext)
        return true
    }

    var count: Int {
        lock.withLock { $0.surfaces.count }
    }

    /// Whether this display already has a live renderer in the same role (preview or
    /// desktop): an existing Loopwall surface WallpaperAgent keeps compositing in the
    /// same `CALayerHost` while the new context comes up. On a cold start (nothing to
    /// hold for this role) the acquire replies as soon as the poster still is up; on a
    /// same-role switch it defers the reply until the new context renders video.
    func hasLiveRenderer(onDisplay displayID: UInt32, isPreview: Bool) -> Bool {
        lock.withLock { contents in
            contents.surfaces.values.contains { $0.displayID == displayID && $0.renderer != nil && $0.isPreview == isPreview }
        }
    }

    // MARK: - WallpaperID mapping

    func register(wallpaperID uuid: UUID, as key: SurfaceKey) {
        lock.withLock { $0.keyForWallpaperUUID[uuid] = key }
    }

    func key(forWallpaperID uuid: UUID) -> SurfaceKey? {
        lock.withLock { $0.keyForWallpaperUUID[uuid] }
    }

    func forget(wallpaperID uuid: UUID) {
        lock.withLock { _ = $0.keyForWallpaperUUID.removeValue(forKey: uuid) }
    }

    // MARK: - Cache directory

    /// The agent-provided cache directory from the latest acquire, where the BMP
    /// snapshot for the lock-screen fallback goes.
    var cacheDirectoryURL: URL? {
        get { lock.withLock { $0.cacheDirectoryURL } }
        set { lock.withLock { $0.cacheDirectoryURL = newValue } }
    }
}

/// Force the WindowServer to reclaim a remote `CAContext`. Dropping our reference is
/// not enough: WallpaperAgent's `CALayerHost` keeps the layer tree resident in the
/// render server until the context is explicitly invalidated, so every switch would
/// otherwise pin a tree (escalating composite cost / gray, reset only by
/// `killall WallpaperAgent`).
func invalidateRemoteContext(_ caContext: AnyObject) {
    let sel = NSSelectorFromString("invalidate")
    guard let object = caContext as? NSObject, object.responds(to: sel) else { return }
    object.perform(sel)
}
