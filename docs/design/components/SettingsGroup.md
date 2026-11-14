# SettingsGroup

设置分组：分区标题加一块内容层面板，面板里是一行行「标题、说明、控件」，像系统设置那样。

源码：`widgets.go` 的 `sectionTitle`、`group`、`groupRow`。

**这是内容层，不是玻璃。** 面板用标准材质：「标准」档 `content-standard`，「极致」档 `content-max` 叠 `blur-content-max`，「无」档 `solid-pane`；边线一律 `hairline`。玻璃只留给浮在其上的控件。

**调用方提供**
- 分区：一个图标和短标题（「外观」「液态玻璃」「通用」「局域网共享」「启用与资料库」）。
- 每行：标题（`body`）、可选说明（`caption` / `text-muted`，写清开启后会怎样、有无副作用），以及右侧控件（开关、分段、按钮、输入框）。
- 行之间用 `Divider`（`border`）。

**规格**：分区标题 `section-title`，`text-on-backdrop-muted` 加 `text-halo`，与分组间隔 `space-10`；分区之间 `space-26`；行内边距 `space-12` × `space-14`，标题与控件间距 `space-16`；圆角 `radius-group`。设置页内容最大宽 720px，居中。
