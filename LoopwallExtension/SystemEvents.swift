import AppKit
import CoreGraphics
import IOKit.ps
import os

/// Turns display sleep/wake and loginwindow lock/unlock into playback events.
enum SystemEvents {
    static func start() {
        PlaybackStore.post(.screenLockChanged(isScreenLockedNow()))
        PowerMonitor.start()

        let workspace = NSWorkspace.shared.notificationCenter
        workspace.addObserver(forName: NSWorkspace.screensDidSleepNotification, object: nil, queue: .main) { _ in
            PlaylistController.shared.checkpoint()
            extensionLog("[Extension] Displays asleep")
            PlaybackStore.post(.displaysAsleepChanged(true))
        }
        workspace.addObserver(forName: NSWorkspace.screensDidWakeNotification, object: nil, queue: .main) { _ in
            extensionLog("[Extension] Displays awake")
            PlaybackStore.post(.displaysAsleepChanged(false))
        }

        // loginwindow reports the lock before WallpaperAgent updates any surface, which
        // is what lets the lock screen start playing without waiting on the agent.
        let distributed = DistributedNotificationCenter.default()
        distributed.addObserver(forName: .init("com.apple.screenIsLocked"), object: nil, queue: .main) { _ in
            extensionLog("[Extension] Screen locked")
            PlaybackStore.post(.screenLockChanged(true))
        }
        distributed.addObserver(forName: .init("com.apple.screenIsUnlocked"), object: nil, queue: .main) { _ in
            extensionLog("[Extension] Screen unlocked")
            PlaybackStore.post(.screenLockChanged(false))
        }
    }

    /// The lock state at launch, before any notification arrives.
    private static func isScreenLockedNow() -> Bool {
        guard let session = CGSessionCopyCurrentDictionary() as? [String: Any] else { return false }
        return session["CGSSessionScreenIsLocked"] as? Bool ?? false
    }
}

/// Observes the three inputs of power saving by notification (with a 5 s re-read from
/// the playlist tick, in case a notification never reaches this process), and
/// posts `.powerChanged` only when the combined conditions actually change.
enum PowerMonitor {
    private static let last = OSAllocatedUnfairLock<PowerConditions?>(initialState: nil)

    static func start() {
        refresh()
        let center = NotificationCenter.default
        center.addObserver(forName: .NSProcessInfoPowerStateDidChange, object: nil, queue: .main) { _ in refresh() }
        center.addObserver(forName: ProcessInfo.thermalStateDidChangeNotification, object: nil, queue: .main) { _ in refresh() }
        // Fires when the providing power source or its state changes, on the main run loop.
        if let source = IOPSNotificationCreateRunLoopSource({ _ in PowerMonitor.refresh() }, nil)?.takeRetainedValue() {
            CFRunLoopAddSource(CFRunLoopGetMain(), source, .commonModes)
        }
    }

    static func refresh() {
        let now = read()
        guard last.withLock({ let changed = $0 != now; $0 = now; return changed }) else { return }
        extensionLog("[Extension] Power \(now)")
        PlaybackStore.post(.powerChanged(now))
    }

    private static func read() -> PowerConditions {
        let info = IOPSCopyPowerSourcesInfo()?.takeRetainedValue()
        let source = info.flatMap { IOPSGetProvidingPowerSourceType($0)?.takeUnretainedValue() }
        return PowerConditions(
            onBattery: source as String? == kIOPSBatteryPowerValue,
            lowPowerMode: ProcessInfo.processInfo.isLowPowerModeEnabled,
            thermalState: ProcessInfo.processInfo.thermalState,
        )
    }
}
