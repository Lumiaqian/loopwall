import CoreGraphics

/// How large a still frame has to be. The layers fill their display with
/// resizeAspectFill, so a frame only needs to cover the display's pixels; decoding the
/// video's full resolution (8K sources) for it is wasted memory and time. Pure,
/// compiled into the test target.
nonisolated enum SnapshotSizing {
    /// The smallest `maximumSize` for `AVAssetImageGenerator` whose aspect-fit frame
    /// still covers `displayPixels`, never larger than the video itself. `videoSize` is
    /// the displayed size (natural size with the preferred transform applied); signs
    /// are ignored. Nil when either size is empty, which leaves the generator's own
    /// behavior.
    static func maximumSize(videoSize: CGSize, displayPixels: CGSize) -> CGSize? {
        let video = CGSize(width: abs(videoSize.width), height: abs(videoSize.height))
        let display = CGSize(width: abs(displayPixels.width), height: abs(displayPixels.height))
        guard video.width > 0, video.height > 0, display.width > 0, display.height > 0,
              video.width.isFinite, video.height.isFinite, display.width.isFinite, display.height.isFinite
        else { return nil }
        let scale = max(display.width / video.width, display.height / video.height)
        guard scale < 1 else { return video }
        return CGSize(
            width: min(video.width, (video.width * scale).rounded(.up)),
            height: min(video.height, (video.height * scale).rounded(.up)),
        )
    }
}
