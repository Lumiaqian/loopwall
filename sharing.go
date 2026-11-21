package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/Lumiaqian/loopwall/internal/platform"
	"github.com/Lumiaqian/loopwall/internal/share"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// sharing is the app's side of sharing libraries on the local network
// (internal/share): this Mac's server, while prefs.Share is on, and the
// Macs it finds and pulls copies of videos from. Its fields change on the
// main thread only.
type sharing struct {
	store  *share.Store
	server *share.Server
	client *share.Client
	name   string // this Mac's name on the network
	err    string // why sharing could not start, if it could not

	// browsing is read by the ticker, off the main thread.
	browsing atomic.Bool
	peers    []share.Peer             // the Macs found, this one left out
	seen     map[string]time.Time     // when each was last found, by device
	remotes  map[string]*remote       // what each peer shares, by its device
	pulls    map[string]*pull         // downloads, by "device/id"
	codes    map[string]*string       // pairing codes being typed, by device
	pairing  map[string]bool          // pairings under way, by device
	thumbs   *lru[string, *ui.Bitmap] // peers' thumbnails, by "device/id"
	// status is how publishing and browsing fare, as the ticker last read it.
	status platform.SharingStatus
}

// localNetworkSettings opens Privacy & Security's Local Network list,
// where Loopwall is allowed to publish and browse.
const localNetworkSettings = "x-apple.systempreferences:com.apple.preference.security?Privacy_LocalNetwork"

// publishNote says in a line how publishing this Mac fares, and whether
// that is a problem: Bonjour fails silently but for what it reports.
func publishNote(st platform.SharingStatus, name string) (string, bool) {
	switch {
	case st.AdvertiseError != "":
		return "其他 Mac 暂时看不到这台：" + st.AdvertiseError + "，会自动重试", true
	case st.Advertised:
		return "其他 Mac 的「附近的 Mac」里会出现「" + name + "」", false
	default:
		return "正在发布到局域网…", false
	}
}

// browseProblem is why looking for Macs fails, empty while it does not.
func browseProblem(st platform.SharingStatus) string {
	if st.Browsing == "waiting" || st.Browsing == "failed" {
		if st.BrowseError == "" {
			return "等待网络"
		}
		return st.BrowseError
	}
	return ""
}

// remote is what a peer shares, as last listed.
type remote struct {
	items   []share.Item
	err     error
	loading bool
}

// pull is a video being downloaded from a peer.
type pull struct {
	done, total int64
	err         string
	cancel      context.CancelFunc
}

// setupSharing opens sharing's state beside the settings and starts
// sharing if it was on. Without it, the app works as before.
func (a *application) setupSharing() {
	path, err := prefsPath()
	var st *share.Store
	if err == nil {
		st, err = share.OpenStore(filepath.Dir(path))
	}
	if err != nil {
		a.status = "局域网共享不可用：" + err.Error()
		return
	}
	name, _ := a.host.ComputerName()
	if strings.TrimSpace(name) == "" {
		name = "Mac"
	}
	a.share = &sharing{store: st, server: share.NewServer(a.root, st), client: share.NewClient(st, name), name: name,
		remotes: map[string]*remote{}, pulls: map[string]*pull{}, codes: map[string]*string{}, seen: map[string]time.Time{}, pairing: map[string]bool{},
		thumbs: newLRU[string, *ui.Bitmap](thumbCacheSize)}
	if a.prefs.Share {
		a.startSharing()
	}
}

// startSharing serves the library and advertises it.
func (a *application) startSharing() {
	s := a.share
	if s == nil || s.server.Port() != 0 {
		return
	}
	s.err = ""
	port, err := s.server.Start(":0")
	if err == nil {
		if err = a.host.Advertise(s.name, int64(port), s.store.Device()); err != nil {
			_ = s.server.Stop()
		}
	}
	if err != nil {
		s.err = err.Error()
	}
}

func (a *application) stopSharing() {
	s := a.share
	if s == nil {
		return
	}
	_ = a.host.StopAdvertising()
	_ = s.server.Stop()
	s.err = ""
}

// setSharing turns sharing this library on or off, and remembers it.
func (a *application) setSharing(on bool) {
	a.prefs.Share = on
	a.savePrefs()
	if on {
		a.share.server.NewCode()
		a.startSharing()
	} else {
		a.stopSharing()
	}
}

// findPeers is the ticker's: while browsing, the Macs found, without this
// one. Off the main thread: it reads only what is safe there.
func (a *application) findPeers() ([]share.Peer, bool) {
	if a.share == nil || !a.share.browsing.Load() {
		return nil, false
	}
	self := a.share.store.Device()
	found, err := a.host.Peers()
	if err != nil {
		return nil, false
	}
	var peers []share.Peer
	for _, p := range found {
		if p.Device != "" && p.Device != self {
			peers = append(peers, share.Peer{Name: p.Name, Device: p.Device, Addr: share.Addr(p.Host, p.Port)})
		}
	}
	return peers, true
}

// peerGrace is how long a Mac that Bonjour stopped reporting stays in the
// list: a resolve that fails once, or a lost packet, would make it flicker
// away and back.
const peerGrace = 30 * time.Second

// mergePeers is the list to show: the Macs found now, and those found
// within peerGrace before, as last seen, by name. seen keeps when each was
// last found, by device.
func mergePeers(shown, found []share.Peer, seen map[string]time.Time, now time.Time) []share.Peer {
	byDevice := map[string]share.Peer{}
	for _, p := range shown {
		if now.Sub(seen[p.Device]) < peerGrace {
			byDevice[p.Device] = p
		}
	}
	for _, p := range found {
		seen[p.Device] = now
		byDevice[p.Device] = p
	}
	for d, at := range seen {
		if now.Sub(at) >= peerGrace {
			delete(seen, d)
		}
	}
	peers := make([]share.Peer, 0, len(byDevice))
	for _, p := range byDevice {
		peers = append(peers, p)
	}
	slices.SortFunc(peers, func(x, y share.Peer) int { return strings.Compare(x.Name, y.Name) })
	return peers
}

// setPeers takes the Macs found, listing what the paired ones share the
// first time they show up.
func (a *application) setPeers(found []share.Peer) {
	s := a.share
	s.peers = mergePeers(s.peers, found, s.seen, time.Now())
	for _, p := range s.peers {
		if _, paired := s.store.Paired(p.Device); paired && s.remotes[p.Device] == nil {
			a.listRemote(p)
		}
	}
}

// browse starts looking for Macs, which asks for the local network the
// first time.
func (a *application) browse() {
	s := a.share
	if s == nil || s.browsing.Load() {
		return
	}
	s.browsing.Store(true)
	go func() {
		peers, ok := a.findPeers()
		mygo.RunOnMain(func() {
			if ok {
				a.setPeers(peers)
			}
			a.invalidate()
		})
	}()
}

// listRemote lists what peer shares, then fetches the thumbnails missing.
func (a *application) listRemote(p share.Peer) {
	s := a.share
	r := s.remotes[p.Device]
	if r == nil {
		r = &remote{}
		s.remotes[p.Device] = r
	}
	if r.loading {
		return
	}
	r.loading = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		items, err := s.client.Library(ctx, p)
		mygo.RunOnMain(func() {
			r.items, r.err, r.loading = items, err, false
			a.fetchThumbnails(p, items)
			a.invalidate()
		})
	}()
}

// thumbnailTimeout bounds each thumbnail's fetch, apart from the list's.
const thumbnailTimeout = 15 * time.Second

// missingThumbnails is the items of p whose thumbnails are not cached.
func (s *sharing) missingThumbnails(p share.Peer, items []share.Item) []share.Item {
	var missing []share.Item
	for _, it := range items {
		if !s.thumbs.Has(p.Device + "/" + it.ID) {
			missing = append(missing, it)
		}
	}
	return missing
}

// fetchThumbnails fetches the thumbnails of items that are not cached, one
// after another in the background, each within its own timeout. On the
// main thread.
func (a *application) fetchThumbnails(p share.Peer, items []share.Item) {
	s := a.share
	missing := s.missingThumbnails(p, items)
	if len(missing) == 0 {
		return
	}
	go func() {
		for _, it := range missing {
			ctx, cancel := context.WithTimeout(context.Background(), thumbnailTimeout)
			b, err := s.client.Thumbnail(ctx, p, it.ID)
			cancel()
			if err != nil {
				continue
			}
			bm, err := ui.DecodeBitmap(b)
			if err != nil {
				continue
			}
			key := p.Device + "/" + it.ID
			mygo.RunOnMain(func() {
				s.thumbs.Put(key, bm)
				a.invalidate()
			})
		}
	}()
}

// pair pairs with p by the code typed for it.
func (a *application) pair(p share.Peer) {
	s := a.share
	code := strings.ReplaceAll(strings.TrimSpace(*s.code(p.Device)), " ", "")
	if code == "" || s.pairing[p.Device] {
		return
	}
	s.pairing[p.Device] = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := s.client.Pair(ctx, p, code)
		mygo.RunOnMain(func() {
			s.pairing[p.Device] = false
			if err != nil {
				a.status = "配对失败：" + err.Error()
			} else {
				*s.code(p.Device) = ""
				a.status = "已与「" + p.Name + "」配对，双方都可以导入对方的视频"
				delete(s.remotes, p.Device)
				a.listRemote(p)
			}
			a.invalidate()
		})
	}()
}

// code is the pairing code being typed for device, which a text input
// edits in place.
func (s *sharing) code(device string) *string {
	if s.codes[device] == nil {
		s.codes[device] = new(string)
	}
	return s.codes[device]
}

// unpairDevice forgets device both ways, telling it when it is found.
func (a *application) unpairDevice(device string) {
	s := a.share
	addr := ""
	for _, p := range s.peers {
		if p.Device == device {
			addr = p.Addr
		}
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := s.client.Unpair(ctx, device, addr)
		mygo.RunOnMain(func() {
			delete(s.remotes, device)
			if err != nil {
				a.status = "已在本机取消配对；未能通知对方：" + friendly(err)
			}
			a.invalidate()
		})
	}()
}

// pulled is the local copy of p's video id, if one was pulled and is
// still in the library.
func (a *application) pulled(p share.Peer, id string) (catalog.MediaEntry, bool) {
	local, ok := a.share.store.Imported(p.Device, id)
	if !ok {
		return catalog.MediaEntry{}, false
	}
	return a.entry(local)
}

// pullVideo downloads p's video it into the library, as an import does a
// file, and adds it to the end of the playlist.
func (a *application) pullVideo(p share.Peer, it share.Item) {
	s := a.share
	key := p.Device + "/" + it.ID
	if pl := s.pulls[key]; pl != nil && pl.err == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	pl := &pull{total: it.Size, cancel: cancel}
	s.pulls[key] = pl
	go func() {
		defer cancel()
		var shown time.Time
		m, err := catalog.ImportFrom(ctx, a.root, it.Name, it.Ext, func(w io.Writer) error {
			return s.client.Download(ctx, p, it.ID, it.Size, w, func(done, total int64) {
				if time.Since(shown) < 150*time.Millisecond && done != total {
					return
				}
				shown = time.Now()
				mygo.RunOnMain(func() {
					pl.done, pl.total = done, total
					a.invalidate()
				})
			})
		}, a.probe)
		// Read before the deferred cancel: a failure is not a cancellation.
		outcome := pullOutcome(err, ctx.Err())
		mygo.RunOnMain(func() {
			defer a.invalidate()
			switch outcome {
			case pullCancelled:
				delete(s.pulls, key)
				return
			case pullFailed:
				pl.err = friendly(err)
				return
			}
			delete(s.pulls, key)
			if !a.finish(a.addImported([]catalog.MediaEntry{m})) {
				return
			}
			if err := s.store.RecordImport(p.Device, it.ID, m.ID); err != nil {
				a.status = err.Error()
				return
			}
			a.status = "已导入「" + m.Name + "」，并加入轮播"
		})
	}()
}

// pullOutcome is how a pull ended: err is the import's, ctxErr the pull's
// context's, read before it is cancelled for good. A failure that is not
// the user's cancelling stays listed, with its reason; a cancel leaves
// nothing; a pull that finished before the cancel arrived is done.
func pullOutcome(err, ctxErr error) pullState {
	switch {
	case err == nil:
		return pullDone
	case errors.Is(ctxErr, context.Canceled):
		return pullCancelled
	}
	return pullFailed
}

type pullState int

const (
	pullDone pullState = iota
	pullCancelled
	pullFailed
)

// friendly words an error of sharing for the user: the network's own,
// as a refused or timed out connection, say what likely happened.
func friendly(err error) string {
	var netErr net.Error
	switch {
	case errors.Is(err, share.ErrNotPaired), errors.Is(err, share.ErrWrongCode), errors.Is(err, share.ErrChangedCertificate),
		errors.Is(err, share.ErrChangedFile), errors.Is(err, share.ErrStalled):
		return err.Error()
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return "连接超时：对方可能已休眠或离开了这个网络"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "连接被拒绝：对方可能已关闭共享"
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, syscall.ECONNRESET):
		return "连接中断了，请重试"
	case errors.As(err, &netErr):
		return "连不上对方：" + err.Error()
	}
	return err.Error()
}

func (a *application) invalidate() {
	if a.window != nil {
		a.window.Invalidate()
	}
}

// sizeText is a file's size, as Finder writes it.
func sizeText(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.0f MB", float64(n)/1e6)
	default:
		return fmt.Sprintf("%.0f KB", float64(n)/1e3)
	}
}

// grantedDevices is who may pull from this Mac, by name, oldest first.
func (s *sharing) grantedDevices() []string {
	granted := s.store.Granted()
	devices := make([]string, 0, len(granted))
	for d := range granted {
		devices = append(devices, d)
	}
	slices.SortFunc(devices, func(x, y string) int { return int(granted[x].PairedAt - granted[y].PairedAt) })
	return devices
}
