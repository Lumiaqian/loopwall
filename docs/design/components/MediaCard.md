# MediaCard

资料库里的一个视频：带阴影的缩略图、压在画面上的玻璃标签，下面是名称、元信息和操作。

源码：`views.go` 的 `mediaCard` 与 `nearby.go` 的 `remoteCard`。

**画面上的控件用 Clear 玻璃。** 时长标签、悬停播放键、下载百分比都用 `media-dim` 压暗的清透玻璃配 `on-media` 白字：这是 Apple 对照片和视频上控件的要求。轮播序号「#1 轮播」用 `accent` 实色标签。

**调用方提供**
- 缩略图（无则显示 `film` 图标占位）、名称、`分辨率 · 帧率` 元信息；被固定的视频追加「· 固定在 某显示器」，远端视频追加文件大小。
- 右侧操作：加入或移出轮播（`plus` / `check`，后者用 `accent`），更多菜单（`more`）。远端视频换成「导入」玻璃按钮、进度百分比加取消，或「已导入」。

**规格**：网格最小列宽 `media-card-width`、间距 `space-18`；缩略图圆角 `radius-thumb`、阴影 `shadow-thumb`；标签内边距 3 × 9、`caption-strong`、距角 8px；名称 `body-strong`、元信息 `caption`，都配 `text-halo`。
