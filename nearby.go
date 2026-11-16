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
				if s.status.Denied {
					detail = "Loopwall 没有本地网络权限，看不到也连不上其他 Mac。\n在「系统设置 → 隐私与安全性 → 本地网络」中打开 Loopwall，然后回到这里。"
				}
				emptyState(c, iconAlert, "无法查找附近的 Mac", detail)
				if s.status.Denied {
					localNetworkButton(c)
				}
			case len(s.peers) == 0:
				detail := "在另一台 Mac 的 Loopwall 中打开「设置 → 局域网共享」，它就会出现在这里。\n第一次查找时 macOS 会询问是否允许访问本地网络，请选择允许。"
				if a.prefs.Share && s.status.Advertised {
					detail += "\n这台 Mac 在其他 Mac 上显示为「" + s.name + "」。"
				}
				emptyState(c, iconNearby, "正在查找附近的 Mac…", detail)
			default:
				for _, p := range s.peers {
					a.peerSection(c, p)
				}
			}
		})
	})
}

func (a *application) peerSection(c *ui.Context, p share.Peer) {
	s := a.share
	_, paired := s.store.Paired(p.Device)
	ui.Column(c.Key("peer-" + p.Device)).Gap(10).FillWidth().Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).FillWidth().Children(func() {
			sectionTitle(c, iconMonitor, p.Name)
			ui.Box(c).Grow(1)
			if paired {
				r := s.remotes[p.Device]
				glassButton(c, iconRepeat, "刷新", false).Disabled(r != nil && r.loading).OnClick(func() { a.listRemote(p) })
				glassButton(c, nil, "取消配对", false).OnClick(func() { a.unpair(p) })
			}
		})
		if !paired {
			group(c, func() {
				groupRow(c, "输入配对码", "在「"+p.Name+"」上打开「设置 → 局域网共享」，输入那里显示的 6 位数字。", func() {
					code := s.code(p.Device)
					ui.TextInput(c, code).Width(120).Radius(999).Label("配对码").Placeholder("000 000").OnSubmit(func() { a.pair(p) })
					label := "配对"
					if s.pairing[p.Device] {
						label = "配对中…"
					}
					glassButton(c, iconLink, label, true).Disabled(s.pairing[p.Device] || *code == "").OnClick(func() { a.pair(p) })
				})
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
						glassButton(c, iconLink, "重新配对", true).OnClick(func() { a.unpair(p) })
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
			if bm := s.thumbs[key]; bm != nil {
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
				code := s.server.Code()
				groupRow(c, "配对码", "在另一台 Mac 的「附近的 Mac」中输入；用过一次或连续输错 5 次后会换新的。", func() {
					if code == "" {
						ui.Text(c, "已失效").FontWeight(600).TextColor(t.Warning)
					} else {
						ui.Text(c, code[:3]+" "+code[3:]).FontSize(22).Bold().TextColor(t.Accent)
					}
					glassButton(c, nil, "换一个", false).OnClick(s.server.NewCode)
				})
			}
			granted := s.store.Granted()
			for _, d := range s.grantedDevices() {
				g := granted[d]
				ui.Divider(c)
				groupRow(c, g.Name, "可以导入你的视频 · "+time.Unix(g.PairedAt, 0).Format("2006-01-02 配对"), func() {
					glassButton(c, nil, "取消配对", false).OnClick(func() {
						if err := s.store.Revoke(d); err != nil {
							a.status = err.Error()
						}
					})
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
