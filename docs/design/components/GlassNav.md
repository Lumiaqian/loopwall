# GlassNav

侧栏导航：一块悬浮的玻璃面板，选中页下方是一枚以 `accent` 着色的玻璃透镜，切换时滑到新位置并在途中膨胀成清透玻璃。

源码：`glass_controls.go` 的 `glassNav(c, &section, items)` 与 `views.go` 的侧栏。

**何时用**：只用于主窗口侧栏的四个分区：资料库、播放与显示器、附近的 Mac、设置。不要用系统 `ui.Sidebar` 的实色强调块。

**调用方提供**
- `navItem{id, icon, label, badge}` 列表；`badge` 是计数（视频数、找到的 Mac 数），为 0 或无意义时留空。
- 选中状态写回 `*string`；上下方向键也会切换。

**规格**
- 侧栏宽 `sidebar-width`，离窗口边 `space-8`，圆角 `radius-sidebar`，材质为侧栏玻璃（Clarity 见 `clarity-sidebar-*`）。
- 行高 `nav-row-height`，行距 `space-2`，左右内缩 `space-10`，圆角 `radius-lens`；图标 16px 用 `accent`，选中项文字 600。
- 透镜着色 `accent` 24%，按钮层 ZIndex 高于透镜，文字始终清晰。
- 底部「正在播放」列出每块显示器：状态点（`success` / `warning` / `text-muted`）加名称和 `caption`。
