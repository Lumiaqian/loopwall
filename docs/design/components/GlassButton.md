# GlassButton

胶囊形的交互玻璃按钮，`prominent` 时以 `accent` 着色，用于页面的主操作。

源码：`widgets.go` 的 `glassButton(c, svg, label, prominent)`。

**何时用**
- 每页最多一个 prominent 按钮，放在悬浮标题栏右侧（「导入视频」）或修复横幅里（「修复壁纸」）。
- 其余操作用普通玻璃按钮：「系统墙纸设置」「打开」「换一个」「播放：轮播」。
- 行内的次要动作（上移、下移、更多、移出轮播）用 28px 图标按钮 `iconButton`，不用玻璃。

**调用方提供**
- 一个 Lucide 风格图标（可为空）和中文动词标签；标签即无障碍名称。
- 点击处理用 `OnClick`；带菜单时用 `.Menu(...)`，菜单由系统原生绘制。
- 忙碌时 `Disabled(a.busy)`，标签换成「处理中…」这类进行时。

**规格**：内边距 `space-8` × `space-16`，图文间距 `space-6`，`body-strong`，圆角 `radius-capsule`。prominent 的文字用 `on-accent`（五种预设强调色均满足 ≥ 3:1 对比度）。无玻璃档下普通按钮用 `solid-raised` 加 `shadow-raised`。

**不要**：把玻璃按钮放在另一块玻璃上叠两层；用颜色以外的方式区分危险操作（删除走系统确认对话框）。
