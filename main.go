package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/Lumiaqian/loopwall/internal/platform"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	updatenative "github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

type application struct {
	host          platform.Host
	root          string
	config        catalog.Config
	window        *mygo.Window
	section       string
	grid          ui.GridState
	selected      string
	rename        string
	interval      string
	status        string
	busy          bool
	login         bool
	thumbs        map[string]*ui.Bitmap
	runtime       catalog.Runtime
	displays      []platform.DisplayInfo
	preview       *ui.VideoPlayer
	previewWindow *mygo.Window
	prefs         prefs
	share         *sharing // nil when sharing's state could not open
	// Wallpaper recovery (recovery.go).
	stuckSignals int64     // the extension's calls for help seen so far
	stallSince   time.Time // when the displays began waiting, or the extension went silent
	repairedAt   time.Time // when the app last restarted WallpaperAgent
}

func main() {
	mygo.App.SetName("Loopwall")
	// Updates, asked before they install (update.go).
	mygo.Use(updatenative.Plugin)
	updatesUsed = true
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	app := &application{root: catalog.Root(), section: sectionLibrary, thumbs: map[string]*ui.Bitmap{}}
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnActivate(func(visible bool) {
		if !visible && !(len(os.Args) > 2 && os.Args[1] == "--import") {
			app.open()
		}
	})
	mygo.App.OnSecondInstance(func(_ []string, _ string) { app.open() })
	mygo.App.OnOpenURL(func(_ string) { app.open() })
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{{Role: mygo.RoleAppMenu}, {Role: mygo.RoleEditMenu}, {Role: mygo.RoleWindowMenu}}))
	mygo.App.WhenReady(func() {
		var err error
		app.host, err = platform.NewHost()
		if err != nil {
			mygo.Dialog.Error("无法加载原生模块", err.Error())
			mygo.App.Quit()
			return
		}
		// Once, the library and settings the app kept as Phosphene Go.
		var migrated []string
		if moved, err := catalog.Migrate(catalog.LegacyRoot(), app.root); err != nil {
			migrated = append(migrated, "资料库迁移失败："+err.Error())
		} else if moved {
			migrated = append(migrated, "已从 Phosphene Go 迁移资料库")
		}
		if err := migrateUserData(); err != nil {
			migrated = append(migrated, "设置迁移失败："+err.Error())
		}
		app.config, err = catalog.Load(app.root)
		if err != nil {
			mygo.Dialog.Error("资料库读取失败", err.Error())
			mygo.App.Quit()
			return
		}
		app.interval = strconv.Itoa(app.config.Playlist.IntervalSeconds)
		app.prefs = loadPrefs()
		mygo.Theme.SetSource(app.prefs.themeSource())
		app.login = mygo.App.OpenAtLogin()
		if len(os.Args) > 2 && os.Args[1] == "--import" {
			go func() {
				videos, _ := videoFiles(os.Args[2:], app.root)
				added, errs := app.importFiles(videos)
				app.addMedia(added...)
				imported := len(added)
				if err := catalog.Save(app.root, &app.config); err != nil {
					errs = append(errs, err.Error())
				} else if err := app.host.NotifyLibraryChanged(); err != nil {
					errs = append(errs, err.Error())
				}
				_ = catalog.AtomicJSON(filepath.Join(app.root, "import-result.json"), map[string]any{"imported": imported, "errors": errs})
				mygo.App.Quit()
			}()
			return
		}
		app.loadThumbnails()
		app.reapLater()
		if len(migrated) > 0 {
			app.status = strings.Join(migrated, "；")
			if err := app.host.NotifyLibraryChanged(); err != nil {
				log.Println(err)
			}
		}
		app.setupSharing()
		// Install updates only when asked, then relaunch at once, so the
		// wallpaper's extension never waits on a bundle replaced unseen.
		updater.SetAutomaticDownloads(false)
		version := mygo.App.Version()
		app.checkExtension(app.launchedVersion(version), version)
		updater.OnInstalled(guardRelaunch)
		tray, err := mygo.NewTray(mygo.TrayOptions{Icon: trayIcon(), IconIsTemplate: true, ToolTip: "Loopwall", Menu: mygo.NewMenu([]*mygo.MenuItem{
			{Label: "打开资料库", Click: func(*mygo.MenuItem, *mygo.Window) { app.open() }},
			{Label: "系统墙纸设置", Click: func(*mygo.MenuItem, *mygo.Window) { openWallpaperSettings() }},
			updater.MenuItem(),
			{Label: "修复壁纸（重建墙纸服务）", Click: func(*mygo.MenuItem, *mygo.Window) { app.repairWallpaper("正在重建墙纸服务…", time.Now()) }},
			mygo.Separator(), {Label: "退出管理端（壁纸继续播放）", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
		})})
		if err != nil {
			log.Println(err)
		}
		_ = tray
		if !mygo.App.WasOpenedAtLogin() {
			app.open()
		}
		go func() {
			for range time.NewTicker(2 * time.Second).C {
				r, _ := catalog.ReadRuntime(app.root)
				displays, err := app.host.Displays()
				stuck, _ := app.host.AgentStuckSignals()
				peers, browsing := app.findPeers()
				var sharingStatus platform.SharingStatus
				if app.share != nil {
					sharingStatus, _ = app.host.SharingStatus()
				}
				mygo.RunOnMain(func() {
					if browsing {
						app.setPeers(peers)
					}
					if app.share != nil {
						app.share.status = sharingStatus
					}
					app.runtime = r
					app.observeWallpaper(r, stuck, time.Now())
					if err == nil {
						app.displays = displays
					}
					if app.window != nil {
						app.window.Invalidate()
					}
				})
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
func (a *application) open() {
	if a.host == nil {
		return
	}
	if a.window != nil {
		a.window.Show()
		a.window.Focus()
		return
	}
	a.window = mygo.NewWindow(mygo.WindowOptions{
		Title: "Loopwall", Width: 1100, Height: 760, MinWidth: 760, MinHeight: 520,
		TitleBarStyle: mygo.TitleBarHiddenInset, TitleBarHeight: titleBarHeight,
		Content: ui.View(a.view),
	})
	a.window.OnClosed(func() { a.window = nil })
}
func openWallpaperSettings() {
	_ = mygo.Shell.OpenExternal("x-apple.systempreferences:com.apple.Wallpaper-Settings.extension")
}
func (a *application) save() bool {
	if err := catalog.Save(a.root, &a.config); err != nil {
		a.status = "保存失败：" + err.Error()
		return false
	}
	if err := a.host.NotifyLibraryChanged(); err != nil {
		a.status = "已保存，通知扩展失败：" + err.Error()
		return false
	}
	a.status = "已保存，壁纸会自动更新"
	return true
}
func (a *application) loadThumbnails() {
	for _, m := range a.config.Media {
		if a.thumbs[m.ID] == nil {
			if b, e := os.ReadFile(catalog.ThumbnailPath(a.root, m)); e == nil {
				a.thumbs[m.ID], _ = ui.DecodeBitmap(b)
			}
		}
	}
}

// probe reads a video's size, rate and length and writes its thumbnail,
// for catalog.Import and catalog.ImportFrom.
func (a *application) probe(ctx context.Context, path, thumb string) (catalog.MediaEntry, error) {
	info, e := a.host.Probe(ctx, path, thumb)
	return catalog.MediaEntry{Width: info.Width, Height: info.Height, DurationSeconds: info.Duration, FrameRate: info.FrameRate}, e
}

// importFiles imports paths into the library's files, off the main thread:
// it reads only the host and the root, and returns the entries for
// addMedia to add.
func (a *application) importFiles(paths []string) ([]catalog.MediaEntry, []string) {
	var added []catalog.MediaEntry
	errs := []string{}
	for _, path := range paths {
		m, e := catalog.Import(context.Background(), a.root, path, a.probe)
		if e != nil {
			errs = append(errs, filepath.Base(path)+"："+e.Error())
			continue
		}
		added = append(added, m)
	}
	return added, errs
}

// addMedia adds imported videos to the library and the end of the
// playlist, unsaved.
func (a *application) addMedia(entries ...catalog.MediaEntry) {
	for _, m := range entries {
		a.config.Media = append(a.config.Media, m)
		a.config.Playlist.VideoIDs = append(a.config.Playlist.VideoIDs, m.ID)
	}
}
func (a *application) chooseImport() {
	if a.busy {
		return
	}
	a.busy = true
	a.status = "请选择视频…"
	parent := a.window
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: parent, Title: "导入视频壁纸", Multiple: true, Filters: []mygo.FileFilter{{Name: "视频", Extensions: []string{"mp4", "mov", "m4v"}}}})
		mygo.RunOnMain(func() {
			a.busy = false
			switch {
			case err != nil:
				a.status = err.Error()
			case len(paths) == 0:
				a.status = "已取消导入"
			default:
				a.importPaths(paths)
			}
			a.invalidate()
		})
	}()
}

// importPaths imports the videos among paths, chosen or dropped, and in
// the folders among them, in the background. On the main thread.
func (a *application) importPaths(paths []string) {
	if a.busy {
		a.status = "正在处理上一批，请稍后再导入"
		return
	}
	videos, skipped := videoFiles(paths, a.root)
	if len(videos) == 0 {
		a.status = "没有可导入的视频（支持 MP4、MOV、M4V）"
		return
	}
	a.busy = true
	a.section = sectionLibrary
	a.status = fmt.Sprintf("正在复制并验证 %d 个视频…", len(videos))
	go func() {
		// The library changes only on the main thread, by what was added:
		// what changed it meanwhile, as a download, stays.
		added, errs := a.importFiles(videos)
		mygo.RunOnMain(func() {
			a.addMedia(added...)
			a.busy = false
			saved := a.save()
			a.loadThumbnails()
			if saved {
				a.status = fmt.Sprintf("已导入 %d 个视频", len(added))
				if skipped > 0 {
					a.status += fmt.Sprintf("，跳过 %d 个非视频文件", skipped)
				}
				if len(errs) > 0 {
					a.status += "；" + strings.Join(errs, "；")
				}
			}
			a.invalidate()
		})
	}()
}

// videoFiles expands paths into the videos to import: those of a video's
// extension, and those in the folders, at any depth, hidden ones left
// out, each once, in order. It counts the other files as skipped. What is
// in the library is left out, so dropping it on itself imports nothing.
func videoFiles(paths []string, library string) (videos []string, skipped int) {
	seen := map[string]bool{}
	inLibrary := func(p string) bool {
		return library != "" && (p == library || strings.HasPrefix(p, library+string(filepath.Separator)))
	}
	add := func(p string) {
		switch {
		case inLibrary(p) || seen[p]:
		case slices.Contains([]string{".mp4", ".mov", ".m4v"}, strings.ToLower(filepath.Ext(p))):
			seen[p] = true
			videos = append(videos, p)
		default:
			skipped++
		}
	}
	for _, root := range paths {
		root = filepath.Clean(root)
		info, err := os.Stat(root)
		if err != nil {
			skipped++
			continue
		}
		if !info.IsDir() {
			add(root)
			continue
		}
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return nil // unreadable: leave it out
			case p != root && strings.HasPrefix(d.Name(), "."):
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			case d.IsDir():
				if inLibrary(p) {
					return filepath.SkipDir
				}
				return nil
			}
			add(p)
			return nil
		})
	}
	return videos, skipped
}

func (a *application) entry(id string) (catalog.MediaEntry, bool) {
	for _, m := range a.config.Media {
		if m.ID == id {
			return m, true
		}
	}
	return catalog.MediaEntry{}, false
}
func (a *application) deleteMedia(m catalog.MediaEntry) {
	if a.busy {
		return
	}
	a.busy = true
	parent := a.window
	go func() {
		r, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: parent, Type: mygo.MessageWarning, Message: "从资料库删除「" + m.Name + "」？", Detail: "原始文件会保留。正在播放的屏幕会切换到下一项。", Buttons: []string{"取消", "删除"}, CancelButton: 0, DefaultButton: 0, DestructiveButtons: []int{1}})
		mygo.RunOnMain(func() {
			a.busy = false
			if err == nil && r.Button == 1 {
				a.config.Playlist.VideoIDs = slices.DeleteFunc(a.config.Playlist.VideoIDs, func(id string) bool { return id == m.ID })
				maps.DeleteFunc(a.config.Playlist.Pins, func(_, id string) bool { return id == m.ID })
				a.config.Media = slices.DeleteFunc(a.config.Media, func(e catalog.MediaEntry) bool { return e.ID == m.ID })
				a.config.RemovedMediaIDs = append(a.config.RemovedMediaIDs, m.ID)
				delete(a.thumbs, m.ID)
				saved := a.save()
				if a.previewWindow != nil {
					a.previewWindow.Close()
				}
				if saved {
					a.status = "已删除；播放切换完成后会清理资料库副本"
					a.reapLater()
				}
			}
			if a.window != nil {
				a.window.Invalidate()
			}
		})
	}()
}
func (a *application) reapLater() {
	go func() {
		for i := 0; i < 3; i++ {
			time.Sleep(6 * time.Second)
			_ = catalog.Reap(a.root)
		}
	}()
}
func trayIcon() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 36, 36))
	ink := color.RGBA{0, 0, 0, 255}
	for y := 6; y < 29; y++ {
		for x := 3; x < 33; x++ {
			if x < 6 || x > 29 || y < 9 || y > 25 {
				img.Set(x, y, ink)
			}
		}
	}
	for y := 12; y < 23; y++ {
		for x := 14; x < 14+(y-12)/2+1 && x < 22; x++ {
			img.Set(x, y, ink)
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
