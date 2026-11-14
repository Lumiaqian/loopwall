# GlassSwitch

Liquid Glass 开关：开启时轨道为 `accent`，按住时白色旋钮膨胀成比轨道更大的清透透镜，可拖动。

源码：`glass_controls.go` 的 `glassSwitch(c, &on, label, disabled)`。

**交互**
- 轻点切换；拖动时旋钮跟手，松手按位置决定开关，会抵消系统同时触发的那次切换。
- 轨道颜色在 Oklab 中随旋钮位置从 `surface-pressed` 渐变到 `accent`，让透镜有颜色可折射。
- 按住期间每 `drag-frame` 请求一帧，保证松手那一帧立即绘制。

**调用方提供**：`*bool` 和无障碍标签；放在设置行（`SettingsGroup`）右侧，行标题说明开启后的效果。

**规格**：`switch-width` × 28px；旋钮 30×22，静止 `knob-rest` 加 `shadow-knob`，膨胀为 42×34、描边 `lens-rim`。无玻璃档旋钮为纯白。
