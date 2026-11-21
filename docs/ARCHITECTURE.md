# 契约与播放状态

## 职责

Go 是资料库和配置的唯一产品写入方，Swift 扩展是进度的唯一写入方。实际壁纸视频通过 AVAssetReader → AVSampleBufferDisplayLayer → 远程 CAContext 播放，帧数据不经过 Go。管理端自己的预览使用 MyGO 的系统视频播放器，与桌面会话分开。

原 Phosphene 的 Steam/Workshop、SwiftUI 管理端、视频变体生成、全屏/遮挡/游戏暂停均已移除；电量相关暂停只保留为可选的省电模式（默认关闭）。保留 Apple 调用者验证、独立表面上下文、原生解码、缓存快照、屏幕尺寸恢复与系统生命周期处理。

## JSON v1

所有播放时间单位为秒，日期为 Unix 秒。数组用 `[]` 表示空值，不能用 `null` 替代必填数组。外层 `schemaVersion` 为 1，不支持的版本不覆盖当前有效配置。

`catalog.json` 的字段：

| 字段 | 含义 |
| --- | --- |
| schemaVersion / revision | 契约版本、配置修订号 |
| media | `MediaEntry` 数组 |
| playlist | `PlaylistSpec` |
| removedMediaIDs | Go 内部的显式删除记录，可省略；扩展忽略此字段 |

`MediaEntry` 保存 `id`、`name`、`filename`、`width`、`height`、`durationSeconds`、`frameRate`、`addedAtUnix`。ID 为 UUID，文件名必须是安全的单个路径组件。原片在 `videos/<id>/<filename>`，缩略图为同目录 `thumbnail.jpg`。

`PlaylistSpec` 保存 `videoIDs`、`advanceMode`（`end` 或 `duration`）、`intervalSeconds`（正整数）、`keepAwakeWhenLockedOnAC`，以及可选的 `powerSaving` 与 `pins`：显示器 UUID → 资料库中的视频 ID。被固定的显示器只循环该视频，其余显示器播放列表；固定的视频无法解码或文件缺失时，该屏退回播放列表而不黑屏。`powerSaving` 为 true 时，用电池、开启低电量模式或热状态达到 serious/critical，所有表面都暂停并停在当前画面，30 秒后原有的深度暂停再释放解码器；扩展用 `NSProcessInfoPowerStateDidChange`、热状态通知和 IOPS 电源通知取得这些条件，仅在条件变化时发出事件，由纯函数 reducer 决定目标。`pins` 与 `powerSaving` 字段可省略，旧配置无需迁移，`schemaVersion` 仍为 1。

`runtime.json` 保存 `schemaVersion`、`appliedRevision`、`updatedAtUnix`、`ruleSignature`、`displays`、`activeDisplayKeys`、`openVideoIDs`、`keepAwakeActive`、可选的 `powerSavingActive`、`errors`。`powerSavingActive` 为 true 表示壁纸被省电模式有意暂停，此时 `activeDisplayKeys` 为空不算卡住，管理端不会提示修复。每个 `DisplayProgress` 保存 `displayKey`、`videoID`、`positionSeconds`、`itemElapsedSeconds`。

Go 通过同目录临时文件、同步并原子 rename 发布配置；扩展通过 `.atomic` 写运行进度。Darwin 通知 `local.lumiaqian.loopwall.libraryChanged` 通知扩展重读，扩展无进程时由下次启动读取。管理端读取运行进度并标明陈旧状态。

## 导入与删除

导入复制到隐藏临时目录，原生验证视频轨道、真实尺寸、时长以及能否解码首帧，完成后再改名为 UUID 目录，发布配置后才进入列表。失败不改动原片，也不会向配置暴露半成品。

删除将素材从资料库索引、播放列表和指向它的 `pins` 中移除，并在同一份原子配置中记录其 ID。扩展先切到下一个可播放项目；只有其确认新配置、运行记录仍新鲜、且所有原生读者不再持有该 ID 后，管理端才清理资料库副本。清理只处理显式删除记录，绝不把尚未发布的批量导入文件当作垃圾。

列表为空时保留已经显示的末帧，并在管理端提示。如果末帧仍由旧原片支持，副本会暂留到播放器真正释放该素材。原始文件和原 Phosphene 的资料库均不参与清理。

## 局域网共享

管理端在「共享我的资料库」打开时运行只读 HTTPS 服务（`internal/share`），端口随机，并由原生模块用 Bonjour 发布为 `_loopwall._tcp`，TXT 记录携带本机设备 ID。查找在用户打开「附近的 Mac」后才开始（`NWBrowser`），以连接得到对方的 IPv4 地址和端口，再排除本机。

- 身份：每台 Mac 在 `~/Library/Application Support/Loopwall/share.json` 保存随机设备 ID、已授权的设备（只存令牌的 SHA-256）、已配对的对方（令牌与证书指纹）以及导入记录；自签名证书和私钥同目录，权限 0600。
- 配对码（版本 2）：共享方显示的配对码是它证书 SHA-256 的前 16 字节（128 位）加上一次性密码（原来的 6 位数字，20 位），共 148 位，按 RFC 4648 base32 编码为 30 个字符（补 2 位 0），切成前后两半各 15 个字符，每半后接一个 Luhn mod-32 校验字符（与 Syncthing 设备 ID 相同的做法），共 32 个字符，显示为 8 组 4 字符、以 `-` 连接。输入时不分大小写，忽略空格和 `-`，`0`/`1`/`8` 读作 `O`/`I`/`B`；两个校验字符在联网前先验证，输错一个字符就能发现。指纹本身是公开的（任何人都能握手取得证书），证明用户看过共享方屏幕的是密码；指纹则让输入方在发出任何内容之前确认对方是谁。
- 配对流程：输入方解析配对码，用 TLS 1.3 连接共享方，并出示本机证书作为客户端证书；握手时 `VerifyConnection` 要求对方证书的 SHA-256 以码中的 16 字节开头，否则握手失败（`ErrFingerprintMismatch`），密码、反向令牌和 HTTP 请求都不会发出。握手完成（对方已证明持有私钥）后，输入方才为对方授予反向令牌，并绑定到观察到的对方完整指纹；随后 `POST /v1/pair` 带上 `version: 2`、密码和反向授权（令牌与本机证书完整指纹）。共享方用 `ClientAuth: RequestClientCert`（索要但不强制，旧版不出示），v2 请求必须出示客户端证书且其指纹等于 `back.fingerprint`，否则在比较密码之前就返回 400；成功后把客户端证书指纹记入该设备的授权。此后 `authorized()` 对带指纹的授权，除令牌外还要求本次连接出示的证书与之相符，所以令牌泄露也不够用；双向都是如此。输入方固定共享方完整指纹，此后指纹不符即拒绝连接并要求重新配对。密码用过一次即更换，连续输错 5 次作废，需在共享方「换一个」；格式不对的请求不计入输错次数。共享方可随时取消某台设备的授权，接收方也可取消配对。
- 旧版路径：0.1.3 起的旧版只输入 6 位数字，不出示证书、不带 `version`。新版输入方遇到恰好 6 位数字，走原来的流程（首次连接即信任），并把配对记为 `verified: false`；已有的旧记录没有该字段，同样视为旧版。新版共享方默认拒绝这类请求（403，响应头 `X-Loopwall-Error: legacy-disabled`；旧版客户端只把 403 显示为「配对码不对」，新版输入方则据此提示对方需要允许旧版配对）。用户在设置里打开「允许旧版 Loopwall 配对（不安全）」（保存在 `settings.json` 的 `shareLegacy`）后才接受，此时设置页同时显示 6 位数字并警告它无法发现冒充。即便打开了这个选项，已用新配对码配对过的设备也不会被旧版方式重新配对，以免 6 位数字替换掉已绑定证书的授权和已固定的指纹。旧版配对的授权没有证书指纹，仍只凭令牌；界面把它们标为「旧版配对（不安全）」，建议重新配对。
- 同名提示：「附近的 Mac」中有两台及以上同名的 Mac 时，每一台都显示警告，提示核对配对码。
- 接口：`POST /v1/pair`、`POST /v1/unpair`、`GET /v1/library`、`GET /v1/thumbnail/{id}`、`GET /v1/video/{id}`（支持 Range）。除配对外都需令牌；文件只按资料库中存在的视频 ID 定位，不接受请求给出的路径。
- 导入：下载直接写入导入暂存目录（`catalog.ImportFrom`），与本地导入相同地原生验证、生成缩略图、改名发布，失败或取消不留半成品；完成后加入资料库与播放列表末尾。扩展只播放本地副本，从不读网络。

## 每屏会话

持久身份为 `CGDisplayCreateUUIDFromDisplayID` 对应 UUID，显示器名称只用于界面，系统运行时数字 ID 只用于当前远程表面。初次出现的屏幕按系统当前屏幕顺序在完整播放周期中均匀错开；已保存的屏幕恢复自己的视频和片内位置。

桌面与锁屏的远程层各有自己的 CAContext，避免同一上下文被两个宿主抢占。它们共享同一显示器的逻辑进度。设置预览永远不建立或推进持久会话。表面交接时检查时间轴，必要时对齐本屏的会话。

轮播增量来自原生展示时钟，并限制在已排入展示层的时间范围；AVAssetReader 提前读到 EOF 只准备无缝循环，不触发换片。换素材、seek 或换表面会建立新的时钟基线，避免把时钟原点变化计入播放时长。

在 `end` 模式按视频剩余时长切换。在 `duration` 模式按本项累计展示时长计时，短片可循环多次。修改规则保留片内位置，重置本项指定时长计时。素材缺失、被删或解码失败时按原顺序跳到下一有效项。

## 电源与生命周期

系统的屏幕休眠、断开和 suspended 表面会停止推进其时钟；持续挂起会释放读者，恢复后重建。无窗口覆盖或全屏应用检测器，也没有用户暂停标记。

接电锁屏亮屏请求由扩展持有，使用 `PreventUserIdleDisplaySleep`。锁状态通过系统通知及 sessionagent 状态读取，电源状态由 IOKit 读取。解锁、拔电、选项关闭、墙纸停用和扩展退出都会释放。它不改变密码、登录窗口或系统认证配置。

## 系统边界

墙纸接入使用上游已验证的私有 WallpaperExtensionKit 接口。当前本机 ad-hoc 签名可以注册、加载并读写独立容器；这一结果只代表已实测的 macOS 版本和本机环境，不是 Developer ID 公证发行验证。公开发行不在首版范围。

上游 Phosphene 的同步基线与已移植、未移植的修复记录在 [上游同步基线](upstream/SYNC.md)。
