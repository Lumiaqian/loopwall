package main

import (
	"bytes"
	"context"
	"fmt"
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
	_ "github.com/egoist/mygo/locales/zh-Hans"
	"github.com/egoist/mygo/plugins/updater"
	updatenative "github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

type focusTarget uint8

const (
	focusNone focusTarget = iota
	focusRename
	focusSearch
)

type application struct {
	libraryReady   bool
	host           platform.Host
	root           string
	config         catalog.Config
	window         *mygo.Window
	section        string
	grid           ui.GridState
	selected       string
	rename         string
	renameHandle   ui.Handle
	focus          focusTarget
	libraryQuery   string
	libraryFilter  int
	interval       string
	status         string
	choosingImport bool
	importing      bool
	importDone     int
	importTotal    int
	importCurrent  string
	deletingMedia  bool
	login          bool
	thumbs         *lru[string, *ui.Bitmap] // decoded library thumbnails, by media ID; see thumb
	fileSizes      map[string]int64         // cached byte sizes of local videos, by media ID
	tray           *mygo.Tray
	traySig        string
	runtime        catalog.Runtime
	displays       []platform.DisplayInfo
	preview        *ui.VideoPlayer
	previewWindow  *mygo.Window
	prefs          prefs
	shareSaveError string
	share          *sharing // nil when sharing's state could not open
	// Wallpaper recovery (recovery.go).
	stuckSignals int64     // the extension's calls for help seen so far
	stallSince   time.Time // when the displays began waiting, or the extension went silent
	repairing    bool
	repairedAt   time.Time // when the app last restarted WallpaperAgent
}

func configureAppLocale() {
	mygo.App.SetName("Loopwall")
	mygo.App.SetLanguages("zh-Hans")
}

func buildAppMenu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleWindowMenu},
	})
}

func main() {
	configureAppLocale()
	// Updates, asked before they install (update.go).
	mygo.Use(updatenative.Plugin)
	updatesUsed = true
	if !mygo.App.RequestSingleInstanceLock() {
		return // the running instance has the arguments: see secondInstance
	}
	app := &application{root: catalog.Root(), section: sectionLibrary}
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnActivate(func(visible bool) {
		if _, importing := importArgs(os.Args[1:], ""); app.libraryReady && !visible && !importing {
			app.open()
		}
	})
	mygo.App.OnSecondInstance(app.secondInstance)
	mygo.App.OnOpenURL(func(_ string) {
		if app.libraryReady {
			app.open()
		}
	})
	mygo.App.WhenReady(func() {
		// Install after startLocale so MyGo role menus (including the Edit -> Speech submenu)
		// pick up zh-Hans instead of falling back to English.
		mygo.App.SetMenu(buildAppMenu())
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
		config, err := catalog.Load(app.root)
		if os.IsPermission(err) {
			go app.requestLibraryAccess(migrated)
			return
		}
		app.finishLibraryLoad(config, err, migrated)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func (app *application) requestLibraryAccess(migrated []string) {
	expected := app.root
	entry, err := createLibraryShortcut(expected)
	if err != nil {
		mygo.RunOnMain(func() { app.finishLibraryLoad(catalog.Config{}, err, migrated) })
		return
	}
	mygo.App.OnQuit(func() { os.Remove(entry); os.Remove(filepath.Dir(entry)) })
	message := "单击「原壁纸资料库」文件夹，再点「使用原资料库」。"
	for {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title: "打开原壁纸资料库", Message: message, DefaultPath: filepath.Dir(entry),
			Directory: true, ButtonLabel: "使用原资料库",
		})
		if err != nil {
			mygo.RunOnMain(func() { app.finishLibraryLoad(catalog.Config{}, err, migrated) })
			return
		}
		if len(paths) == 0 {
			mygo.RunOnMain(func() { mygo.App.Quit() })
			return
		}
		selected, err := selectedLibraryDirectory(expected, paths)
		if err != nil {
			result, dialogErr := mygo.Dialog.Message(mygo.MessageOptions{
				Type: mygo.MessageWarning, Message: "尚未选中原资料库",
				Detail:  err.Error() + "\n可在 Finder 显示「原壁纸资料库」，将它拖入随后打开的选择窗口，再确认。",
				Buttons: []string{"在 Finder 显示入口", "取消"}, CancelButton: 1,
			})
			if dialogErr != nil || result.Button != 0 {
				mygo.RunOnMain(func() { mygo.App.Quit() })
				return
			}
			mygo.Shell.ShowItemInFolder(entry)
			message = "从 Finder 将「原壁纸资料库」拖到此窗口，然后点「使用原资料库」。"
			continue
		}
		config, err := catalog.LoadExisting(selected)
		mygo.RunOnMain(func() {
			if err == nil {
				app.root = selected
			}
			app.finishLibraryLoad(config, err, migrated)
		})
		return
	}
}

// The panel can open this directory before the user grants access to the
// extension container. Creating the link does not read or change the target.
func createLibraryShortcut(root string) (string, error) {
	dir, err := os.MkdirTemp("", "Loopwall-")
	if err != nil {
		return "", err
	}
	entry := filepath.Join(dir, "原壁纸资料库")
	if err := os.Symlink(root, entry); err != nil {
		os.Remove(dir)
		return "", err
	}
	return entry, nil
}

func selectedLibraryDirectory(expected string, paths []string) (string, error) {
	if len(paths) != 1 {
		return "", fmt.Errorf("需要选择一个目录，当前选择：%q", paths)
	}
	selected := paths[0]
	info, err := os.Stat(selected)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("所选路径不是目录：%s", selected)
	}
	if selected != expected {
		original, err := os.Stat(expected)
		if err != nil {
			return "", fmt.Errorf("已选择 %s，无法核实原目录：%w", selected, err)
		}
		if !os.SameFile(info, original) {
			return "", fmt.Errorf("已选择其他目录：%s", selected)
		}
	}
	return selected, nil
}

func (app *application) finishLibraryLoad(config catalog.Config, err error, migrated []string) {
	if err != nil {
		mygo.Dialog.Error("资料库读取失败", err.Error())
		mygo.App.Quit()
		return
	}
	app.config = config
	app.libraryReady = true
	app.interval = strconv.Itoa(app.config.Playlist.IntervalSeconds)
	app.prefs = loadPrefs()
	mygo.Theme.SetSource(app.prefs.themeSource())
	app.login = mygo.App.OpenAtLogin()
	if paths, importing := importArgs(os.Args[1:], ""); importing {
		app.importPaths(paths, func(imported int, errs []string) {
			app.writeImportResult(imported, errs)
			mygo.App.Quit()
		})
		return
	}
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
	app.checkExtension(version)
	updater.OnInstalled(guardRelaunch)
	updater.OnChange(func() {
		mygo.RunOnMain(func() {
			app.invalidate()
			app.syncTrayMenu()
		})
	})
	menu, sig := app.buildTrayMenu()
	tray, err := mygo.NewTray(mygo.TrayOptions{Icon: trayIcon(), IconIsTemplate: true, ToolTip: "Loopwall", Menu: menu})
	if err != nil {
		log.Println(err)
	} else {
		app.tray = tray
		app.traySig = sig
	}
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
				if err == nil {
					app.displays = displays
				}
				app.observeWallpaper(r, stuck, time.Now())
				app.syncTrayMenu()
				if app.window != nil {
					app.window.Invalidate()
				}
			})
		}
	}()
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

// saveCatalog writes the library's configuration; tests replace it.
var saveCatalog = catalog.Save

// cloneConfig copies c deeply enough that editing the copy leaves c alone.
func cloneConfig(c catalog.Config) catalog.Config {
	c.RemovedMediaIDs = slices.Clone(c.RemovedMediaIDs)
	c.Media = slices.Clone(c.Media)
	c.Playlist.VideoIDs = slices.Clone(c.Playlist.VideoIDs)
	c.Playlist.Pins = maps.Clone(c.Playlist.Pins)
	return c
}

// update applies edit to a copy of the configuration, saves the copy and
// only then makes it the app's: a save that fails leaves memory as the disk
// still is.
func (a *application) update(edit func(*catalog.Config)) error {
	next := cloneConfig(a.config)
	edit(&next)
	if err := saveCatalog(a.root, &next); err != nil {
		return err
	}
	a.config = next
	return nil
}

// commit is update, then telling the extension, and saying how it went.
func (a *application) commit(edit func(*catalog.Config)) bool {
	return a.finish(a.update(edit))
}

// commitQuiet saves and notifies the extension like commit, but only sets
// a.status when something fails so repeated drag/step reorders stay silent.
func (a *application) commitQuiet(edit func(*catalog.Config)) bool {
	if err := a.update(edit); err != nil {
		a.status = "保存失败：" + err.Error()
		return false
	}
	if err := a.host.NotifyLibraryChanged(); err != nil {
		a.status = "已保存，通知扩展失败：" + err.Error()
		return false
	}
	a.syncTrayMenu()
	return true
}

// finish says how a save, whose result is err, went, and tells the
// extension of a saved one.
func (a *application) finish(err error) bool {
	if err != nil {
		a.status = "保存失败：" + err.Error()
		return false
	}
	if err := a.host.NotifyLibraryChanged(); err != nil {
		a.status = "已保存，通知扩展失败：" + err.Error()
		return false
	}
	a.status = "已保存，壁纸会自动更新"
	a.syncTrayMenu()
	return true
}

// thumbCacheSize bounds the decoded thumbnails kept in memory, a library's
// and the peers' each.
const thumbCacheSize = 256

// thumb is the thumbnail of library video id, nil when it has none: read
// and decoded on first use, and kept while it is among the most recently
// used. A miss is kept too, so a frame does not read the disk again. On the
// main thread.
func (a *application) thumb(id string) *ui.Bitmap {
	if a.thumbs == nil {
		a.thumbs = newLRU[string, *ui.Bitmap](thumbCacheSize)
	}
	if bm, ok := a.thumbs.Get(id); ok {
		return bm
	}
	var bm *ui.Bitmap
	if b, e := os.ReadFile(catalog.ThumbnailPath(a.root, catalog.MediaEntry{ID: id})); e == nil {
		bm, _ = ui.DecodeBitmap(b)
	}
	a.thumbs.Put(id, bm)
	return bm
}

// mediaSize returns m's on-disk byte size via os.Stat and caches it so
// library cards show file sizes without changing catalog.json.
func (a *application) mediaSize(m catalog.MediaEntry) int64 {
	if a.fileSizes != nil {
		if sz, ok := a.fileSizes[m.ID]; ok {
			return sz
		}
	}
	info, err := os.Stat(catalog.VideoPath(a.root, m))
	if err != nil {
		return 0
	}
	if a.fileSizes == nil {
		a.fileSizes = map[string]int64{}
	}
	sz := info.Size()
	a.fileSizes[m.ID] = sz
	return sz
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
func (a *application) importFiles(paths []string, onProgress func(done, total int, name string)) ([]catalog.MediaEntry, []string) {
	var added []catalog.MediaEntry
	errs := []string{}
	total := len(paths)
	for i, path := range paths {
		name := filepath.Base(path)
		if onProgress != nil {
			onProgress(i, total, name)
		}
		m, e := catalog.Import(context.Background(), a.root, path, a.probe)
		if e != nil {
			errs = append(errs, name+"："+e.Error())
		} else {
			added = append(added, m)
		}
		if onProgress != nil {
			onProgress(i+1, total, name)
		}
	}
	return added, errs
}

// addMedia adds imported videos to c's library and the end of its
// playlist.
func addMedia(c *catalog.Config, entries ...catalog.MediaEntry) {
	for _, m := range entries {
		c.Media = append(c.Media, m)
		c.Playlist.VideoIDs = append(c.Playlist.VideoIDs, m.ID)
	}
}

// addImported saves the library with entries added. When that fails, the
// copies already published in videos/ are removed, so nothing is orphaned.
func (a *application) addImported(entries []catalog.MediaEntry) error {
	err := a.update(func(c *catalog.Config) { addMedia(c, entries...) })
	if err != nil {
		for _, m := range entries {
			_ = os.RemoveAll(filepath.Join(a.root, "videos", m.ID))
		}
	}
	return err
}

var openImportDialog = func(parent *mygo.Window) ([]string, error) {
	return mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:   parent,
		Title:    "导入视频壁纸",
		Multiple: true,
		Filters:  []mygo.FileFilter{{Name: "视频", Extensions: []string{"mp4", "mov", "m4v"}}},
	})
}

func (a *application) chooseImport() {
	if a.choosingImport || a.importing {
		return
	}
	a.choosingImport = true
	parent := a.window
	go func() {
		paths, err := openImportDialog(parent)
		mygo.RunOnMain(func() {
			a.finishChooseImport(paths, err)
			a.invalidate()
		})
	}()
}

func (a *application) finishChooseImport(paths []string, err error) {
	a.choosingImport = false
	switch {
	case err != nil:
		a.status = err.Error()
	case len(paths) > 0:
		a.importPaths(paths, nil)
	}
}

var importOnMain = mygo.RunOnMain

// importPaths imports the videos among paths, chosen or dropped, and in
// the folders among them, in the background. done, if not nil, is told how
// many were imported and what went wrong, once, on the main thread. On the
// main thread.
func (a *application) importPaths(paths []string, done func(imported int, errs []string)) {
	finish := func(imported int, errs []string) {
		if done != nil {
			done(imported, errs)
		}
	}
	if a.importing {
		a.status = "正在处理上一批，请稍后再导入"
		finish(0, []string{a.status})
		return
	}
	a.importing = true
	a.section = sectionLibrary
	a.invalidate()
	paths = slices.Clone(paths)
	go func() {
		videos, skipped := videoFiles(paths, a.root)
		if len(videos) == 0 {
			importOnMain(func() {
				a.importing = false
				a.status = "没有可导入的视频（支持 MP4、MOV、M4V）"
				a.invalidate()
				finish(0, []string{a.status})
			})
			return
		}
		// The library changes only on the main thread, by what was added:
		// what changed it meanwhile, as a download, stays.
		added, errs := a.importFiles(videos, func(doneCount, total int, name string) {
			importOnMain(func() {
				a.importDone = doneCount
				a.importTotal = total
				a.importCurrent = name
				a.invalidate()
			})
		})
		importOnMain(func() {
			err := a.addImported(added)
			saved := a.finish(err)
			a.importing = false
			a.importDone = 0
			a.importTotal = 0
			a.importCurrent = ""
			imported := len(added)
			switch {
			case err != nil:
				imported = 0
				errs = append(errs, err.Error())
			case !saved: // saved, but the extension was not told
				errs = append(errs, a.status)
			}
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
			finish(imported, errs)
		})
	}()
}

// importArgs reads the paths of "--import <paths…>" from args, a command
// line without the program's name, as the first instance is handed a
// second's. Relative paths are resolved against workingDir, when it is
// not empty. It reports false for any other command line.
func importArgs(args []string, workingDir string) ([]string, bool) {
	if len(args) < 2 || args[0] != "--import" {
		return nil, false
	}
	paths := make([]string, 0, len(args)-1)
	for _, p := range args[1:] {
		if workingDir != "" && !filepath.IsAbs(p) {
			p = filepath.Join(workingDir, p)
		}
		paths = append(paths, p)
	}
	return paths, true
}

// writeImportResult tells whoever ran --import how it went.
func (a *application) writeImportResult(imported int, errs []string) {
	if errs == nil {
		errs = []string{}
	}
	if err := catalog.AtomicJSON(filepath.Join(a.root, "import-result.json"), map[string]any{"imported": imported, "errors": errs}); err != nil {
		log.Println(err)
	}
}

// secondInstance is what the running app does when another is started,
// which handed over its command line: "--import" imports the paths, as the
// command line does when the app is not running; anything else opens the
// window. On the main thread.
func (a *application) secondInstance(args []string, workingDir string) {
	paths, importing := importArgs(args, workingDir)
	if !importing {
		if !a.libraryReady {
			return
		}
		a.open()
		return
	}
	if !a.libraryReady {
		a.writeImportResult(0, []string{"管理端尚未就绪，请稍后重试"})
		return
	}
	a.importPaths(paths, a.writeImportResult)
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
	if a.deletingMedia {
		return
	}
	a.deletingMedia = true
	parent := a.window
	go func() {
		r, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: parent, Type: mygo.MessageWarning, Message: "从资料库删除「" + m.Name + "」？", Detail: "原始文件会保留。正在播放的屏幕会切换到下一项。", Buttons: []string{"取消", "删除"}, CancelButton: 0, DefaultButton: 0, DestructiveButtons: []int{1}})
		mygo.RunOnMain(func() {
			a.deletingMedia = false
			if err == nil && r.Button == 1 {
				saved := a.commit(func(c *catalog.Config) {
					c.Playlist.VideoIDs = slices.DeleteFunc(c.Playlist.VideoIDs, func(id string) bool { return id == m.ID })
					maps.DeleteFunc(c.Playlist.Pins, func(_, id string) bool { return id == m.ID })
					c.Media = slices.DeleteFunc(c.Media, func(e catalog.MediaEntry) bool { return e.ID == m.ID })
					c.RemovedMediaIDs = append(c.RemovedMediaIDs, m.ID)
				})
				if _, ok := a.entry(m.ID); !ok {
					if a.thumbs != nil {
						a.thumbs.Remove(m.ID)
					}
					if a.fileSizes != nil {
						delete(a.fileSizes, m.ID)
					}
					if a.selected == m.ID {
						a.cancelRename()
					}
				}
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

var trayIconSVG = icon(`<path d="M7 14H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2v3"/><rect x="8" y="9" width="14" height="12" rx="2"/><path d="M13.2 12.2v5.6l4.6-2.8z"/>`)

func trayIcon() []byte {
	img := trayIconSVG.Image(36, 36, color.Black)
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func (a *application) setPowerSaving(on bool) {
	if a.config.Playlist.PowerSaving == on {
		return
	}
	a.commit(func(c *catalog.Config) { c.Playlist.PowerSaving = on })
	a.invalidate()
}

func (a *application) buildTrayMenu() (*mygo.Menu, string) {
	var items []*mygo.MenuItem
	var sig strings.Builder
	sig.WriteString(strconv.FormatBool(a.config.Playlist.PowerSaving))
	sig.WriteByte('|')
	sig.WriteString(strconv.FormatBool(updatesUsed))
	if len(a.displays) == 0 {
		items = append(items, &mygo.MenuItem{Label: "未检测到显示器", Disabled: true})
		sig.WriteString("|none")
	} else {
		for _, d := range a.displays {
			sub, badge := a.displayState(d.Identifier).trayStatus()
			sig.WriteByte('|')
			sig.WriteString(d.Identifier)
			sig.WriteByte('=')
			sig.WriteString(d.Name)
			sig.WriteByte(':')
			sig.WriteString(sub)
			sig.WriteByte(':')
			sig.WriteString(badge)
			items = append(items, &mygo.MenuItem{
				Label:    d.Name,
				Subtitle: sub,
				Badge:    badge,
				Click: func(*mygo.MenuItem, *mygo.Window) {
					a.section = sectionPlay
					a.open()
				},
			})
		}
	}
	items = append(items,
		mygo.Separator(),
		&mygo.MenuItem{Label: "打开资料库", Click: func(*mygo.MenuItem, *mygo.Window) {
			a.section = sectionLibrary
			a.open()
		}},
		&mygo.MenuItem{Label: "导入视频…", Click: func(*mygo.MenuItem, *mygo.Window) {
			a.open()
			a.chooseImport()
		}},
		&mygo.MenuItem{
			Label:    "省电模式",
			Type:     mygo.MenuItemCheckbox,
			Checked:  a.config.Playlist.PowerSaving,
			KeepOpen: true,
			Click: func(it *mygo.MenuItem, _ *mygo.Window) {
				a.setPowerSaving(it.IsChecked())
			},
		},
		&mygo.MenuItem{Label: "系统墙纸设置", Click: func(*mygo.MenuItem, *mygo.Window) { openWallpaperSettings() }},
		mygo.Separator(),
	)
	if updatesUsed {
		items = append(items, updater.MenuItem())
	}
	items = append(items,
		&mygo.MenuItem{Label: "修复壁纸（重建墙纸服务）", Click: func(*mygo.MenuItem, *mygo.Window) { a.startWallpaperRepair() }},
		mygo.Separator(),
		&mygo.MenuItem{Label: "退出管理端（壁纸继续播放）", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	)
	return mygo.NewMenu(items), sig.String()
}

func (a *application) syncTrayMenu() {
	if a.tray == nil {
		return
	}
	menu, sig := a.buildTrayMenu()
	if sig == a.traySig {
		return
	}
	a.traySig = sig
	a.tray.SetMenu(menu)
}
