# GlassSegmented

Liquid Glass 分段控件：一条胶囊轨道，一块玻璃透镜压在选中段上，可点、可拖、可用键盘切换。

源码：`glass_controls.go` 的 `glassSegmented(c, &selected, label, disabled, labels...)`，返回是否改变。

**交互**
- 点击某段即选中；按住拖动时透镜膨胀成清透玻璃跟手移动，松手吸附到最近的段。移动超过 `drag-threshold` 才算拖动。
- 透镜下的文字随膨胀放大到 `lens-magnify`（1.22 倍），像透过透镜看。
- Enter / Space 切到下一段；聚焦时左右方向键在段间切换。
- 无玻璃档透镜不膨胀，以 `solid-raised` 作选中底。

**调用方提供**
- `*int` 选中索引、一个用于无障碍的标签（「外观」「切换时机」）和 2–3 个短标签（2–4 个汉字）。
- 返回 true 时保存设置。

**规格**：每段 `segment-width` × 30px，轨道内边距 3px，底色 `surface`；透镜静止着色 `lens-rest`，膨胀后描边 `lens-rim`；标签 `body-strong`，未选中 `text-muted`。

**不要**：超过 3 段，或标签长短悬殊；那种情况改用菜单按钮。
