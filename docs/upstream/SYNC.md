# 上游同步基线

Loopwall 的壁纸扩展从上游 Phosphene 分叉而来。上游的修复不会自动进入本仓库，所以这里记录同步到哪里、哪些已移植、哪些还没有。

## 基线

| 项目 | 值 |
| --- | --- |
| 分叉点 | 上游 Phosphene v1.11.4（`cee21ce`，即本仓库 `920b222^`） |
| 最近一次对照 | 上游 v1.11.6（`3ec405d`），2026-11-21 |

## 上游提交的处理

| 提交 | 内容 | 状态 |
| --- | --- | --- |
| `c6e8118` | 仅在扩展的恢复时间戳新鲜时才响应 agentStuck | 已移植：管理端只在 `loopwall-last-recovery` 距今 -1 秒到 10 秒之内时自动重建 WallpaperAgent（`recovery.go`） |
| `5a9a3e0` | 已连接显示器变化时重启 WallpaperAgent | 未移植：等合盖等场景的真机验证（见代码审查报告“待确认”第 1 条）确认问题存在后再决定 |
| `45cd935` | Phosphene SwiftUI 管理端里的 Space 所有者检查 | 不适用：Loopwall 已用 MyGO 管理端替换了该部分 |
| `ddadfad` | AppUpdater 版本升级 | 不适用：Loopwall 使用 MyGO 的更新器 |

## 如何再次对照

```sh
git log cee21ce..<新的上游提交> -- PhospheneExtension
```

只需要看扩展目录：管理端部分 Loopwall 已重写。逐条判断后更新上表，并把“最近一次对照”改为新的上游版本和日期。
