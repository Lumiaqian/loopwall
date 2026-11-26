package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/Lumiaqian/loopwall/internal/share"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// nearbyBadge counts the Macs found, once looking for them.
func (a *application) nearbyBadge() string {
	if a.share == nil || len(a.share.peers) == 0 {
		return ""
	}
	return fmt.Sprint(len(a.share.peers))
}

// nearbyView lists the Macs sharing their libraries on the network: a
// pairing to make with each new one, and what the paired ones share, to
// pull copies of. Showing it starts looking for them.
func (a *application) nearbyView(c *ui.Context) {
	a.browse()
	pageFrame(c, "附近的 Mac", "从同一网络中开启了共享的 Mac 导入视频，原画质复制到本机", nil, func(top float32) {
		scrollBody(c, top, 0, func() {
			s := a.share
			switch {
			case s == nil:
				emptyState(c, iconNearby, "局域网共享不可用", "无法读取共享设置，请重新打开 App。")
			case len(s.peers) == 0 && browseProblem(s.status) != "":
				detail := "查找附近的 Mac 失败：" + browseProblem(s.status) + "。"
				var action func()
				if s.status.Denied {
					detail = "Loopwall 没有本地网络权限，看不到也连不上其他 Mac。\n在「系统设置 → 隐私与安全性 → 本地网络」中打开 Loopwall，然后回到这里。"
					action = func() { localNetworkButton(c) }
				}
				emptyState(c, iconAlert, "无法查找附近的 Mac", detail, action)
			case len(s.peers) == 0:
				detail := "在另一台 Mac 的 Loopwall 中打开「设置 → 局域网共享」，它就会出现在这里。\n第一次查找时 macOS 会询问是否允许访问本地网络，请选择允许。"
				if a.prefs.Share && s.status.Advertised {
					detail += "\n这台 Mac 在其他 Mac 上显示为「" + s.name + "」。"
				}
				emptyState(c, iconNearby, "正在查找附近的 Mac…", detail, func() {
					t := c.Theme()
					ui.Row(c).Gap(6).AlignItems(ui.Center).TextColor(t.TextMuted).Children(func() {
						ui.Spinner(c).Label("正在搜索局域网设备")
						ui.Text(c, "持续扫描局域网中…").FontSize(12)
					})
				})
			default:
				for _, p := range s.peers {
					a.peerSection(c, p)
				}
			}
		})
	})
}

// codeFont is the font pairing codes are shown and typed in.
const codeFont = "SF Mono, Menlo, monospace"

// legacyTypedWarning is what typing six digits is told: the legacy code is
// no proof of whom it is paired with.
const legacyTypedWarning = "旧版配对方式，无法防冒充"

// warnRow is a line of warning on its own.
func warnRow(c *ui.Context, text string) {
	t := c.Theme()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(8, 14).FillWidth().TextColor(t.Warning).Children(func() {
		ui.Icon(c, iconAlert)
		ui.Text(c, text).FontSize(12).Grow(1)
	})
}

func (a *application) peerSection(c *ui.Context, p share.Peer) {
	s := a.share
	pairing, paired := s.store.Paired(p.Device)
	ui.Column(c.Key("peer-" + p.Device)).Gap(10).FillWidth().Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).FillWidth().Children(func() {
			sectionTitle(c, iconMonitor, p.Name)
			ui.Box(c).Grow(1)
			if paired {
				r := s.remotes[p.Device]
				glassButton(c, iconRepeat, "刷新", false).Disabled(r != nil && r.loading).OnClick(func() { a.listRemote(p) })
				glassButton(c, nil, "取消配对", false).OnClick(func() { a.unpairDevice(p.Device) })
			}
		})
		if sameNameDevices(s.peers)[p.Device] {
			warnRow(c, "附近有同名的 Mac，请核对配对码后再配对")
		}
		if paired && !pairing.Verified {
			warnRow(c, "旧版配对（不安全）：这台 Mac 的身份没有校验过，可能被冒充。建议取消配对，用它显示的新配对码重新配对。")
		}
		if !paired {
			group(c, func() {
				code := s.code(p.Device)
				if clean := sanitizePairingLive(*code); clean != *code {
					*code = clean
				}
				t := c.Theme()
				var fbKind pairingHintKind
				var fbText string
				ui.Column(c).Gap(10).Padding(12, 14).FillWidth().Children(func() {
					ui.Column(c).Gap(3).FillWidth().Children(func() {
						ui.Text(c, "输入配对码")
						ui.Text(c, "在「"+p.Name+"」的「设置 → 局域网共享」查看配对码，配对一次即可双向导入。").
							FontSize(11).TextColor(t.TextMuted).MaxLines(2)
					})
					ui.Row(c).Gap(8).AlignItems(ui.Center).FillWidth().Children(func() {
						in := ui.TextInput(c, code).Grow(1).MinWidth(0).Radius(999).Font(codeFont).Label("配对码").
							Placeholder("XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX").
							OnPaste(func(text string) bool {
								if text == "" {
									return false
								}
								cleaned := cleanPairingInput(text)
								if cleaned == text {
									return false
								}
								if s.pasted == nil {
									s.pasted = map[string]string{}
								}
								s.pasted[p.Device] = cleaned
								return true
							}).
							OnSubmit(func() {
								if _, _, ok := pairingInputFeedback(*code); ok {
									a.pair(p)
								}
							})
						if v := s.pasted[p.Device]; v != "" {
							start, end := in.TextSelection()
							in.ReplaceText(start, end, v)
							delete(s.pasted, p.Device)
						}
						if clean := sanitizePairingLive(*code); clean != *code {
							*code = clean
						}
						glassButton(c, iconClipboard, "粘贴", false).Disabled(s.pairing[p.Device]).OnClick(func() {
							pasted := cleanPairingInput(c.ReadClipboard())
							if pasted == "" {
								a.status = "剪贴板没有内容"
								return
							}
							*code = pasted
						})
						var canSubmit bool
						fbKind, fbText, canSubmit = pairingInputFeedback(*code)
						label := "配对"
						if s.pairing[p.Device] {
							label = "配对中…"
						}
						glassButtonKey(c, "pair-"+p.Device, iconLink, label, true).
							Disabled(s.pairing[p.Device] || !canSubmit).OnClick(func() { a.pair(p) })
					})
				})
				switch fbKind {
				case pairingHintWarning:
					ui.Divider(c)
					warnRow(c, fbText)
				case pairingHintValid:
					ui.Divider(c)
					ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(8, 14).FillWidth().TextColor(t.Success).Children(func() {
						ui.Icon(c, iconCheck)
						ui.Text(c, fbText).FontSize(12).Grow(1)
					})
				case pairingHintProgress:
					ui.Divider(c)
					ui.Text(c, fbText).FontSize(11).TextColor(t.TextMuted).Padding(8, 14)
				}
			})
			return
		}
		r := s.remotes[p.Device]
		switch {
		case r == nil || (r.err == nil && r.items == nil):
			current.onBackdrop(c, ui.Text(c, "正在读取对方的资料库…").FontSize(12), true)
		case r.err != nil:
			group(c, func() {
				groupRow(c, "无法读取对方的资料库", friendly(r.err), func() {
					if errors.Is(r.err, share.ErrNotPaired) || errors.Is(r.err, share.ErrChangedCertificate) {
						glassButton(c, iconLink, "重新配对", true).OnClick(func() { a.unpairDevice(p.Device) })
					} else {
						glassButton(c, iconRepeat, "重试", false).OnClick(func() { a.listRemote(p) })
					}
				})
			})
		case len(r.items) == 0:
			current.onBackdrop(c, ui.Text(c, "对方的资料库是空的。").FontSize(12), true)
		default:
			ui.Row(c).Wrap().Gap(18).FillWidth().AlignItems(ui.Start).Children(func() {
				for _, it := range r.items {
					a.remoteCard(c, p, it)
				}
			})
		}
	})
}

// remoteCard is a video a peer shares: its thumbnail and facts, and
// whether this Mac has it, is pulling it, or may.
func (a *application) remoteCard(c *ui.Context, p share.Peer, it share.Item) {
	t := c.Theme()
	s := a.share
	key := p.Device + "/" + it.ID
	pl := s.pulls[key]
	local, have := a.pulled(p, it.ID)
	ui.Column(c.Key(key)).Width(236).Gap(8).Children(func() {
		ui.Box(c).Size(236, 133).Radius(10).Clip().Background(t.Surface).Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.12)).Children(func() {
			if bm, _ := s.thumbs.Get(key); bm != nil {
				ui.Image(c, bm).Fill().Fit(ui.Cover)
			} else {
				ui.Box(c).Fill().Center().TextColor(t.TextMuted).Children(func() { ui.Icon(c, iconFilm).FontSize(28) })
			}
			glassPill(c, clock(it.DurationSeconds), mediaGlass, ui.RGB(255, 255, 255)).Absolute().Bottom(8).Right(8)
			if pl != nil && pl.err == "" {
				var f float64
				if pl.total > 0 {
					f = float64(pl.done) / float64(pl.total)
				}
				glassPill(c, fmt.Sprintf("%.0f%%", f*100), mediaGlass, ui.RGB(255, 255, 255)).Absolute().Bottom(8).Left(8)
				ui.Progress(c, f).Absolute().Left(0).Right(0).Bottom(0).Label("下载进度")
			}
		})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
				current.onBackdrop(c, ui.Text(c, it.Name).FontSize(13).FontWeight(600).SingleLine(), false)
				meta := fmt.Sprintf("%s · %s fps · %s", resolution(it.Width, it.Height), trimFloat(it.FrameRate), sizeText(it.Size))
				current.onBackdrop(c, ui.Text(c, meta).FontSize(11).SingleLine(), true)
			})
			switch {
			case have:
				ui.Row(c).Gap(4).AlignItems(ui.Center).TextColor(t.Accent).Tooltip("已导入为「" + local.Name + "」").Children(func() {
					ui.Icon(c, iconCheck).FontSize(13)
					ui.Text(c, "已导入").FontSize(12).FontWeight(600)
				})
			case pl != nil && pl.err == "":
				iconButton(c, iconX, "取消下载").OnClick(pl.cancel)
			default:
				label := "导入"
				if pl != nil {
					label = "重试"
				}
				glassButton(c, iconImport, label, false).OnClick(func() { a.pullVideo(p, it) })
			}
		})
		if pl != nil && pl.err != "" {
			ui.Text(c, pl.err).FontSize(11).TextColor(t.Warning).MaxLines(2)
		}
	})
}

// shareSettings turns sharing this library on and off, shows the code to
// pair with it, and the devices paired.
func (a *application) shareSettings(c *ui.Context) {
	s := a.share
	if s == nil {
		return
	}
	t := c.Theme()
	ui.Column(c).Gap(10).FillWidth().Children(func() {
		sectionTitle(c, iconNearby, "局域网共享")
		group(c, func() {
			groupRow(c, "共享我的资料库", "已配对的 Mac 可以在「附近的 Mac」中浏览并导入你的视频；只读，关闭后立即停止。", func() {
				if glassSwitch(c, &a.prefs.Share, "共享我的资料库", false) {
					a.setSharing(a.prefs.Share)
				}
			})
			if a.shareSaveError != "" {
				groupRow(c, "共享设置未保存", a.shareSaveError, func() { glassButton(c, nil, "重试保存", false).OnClick(a.saveShareSettings) })
			}
			if s.err != "" {
				ui.Divider(c)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(10, 14).FillWidth().TextColor(t.Warning).Children(func() {
					ui.Icon(c, iconAlert)
					ui.Text(c, "无法共享："+s.err).FontSize(12).Grow(1)
				})
			}
			if a.prefs.Share && s.err == "" {
				ui.Divider(c)
				note, problem := publishNote(s.status, s.name)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(10, 14).FillWidth().Children(func() {
					dot := ui.Box(c).Size(7, 7).Radius(4).Shrink(0).Background(t.Success)
					text := ui.Text(c, note).FontSize(12).Grow(1)
					switch {
					case problem:
						dot.Background(t.Warning)
						text.TextColor(t.Warning)
					case !s.status.Advertised:
						dot.Background(t.TextMuted)
						text.TextColor(t.TextMuted)
					}
					if s.status.Denied {
						localNetworkButton(c)
					}
				})
				ui.Divider(c)
				code := s.server.PairingCode()
				copied := code != "" && s.copiedCode == code && c.Now().Before(s.copiedUntil)
				if copied {
					c.After(s.copiedUntil.Sub(c.Now()))
				}
				groupRow(c, "配对码", "在另一台 Mac 的「附近的 Mac」中输入；配对后两台都能导入对方的视频。里面含有这台 Mac 的身份，所以别人冒充这台 Mac 时对方会拒绝配对。用过一次或连续输错 5 次后会换新的。", func() {
					if code != "" {
						copyLabel, copyIcon := "复制", iconCopy
						if copied {
							copyLabel, copyIcon = "已复制", iconCheck
						}
						glassButtonKey(c, "copy-pairing-code", copyIcon, copyLabel, false).OnClick(func() {
							a.copyPairingCode(c, code)
						})
					}
					glassButton(c, nil, "换一个", false).OnClick(func() {
						s.copiedCode = ""
						s.server.NewCode()
					})
				})
				if code == "" {
					ui.Text(c, "已失效，请换一个").FontWeight(600).TextColor(t.Warning).Padding(0, 14, 12)
				} else {
					first, second := codeLines(code)
					ui.Column(c).Gap(4).Padding(0, 14, 12).Children(func() {
						ui.Text(c, first).Font(codeFont).FontSize(20).Bold().TextColor(t.Accent).Selectable()
						ui.Text(c, second).Font(codeFont).FontSize(20).Bold().TextColor(t.Accent).Selectable()
					})
				}
				ui.Divider(c)
				expanded := s.showLegacyPairing || a.prefs.ShareLegacy
				chevron := iconRight
				if expanded {
					chevron = iconDown
				}
				legacyState := ""
				if a.prefs.ShareLegacy {
					legacyState = "已开启（不安全）"
				}
				toggle := ui.ButtonBase(c.Key("legacy-disclosure")).FillWidth().Padding(10, 14).Label("旧版兼容选项")
				switch {
				case toggle.Pressed():
					toggle.Background(t.SurfacePressed)
				case toggle.Hovered():
					toggle.Background(t.SurfaceHover)
				}
				if !a.prefs.ShareLegacy && toggle.Clicked() {
					s.showLegacyPairing = !s.showLegacyPairing
				}
				toggle.Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).FillWidth().Children(func() {
						ui.Icon(c, chevron).FontSize(14).TextColor(t.TextMuted)
						ui.Text(c, "旧版兼容选项").FontSize(12).TextColor(t.TextMuted).Grow(1)
						if legacyState != "" {
							ui.Text(c, legacyState).FontSize(11).FontWeight(600).TextColor(t.Warning)
						}
					})
				})
				if expanded {
					ui.Divider(c)
					groupRow(c, "允许旧版 Loopwall 配对（不安全）", "0.1.x 的 Loopwall 只会输入 6 位数字，无法核对对方的身份。只在要和旧版配对时临时打开，配对后关掉。", func() {
						if glassSwitch(c, &a.prefs.ShareLegacy, "允许旧版 Loopwall 配对", false) {
							a.setLegacyPairing(a.prefs.ShareLegacy)
						}
					})
					if a.prefs.ShareLegacy {
						ui.Divider(c)
						warnRow(c, "6 位数字无法识别冒充者：同一网络里的恶意设备可以冒充这台 Mac，骗走输入的数字并拿到资料库的读取权限。")
						if legacy := s.server.Code(); legacy != "" {
							ui.Text(c, "旧版配对码  "+legacy[:3]+" "+legacy[3:]).Font(codeFont).FontSize(16).Bold().TextColor(t.Warning).Padding(0, 14, 12)
						}
					}
				}
			}
			granted := s.store.Granted()
			for _, d := range s.grantedDevices() {
				g := granted[d]
				ui.Divider(c)
				detail := "可以互相导入视频"
				if _, both := s.store.Paired(d); !both {
					detail = "可以导入你的视频；对方是旧版本，升级后重新配对即可双向互通"
				}
				if g.ClientFingerprint == "" {
					detail += " · 旧版配对（不安全），建议取消后用新配对码重新配对"
				}
				groupRow(c, g.Name, detail+" · "+time.Unix(g.PairedAt, 0).Format("2006-01-02 配对"), func() {
					glassButton(c, nil, "取消配对", false).OnClick(func() { a.unpairDevice(d) })
				})
			}
		})
	})
}

// localNetworkButton opens the Local Network list of System Settings, where
// Loopwall gets the permission Bonjour needs.
func localNetworkButton(c *ui.Context) {
	ui.Row(c).Center().Children(func() {
		glassButton(c, iconExternal, "打开本地网络设置", true).OnClick(func() {
			_ = mygo.Shell.OpenExternal(localNetworkSettings)
		})
	})
}
