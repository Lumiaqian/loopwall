# 局域网共享排查手册（给 AI 助手）

本手册写给在用户 Mac 上运行的 AI 编程助手（Claude Code 等）。目标：查清这台 Mac 上的 Loopwall 为什么在「附近的 Mac」里找不到另一台 Mac，或另一台找不到它，并把结论按文末格式报告给用户。

请从头到尾按顺序执行。每一步先跑命令，再按表格判断，最后把输出原样记进报告。

## 0. 规则

- **只读排查**：除非手册写明，不要修改系统设置、不要删除文件、不要结束进程，也不要重启 `WallpaperAgent`（会让壁纸闪一下，而且与本问题无关）。
- **涉及系统设置或弹窗的操作，交给用户在界面上完成**，比如允许本地网络权限、调整防火墙。macOS 不提供读写本地网络权限的命令，不要尝试用 `tccutil` 或改数据库绕过。
- **不要输出密钥**：`~/Library/Application Support/Loopwall/` 下的 `share.json`、`share-key.pem`、`share-cert.pem` 含配对令牌和私钥，只能检查文件在不在，不要 `cat`，也不要上传。
- 终端命令不受 macOS 本地网络权限的限制，所以终端能看到的，只说明「网络是通的」，不代表 Loopwall 也能看到。这一点是判断的关键。

## 1. 先向用户确认这三件事

| 项目 | 说明 |
| --- | --- |
| 对方 Mac 的名称 | 对方「系统设置 → 通用 → 关于本机」里的名称，比如 `MacBook Pro (104)`。它也是对方在 Bonjour 里的服务名 |
| 对方 Mac 的 IP | 对方运行 `ipconfig getifaddr en0` 的结果 |
| 两台是否都打开了共享 | 两台都在 Loopwall「设置 → 局域网共享」里打开了「共享我的资料库」 |

下文用 `<对方名称>`、`<对方IP>` 指代这两个值。

## 2. 版本与基本状态

```sh
A=/Applications/Loopwall.app; [ -d "$A" ] || A="$HOME/Applications/Loopwall.app"
echo "app: $A"
/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$A/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Print :NSBonjourServices' "$A/Contents/Info.plist"
pgrep -lf "Loopwall.app/Contents/MacOS/Loopwall" || echo "Loopwall 没有运行"
ls "$HOME/Library/Application Support/Loopwall/"
python3 -c "import json;print('share on:', json.load(open('$HOME/Library/Application Support/Loopwall/settings.json')).get('share', False))"
```

| 结果 | 处理 |
| --- | --- |
| 版本低于 0.1.2 | 请用户从 https://github.com/Lumiaqian/loopwall/releases/latest 安装最新版，装好后从第 2 步重来。旧版本没有诊断信息 |
| `NSBonjourServices` 不含 `_loopwall._tcp` | 安装包不完整，请用户重新下载安装 |
| Loopwall 没有运行 | 请用户打开 Loopwall。共享方必须一直开着管理端（菜单栏常驻即可） |
| `share on: False` | 请用户在「设置 → 局域网共享」打开「共享我的资料库」 |

## 3. 读 App 自己的诊断（需要用户看界面）

请用户打开 Loopwall「设置 → 局域网共享」，读出「共享我的资料库」下面那一行，再打开「附近的 Mac」页，读出页面上的提示。

| 用户看到的 | 结论 |
| --- | --- |
| 「本地网络权限未开启」，或「附近的 Mac」显示「无法查找附近的 Mac」 | 请用户到「系统设置 → 隐私与安全性 → 本地网络」打开 Loopwall；如果列表里没有 Loopwall，把共享开关关掉再打开以触发系统询问，然后选「允许」。完成后回到本步复查 |
| 绿点「其他 Mac 的『附近的 Mac』里会出现『…』」 | 本机发布成功，继续第 4 步 |
| 一直是「正在发布到局域网…」 | 继续第 4 步，并在报告里注明 |

## 4. 网络层面能否互相发现（终端）

```sh
( dns-sd -B _loopwall._tcp local. & p=$!; sleep 6; kill $p ) 2>&1
```

记下列出的所有 Instance Name。

| 结果 | 结论 |
| --- | --- |
| 有本机，也有 `<对方名称>` | 网络是通的。如果 Loopwall 里仍然看不到对方，问题在本机 App 的本地网络权限（回第 3 步），或对方的连接被拒（看第 6 步） |
| 只有本机 | 两台 Mac 在网络层就互相看不到，继续第 5 步 |
| 连本机也没有 | 本机没发布出去：回第 3 步检查权限，并确认第 2 步里共享已打开、Loopwall 在运行 |

## 5. 两台是否在同一个局域网，代理是否放行了局域网

用户的代理（Clash、Mihomo、Surge 等）**不能关**。本步只检查代理有没有把局域网流量也接管走；如果接管了，给它加「局域网直连」的例外，而不是关闭代理。

```sh
for i in en0 en1; do printf '%s ' $i; ipconfig getifaddr $i || echo -; done
route -n get <对方IP> | grep -E 'interface|gateway'
route -n get 224.0.0.251 | grep -E 'interface'
( dns-sd -B _companion-link._tcp local. & p=$!; sleep 4; kill $p ) 2>&1 | tail -n +5 | wc -l
ping -c 3 -t 5 <对方IP>
```

第 2、3 条看去对方和去 Bonjour 组播地址（`224.0.0.251`）的流量走哪张网卡；第 4 条数一数局域网里能看到多少台苹果设备的服务。

| 结果 | 结论 |
| --- | --- |
| 本机 IP 与 `<对方IP>` 不在同一网段（例如 `172.30.93.x` 对 `10.0.5.x`；按子网掩码算） | Bonjour 无法跨网段发现。请用户让两台连同一个 Wi-Fi 或同一个有线网 |
| `route -n get <对方IP>` 或 `224.0.0.251` 的 `interface` 是 `utun…` | 代理把局域网流量接管了。**不要关代理**，按下面的「代理例外」给它加局域网直连，然后从第 4 步重来 |
| `interface` 是 `en0`/`en1`，第 4 条数出来是 0 或个位数 | 网络屏蔽了组播（mDNS，端口 5353），代理没有问题。这是网络设备的设置，App 无法绕过；用第 7 步的热点对照确认 |
| `interface` 是 `en0`/`en1`，能看到很多设备，同网段但 `ping` 全部超时 | Wi-Fi 开了客户端隔离，或对方防火墙阻止。看第 6 步，或用第 7 步确认 |
| 都正常（同网段、走 `en0`/`en1`、能看到很多设备、能 `ping` 通），但第 4 步仍看不到对方 | 对方没有发布出去：请对方按本手册的第 2、3 步检查自己那台 |

### 代理例外：让局域网直连

只加例外，不要改动其他代理规则；改之前先告诉用户要改哪个文件或哪个界面开关，取得同意。把 `<局域网网段>` 换成本机所在网段（例如 `172.30.88.0/21`），不确定时用下面三个私有网段的全集。

- **Mihomo / Clash Meta / Clash Verge 的 TUN 模式**：在配置里给 `tun` 加排除路由，并在规则最前面加直连：

  ```yaml
  tun:
    route-exclude-address:
      - 10.0.0.0/8
      - 172.16.0.0/12
      - 192.168.0.0/16
      - 224.0.0.0/4   # 组播，Bonjour 用
  rules:
    - IP-CIDR,10.0.0.0/8,DIRECT,no-resolve
    - IP-CIDR,172.16.0.0/12,DIRECT,no-resolve
    - IP-CIDR,192.168.0.0/16,DIRECT,no-resolve
    - DOMAIN-SUFFIX,local,DIRECT
    # ……原有规则保持不变
  ```

  如果开了 `strict-route: true`，它会阻止上面的排除生效，改为 `false`。用的是图形界面（Clash Verge 等）时，一般在「TUN 设置」里有「排除路由 / Route Exclude」和「严格路由」，与上面对应。
- **Surge 增强模式**：`[General]` 里加

  ```ini
  skip-proxy = 127.0.0.1, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, localhost, *.local
  tun-excluded-routes = 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 224.0.0.0/4
  ```

- **其他代理**：找「绕过局域网」「Bypass LAN」「排除私有地址」一类的开关并打开。

改完让代理重新载入配置，再跑本步命令，确认 `interface` 变成 `en0`/`en1`。

## 6. 能否连上对方的共享服务

仅当第 4 步能看到 `<对方名称>` 时执行：

```sh
( dns-sd -L "<对方名称>" _loopwall._tcp local. & p=$!; sleep 4; kill $p ) 2>&1
```

从输出里找 `can be reached at …:<端口>`，记下端口，然后：

```sh
nc -vz -G 5 <对方IP> <端口>
```

| 结果 | 结论 |
| --- | --- |
| `succeeded` | 网络完全通。若 Loopwall 仍报错，把「附近的 Mac」页的报错原文写进报告 |
| `Connection refused` | 对方共享服务没在运行：请对方重新打开共享开关 |
| 超时 | 对方防火墙拦截了传入连接。请对方在「系统设置 → 网络 → 防火墙 → 选项」里允许 Loopwall 接受传入连接，或暂时关闭「阻止所有传入连接」 |

## 7. 手机热点对照测试（可选，最快定位）

请用户用手机开热点，两台 Mac 都只连这个热点（拔掉有线网络），代理保持开着，但确认第 5 步的「代理例外」已经生效，再看两边的「附近的 Mac」。

- 能互相看到：原网络有隔离（网段不同、客户端隔离或屏蔽组播），结论是「网络环境问题」，不是 Loopwall 的问题。
- 仍然看不到：在热点环境下重跑第 3、4、5 步，报告结果。

## 8. 系统日志（补充信息）

```sh
log show --last 15m --style compact --predicate 'subsystem == "local.lumiaqian.loopwall"' 2>/dev/null | tail -30
```

日志可能为空，这不代表有问题；有内容就原样附进报告。

## 9. 报告格式

排查结束后，按下面的格式回复用户（每项照实填写，没做的写「未执行」）：

```text
【Loopwall 局域网排查报告】
本机：版本 x.y.z，Loopwall 运行中/未运行，共享已开/未开
App 诊断（第 3 步）：设置页显示「……」；附近的 Mac 显示「……」
终端发现（第 4 步）：看到的服务名列表：……
网络（第 5 步）：本机 IP ……，对方 IP ……，同网段 是/否，去对方/去 224.0.0.251 走的网卡 ……/……，能看到的设备数 ……，ping 通 是/否，代理例外 已有/已添加/未添加
连接（第 6 步）：端口 ……，nc 结果 ……
热点对照（第 7 步）：……
结论：……（一句话：权限 / 网段 / 网络隔离 / 代理未放行局域网 / 防火墙 / 其他）
建议用户下一步：……
```
