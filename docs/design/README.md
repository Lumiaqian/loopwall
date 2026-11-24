# Loopwall 设计系统

为 Loopwall 做界面时以这里为准：

- 本文：文案、分层、三档外观、颜色、字体、间距、动效与图标的用法规则。
- [`tokens.json`](tokens.json)：全部设计 token（两套主题的颜色、字体层级、间距、圆角、阴影、尺寸、模糊与玻璃参数），每个都注明用途。
- [`components/`](components/)：8 个组件的使用规范：何时用、调用方提供什么、规格、不要做什么。
- 图标即 [`icons.go`](../../icons.go)，应用图标即 [`resources/`](../../resources/)。

代码是最终来源：token 的值取自 `look.go`、`prefs.go`、`views.go`、`widgets.go`、`glass_controls.go` 与 MyGO 主题。改这些代码里的颜色、尺寸或玻璃参数时，同步改 `tokens.json`；[`design_test.go`](../../design_test.go) 会比对两边，不一致就失败。

Loopwall 是一款 macOS 本地视频动态壁纸应用：Go（MyGO）原生管理端加 Swift 墙纸扩展。界面以 Liquid Glass 为语言，但让壁纸本身当主角：玻璃只给浮在内容上方的导航和控件，内容层安静、可读。

这套设计系统写给之后为 Loopwall 做界面的人和 agent。规则里的名字都是 `tokens.json` 里的 token、type style 和资源；数值与源码（`look.go`、`widgets.go`、`glass_controls.go`、MyGO 主题）一致。

## 文案

- 全部中文，简体。专有名词保留原文：macOS、Finder、Bonjour、MP4、4K、Mac。
- 短句，直说结果。按钮用动词：「导入视频」「配对」「换一个」「修复壁纸」；进行时用「处理中…」「配对中…」。
- 引用界面里的名字用直角引号：「设置 → 局域网共享」「Loopwall 轮播」。
- 提示条说已经发生的事：「已保存，壁纸会自动更新」。失败说原因和下一步：「连接被拒绝：对方可能已关闭共享」。不道歉，不用感叹号，不用 emoji。
- 设置项说明写清开启后会怎样、有没有副作用：「解锁、拔掉电源或停用本墙纸时自动释放，不改变系统密码要求。」
- 数字与单位之间：分辨率写「4K」，帧率写「60 fps」，大小写「1.1 GB」，时长写「0:40」，元信息用「 · 」分隔。

## 两层结构

Loopwall 遵循 Apple 的分层：**功能层**是 Liquid Glass，浮在**内容层**之上；内容层用标准材质，绝不用玻璃。

| 层 | 元素 | 材质 |
| --- | --- | --- |
| 背景 | 当前壁纸的缩略图，模糊后加色调 | `blur-backdrop-*` + `backdrop-tone-*`；「无」档为 `solid-background` |
| 内容层 | 设置分组、显示器卡片、播放顺序列表、资料库卡片 | `content-standard` / `content-max`（加 `blur-content-max`），边线 `hairline`；「无」档 `solid-pane` |
| 功能层 | 侧栏、标题栏按钮、分段控件、开关、提示条、拖入提示 | Liquid Glass（Regular），侧栏带 Clarity |
| 媒体上 | 缩略图上的时长、播放键、下载进度 | Clear 玻璃，`media-dim` 压暗，`on-media` 白字 |

规则：
- 不要让玻璃叠在玻璃上（侧栏里的导航透镜是唯一例外，它以 `accent` 着色区分）。
- 新增一个控件前先问：它浮在内容上方吗？是就用玻璃，否则用内容层材质。
- 页面标题与副标题直接压在背景上，用 `text` / `text-on-backdrop-muted` 并加 `text-halo` 光晕；其下的滚动内容以 `edge-tone-*` 渐变加 `blur-header` 渐进模糊淡出。

## 三档外观

用户在「设置 → 液态玻璃」选择，界面必须三档都成立：

- **无**：`solid-background` 底，`solid-pane` 面板，`solid-raised` 凸起控件加 `shadow-raised`。最清晰、最省电；减少透明度时等同此档。
- **标准**（默认）：壁纸模糊 `blur-backdrop-standard`，色调 `backdrop-tone-standard`，内容面板 `content-standard`，侧栏 Clarity `clarity-sidebar-standard`。
- **极致**：壁纸只模糊 `blur-backdrop-max`，让玻璃边缘有细节可折射；内容面板 `content-max` 叠 `blur-content-max` 保可读；深色模式背景单独压暗（`backdrop-tone-max` 深色 0.42）。

## 颜色

- 文字只用 `text`、`text-muted`、`text-on-backdrop-muted`。前两者在内容层面板上读；压在背景上的字用后者加 `text-halo`。
- 控件、悬停、选中、分隔线都用 `text` 的透明着色：`surface`（6%）、`surface-hover`（8%）、`surface-pressed`（12%）、`border`（12%），让下层透出。不要用实色灰。
- `accent` 是用户选的强调色（默认 `accent-violet`，另有 `accent-blue`、`accent-pink`、`accent-orange`、`accent-green` 或跟随系统）。只用于：主按钮填充、开关开启、侧栏透镜与图标、配对码、「#N 轮播」标签、选中项的勾。一屏里强调色的面积要小。
- 强调色上的字用 `on-accent`。注意：橙色、绿色加白字低于 3:1，粉色、蓝色只够 3:1，紫色 4.26:1；这些是现有取值，改动前先评估。
- 状态：`success` 正在播放，`warning` 等待系统显示或需要修复，`text-muted` 未启用。状态点总与文字状态同时出现，不单靠颜色区分。
- 缩略图上不要用实色或 `surface` 底，用 Clear 玻璃加 `media-dim`。

## 字体

- 只用系统字体 `font-system`（SF Pro + 苹方），不打包字体文件。macOS 上默认字号 13。
- 层级：`page-title` 20/700 → `app-name` 17/700 → `empty-title` 16/700 → `section-title` 13/700 → `body-strong` 13/600 → `body` 13/400 → `detail` 12 → `caption` 11 → `caption-strong` 11/600。特例：配对码 `pairing-code` 20/700，`mono` 等宽字体，四位一组分两行。
- 名称、元信息单行省略，不折行；说明文字最多两行。

## 间距、尺寸与圆角

- 间距取自 `space-*`：页面边距 `space-24`，分区间距 `space-26`，分区标题到分组 `space-10`，设置行内边距 `space-12` × `space-14`，网格间距 `space-18`。
- 窗口骨架：`titlebar-height` 52（隐藏式标题栏，侧栏顶部留给拖动），`header-height` 72（悬浮标题区），`sidebar-width` 220，侧栏离窗口边 `space-8`。
- 圆角由内向外同心：控件一律 `radius-capsule`；导航行 `radius-lens`；缩略图 `radius-thumb`；设置分组 `radius-group`；显示器卡片 `radius-card`；侧栏 `radius-sidebar`（与窗口圆角同心）。
- 玻璃不投影；只有缩略图用 `shadow-thumb` 浮起，开关旋钮静止时用 `shadow-knob`。

## 动效

- 所有过渡用 MyGO 的 `ui.Smooth` 弹簧，不设固定时长；可打断，跟手。
- 拖动控件时每 `drag-frame`（16ms）请求一帧；移动超过 `drag-threshold`（3px）才算拖动。
- 玻璃控件按下或移动时膨胀成清透透镜（分段控件、开关、侧栏透镜），静止时收回成磨砂。
- 拖入文件时整窗以 `drop-dim` 压暗，玻璃提示面板从 0.94 缩放淡入。
- 尊重减少动态效果与减少透明度：后者让所有玻璃退为不透明面板。

## 图标

- 线性图标，Lucide 风格：24×24 视框、2px 描边、圆头圆角、`currentColor`。源码 `icons.go`，这里的 `Icons` 组逐一收录。
- 尺寸：正文旁 13–16px，图标按钮 15px，空状态 28px，拖入提示 34px。颜色随文字；侧栏导航与空状态图标用 `accent`。
- 新图标照同一规格画，或从 Lucide 取同名图标；不用 emoji 当图标，不用填充式图标。

## 品牌标记

- 应用图标见 `Logos`：两块错开的屏幕，前一块正播放一幅场景，用 Icon Composer 制作（源文件 `resources/Loopwall.icon`）。
- 没有单独的字标：名称「Loopwall」以 `app-name` 设在侧栏顶部，用系统字体，不加装饰。


## 组件

- [GlassButton](components/GlassButton.md)：胶囊形的交互玻璃按钮，`prominent` 时以 `accent` 着色，用于页面的主操作。
- [GlassSegmented](components/GlassSegmented.md)：Liquid Glass 分段控件：一条胶囊轨道，一块玻璃透镜压在选中段上，可点、可拖、可用键盘切换。
- [GlassSwitch](components/GlassSwitch.md)：Liquid Glass 开关：开启时轨道为 `accent`，按住时白色旋钮膨胀成比轨道更大的清透透镜，可拖动。
- [GlassNav](components/GlassNav.md)：侧栏导航：一块悬浮的玻璃面板，选中页下方是一枚以 `accent` 着色的玻璃透镜，切换时滑到新位置并在途中膨胀成清透玻璃。
- [SettingsGroup](components/SettingsGroup.md)：设置分组：分区标题加一块内容层面板，面板里是一行行「标题、说明、控件」，像系统设置那样。
- [MediaCard](components/MediaCard.md)：资料库里的一个视频：带阴影的缩略图、压在画面上的玻璃标签，下面是名称、元信息和操作。
- [GlassToast](components/GlassToast.md)：提示条：一枚玻璃胶囊，从内容区底部升起，报告刚发生的事。
- [EmptyState](components/EmptyState.md)：空状态：在列表还没有内容时，说清是什么、下一步怎么做。
