//go:build darwin && nearbyintegration

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/Lumiaqian/loopwall/internal/platform"
	"github.com/Lumiaqian/loopwall/internal/share"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestMain(m *testing.M) {
	result := make(chan int, 1)
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.WhenReady(func() {
		go func() {
			result <- m.Run()
			mygo.RunOnMain(mygo.App.Quit)
		}()
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(<-result)
}

// Run explicitly on a Mac with the native module built and local network access.
// Each process owns its own Nearby runtime, credentials and fixture-only HTTPS server.
func TestNearbyTwoProcesses(t *testing.T) {
	role := os.Getenv("LOOPWALL_TEST_PEER")
	if role == "" {
		dir := t.TempDir()
		child := exec.Command(os.Args[0], "-test.run=^TestNearbyTwoProcesses$", "-test.v")
		child.Env = append(os.Environ(), "LOOPWALL_TEST_PEER=B", "LOOPWALL_TEST_CONTROL="+dir)
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
		runNearbyPeer(t, "A", "B", dir)
		return
	}
	runNearbyPeer(t, "B", "A", os.Getenv("LOOPWALL_TEST_CONTROL"))
}

func runNearbyPeer(t *testing.T, role, other, dir string) {
	store, err := share.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	library := catalog.Default()
	library.Media = []catalog.MediaEntry{{ID: "11111111-1111-4111-8111-111111111111", Name: "fixture-" + role, Filename: "video.mp4", Width: 16, Height: 16, FrameRate: 1, DurationSeconds: 1}}
	if err := catalog.Save(root, &library); err != nil {
		t.Fatal(err)
	}
	media := catalog.VideoPath(root, library.Media[0])
	if err := os.MkdirAll(filepath.Dir(media), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(media, []byte(role), 0600); err != nil {
		t.Fatal(err)
	}
	srv := share.NewServer(root, store)
	port, err := srv.Start(":0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	n, err := platform.NewNearby()
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Configure(store.Device(), true, true); err != nil {
		t.Fatal(err)
	}
	defer n.Configure(store.Device(), false, false)
	write := func(key string, value []byte) {
		path := filepath.Join(dir, role+key)
		if err := os.WriteFile(path+".tmp", value, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".tmp", path); err != nil {
			t.Fatal(err)
		}
	}
	host, err := platform.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Advertise("Loopwall Test "+role+" "+store.Device()[:8], int64(port), store.Device()); err != nil {
		t.Fatal(err)
	}
	defer host.StopAdvertising()
	write("peer", []byte(store.Device()))
	s := &sharing{nearby: n, server: srv, store: store, client: share.NewClient(store, role), name: role, codes: map[string]*string{}, pairing: map[string]bool{}, remotes: map[string]*remote{}}
	a := &application{share: s}
	s.seen = map[string]time.Time{}
	s.thumbs = newLRU[string, *ui.Bitmap](thumbCacheSize)
	s.browsing.Store(true)
	var peer share.Peer
	tt := ui.NewTester(a.nearbyView, 900, 1100)
	done := false
	decided := false
	reject := os.Getenv("LOOPWALL_TEST_DECISION") == "mismatch"
	var last string
	for deadline := time.Now().Add(45 * time.Second); time.Now().Before(deadline); {
		time.Sleep(150 * time.Millisecond)
		if peer.Device == "" {
			want, _ := os.ReadFile(filepath.Join(dir, other+"peer"))
			found, err := host.Peers()
			if err != nil {
				t.Fatal(err)
			}
			for _, candidate := range found {
				if len(want) > 0 && candidate.Device == string(want) && strings.HasPrefix(candidate.Name, "Loopwall Test ") {
					peer = share.Peer{Device: candidate.Device, Name: candidate.Name, Addr: share.Addr(candidate.Host, candidate.Port)}
					break
				}
			}
			if peer.Device == "" {
				continue
			}
		}
		var stepErr error
		mygo.RunOnMain(func() {
			a.setPeers([]share.Peer{peer})
			a.pollNearby()
			tt.Frame()
			if role == "B" && s.selected.Device == "" && s.prompt.Attempt == "" && !decided {
				if _, paired := store.Paired(peer.Device); !paired {
					_ = tt.Click("核对短码配对")
				}
			}
			if s.prompt.Kind == "verify" && !decided {
				write("code", []byte(s.prompt.Value))
				b, err := os.ReadFile(filepath.Join(dir, other+"code"))
				if err == nil {
					choice := "两边一致"
					if string(b) != s.prompt.Value {
						stepErr = fmt.Errorf("verification codes differ")
						choice = "不同或取消"
					}
					if reject && role == "A" {
						if _, err := os.Stat(filepath.Join(dir, other+"decided")); err != nil {
							return
						}
						choice = "不同或取消"
					}
					if err := tt.Click(choice); err != nil {
						stepErr = err
					}
					decided = true
					write("decided", []byte("ok"))
				}
			}
			if reject {
				_, paired := store.Paired(peer.Device)
				if paired || len(store.Granted()) != 0 {
					stepErr = fmt.Errorf("rejected session authorized a device")
				}
				done = decided && s.prompt.Attempt == ""
			} else {
				_, done = tt.Find("fixture-" + other)
			}
			last = fmt.Sprintf("phase=%s selected=%t %s", s.prompt.Kind, s.selected.Device != "", a.status)
		})
		if stepErr != nil {
			t.Fatal(stepErr)
		}
		if done {
			write("done", []byte("ok"))
			if _, err := os.Stat(filepath.Join(dir, other+"done")); err == nil {
				return
			}
		}
	}
	t.Fatalf("peer %s timed out: %s", role, last)
}
