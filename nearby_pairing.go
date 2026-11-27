package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Lumiaqian/loopwall/internal/platform"
	"github.com/Lumiaqian/loopwall/internal/share"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Only sent as a byte payload after both Nearby verification handlers accept.
type nearbyIdentity struct {
	Device      string
	Fingerprint string
	Ticket      string
}

func (a *application) configureNearby() {
	s := a.share
	advertise, discover := s.server.Port() != 0, s.browsing.Load()
	if s.nearby == nil {
		if !advertise && !discover {
			return
		}
		var err error
		s.nearby, err = platform.NewNearby()
		if err != nil {
			a.status = err.Error()
			return
		}
	}
	if err := s.nearby.Configure(s.store.Device(), advertise, discover); err != nil {
		a.status = err.Error()
	}
}

func (a *application) verifyNearbyView(c *ui.Context) {
	s := a.share
	p := s.prompt
	group(c, func() {
		title := "请在两台 Mac 上核对确认码"
		if p.Kind == "approved" {
			title = "已确认，等待另一台 Mac"
		}
		ui.Text(c, title).Bold()
		ui.Text(c, p.Value).Font(codeFont).FontSize(26)
		for _, choice := range []struct {
			label  string
			accept bool
		}{{"不同或取消", false}, {"两边一致", true}} {
			glassButton(c, nil, choice.label, choice.accept).Disabled(choice.accept && p.Kind == "approved").OnClick(func() {
				if err := s.nearby.Decide(p.Attempt, choice.accept); err != nil {
					a.status = err.Error()
					return
				}
				if choice.accept {
					s.prompt.Kind = "approved"
				} else {
					s.prompt.Kind = "cancelled"
				}
			})
		}
	})
}

func (a *application) pollNearby() {
	s := a.share
	if s.nearby == nil {
		return
	}
	for {
		ev, err := s.nearby.Next()
		if err != nil || ev.Kind == "" {
			return
		}
		if err := a.handleNearby(ev); err != nil {
			_ = s.nearby.Decide(ev.Attempt, false)
			a.status = err.Error()
		}
	}
}

func (a *application) handleNearby(ev platform.NearbyEvent) error {
	s := a.share
	if ev.Kind == "ended" {
		if s.prompt.Attempt == ev.Attempt || s.prompt.Attempt == "" {
			s.server.CancelNearby(s.nearbyTicket)
			s.nearbyTicket = ""
			s.prompt = platform.NearbyEvent{}
			s.selected = share.Peer{}
		}
		return nil
	}
	if ev.Kind == "verify" {
		if (!ev.Incoming && ev.Device != s.selected.Device) || (ev.Incoming && s.server.Port() == 0) {
			return errors.New("配对对象已变更")
		}
		s.prompt = ev
		a.section = sectionNearby
		a.open()
		return nil
	}
	if ev.Attempt != s.prompt.Attempt || ev.Device != s.prompt.Device {
		return errors.New("配对已过期")
	}
	fp, err := s.store.CertFingerprint()
	if err != nil {
		return err
	}
	local := nearbyIdentity{Device: s.store.Device(), Fingerprint: fp}
	switch ev.Kind {
	case "connected":
		if s.prompt.Kind != "approved" {
			return errors.New("尚未确认短码")
		}
		s.prompt.Kind = "connected"
		if ev.Incoming {
			return nil
		}
	case "payload":
		if s.prompt.Kind != "connected" {
			return errors.New("意外的配对消息")
		}
		var remote nearbyIdentity
		if json.Unmarshal([]byte(ev.Value), &remote) != nil || remote.Device != ev.Device {
			return errors.New("对端身份与所选设备不符")
		}
		s.prompt.Kind = "redeeming"
		if ev.Incoming {
			if remote.Ticket != "" {
				return errors.New("意外的配对邀请")
			}
			local.Ticket, err = s.server.InviteNearby(remote.Device, remote.Fingerprint)
			s.nearbyTicket = local.Ticket
			if err != nil {
				return err
			}
		} else {
			a.redeemNearby(ev.Attempt, s.selected, remote)
			return nil
		}
	default:
		return errors.New("未知配对事件")
	}
	data, err := json.Marshal(local)
	if err != nil {
		return err
	}
	return s.nearby.Send(ev.Attempt, string(data))
}

func (a *application) redeemNearby(attempt string, peer share.Peer, remote nearbyIdentity) {
	s := a.share
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := s.client.PairNearby(ctx, peer, remote.Fingerprint, remote.Ticket)
		mygo.RunOnMain(func() {
			_ = s.nearby.Decide(attempt, false)
			if err != nil {
				a.status = "配对失败：" + friendly(err)
			} else {
				a.status = "短码配对成功"
				a.listRemote(peer)
			}
			a.invalidate()
		})
	}()
}
