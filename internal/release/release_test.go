package release

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// extractArchive and verify are MyGO v0.9.0's update.ExtractArchive and
// update.Verify (internal/update/update.go, unchanged in v0.10.0), copied:
// what installed apps run on an archive, which this package must write as
// `mygo build` does.
func extractArchive(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(hdr.Name)
		if !fs.ValidPath(name) || name == "." {
			return fmt.Errorf("invalid path in update archive: %q", hdr.Name)
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		mode := fs.FileMode(hdr.Mode).Perm()
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode|0o200)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tr)
			if err == nil {
				err = f.Chmod(mode)
			}
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			resolved := path.Join(path.Dir(name), hdr.Linkname)
			if path.IsAbs(hdr.Linkname) || !fs.ValidPath(resolved) {
				return fmt.Errorf("update archive link %q points outside the app", hdr.Name)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported entry in update archive: %q", hdr.Name)
		}
	}
}

func verify(key ed25519.PublicKey, digest []byte, signature string) error {
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || !ed25519.Verify(key, digest, sig) {
		return fmt.Errorf("the update is not signed with the app's key")
	}
	return nil
}

// bundle makes an app bundle as the build ships it: an executable, the
// extension nested in it, resources, and a framework's symbolic links.
func bundle(t *testing.T) string {
	t.Helper()
	app := filepath.Join(t.TempDir(), "Loopwall.app")
	files := map[string]os.FileMode{
		"Contents/Info.plist":     0o644,
		"Contents/MacOS/Loopwall": 0o755,
		"Contents/Extensions/LoopwallExtension.appex/Contents/MacOS/LoopwallExtension": 0o755,
		"Contents/Extensions/LoopwallExtension.appex/Contents/Info.plist":              0o644,
		"Contents/Frameworks/Platform.framework/Versions/A/Platform":                   0o755,
		"Contents/Resources/icon.png":                                                  0o644,
	}
	for p, mode := range files {
		full := filepath.Join(app, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("contents of "+p), mode); err != nil {
			t.Fatal(err)
		}
	}
	fw := filepath.Join(app, "Contents/Frameworks/Platform.framework")
	if err := os.Symlink("A", filepath.Join(fw, "Versions/Current")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("Versions/Current/Platform", filepath.Join(fw, "Platform")); err != nil {
		t.Fatal(err)
	}
	return app
}

// tree lists a directory's entries with their kind, mode and contents.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		info, _ := d.Info()
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			link, _ := os.Readlink(p)
			out[rel] = "link " + link
		case d.IsDir():
			out[rel] = "dir"
		default:
			b, _ := os.ReadFile(p)
			out[rel] = fmt.Sprintf("%v %s", info.Mode().Perm(), b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPackageInstallsAsMyGOsUpdater(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	app, out := bundle(t), t.TempDir()
	archive, manifest, err := Package(Options{App: app, Out: out, Slug: "loopwall", Version: "0.1.2", Target: "darwin-arm64",
		Notes: "- 自动更新", Download: "https://github.com/Lumiaqian/loopwall/releases/download/v0.1.2/", Key: key})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(archive) != "loopwall-0.1.2-darwin-arm64.tar.gz" || filepath.Base(manifest) != "update-darwin-arm64.json" {
		t.Errorf("wrote %s and %s", archive, manifest)
	}
	var m Manifest
	b, _ := os.ReadFile(manifest)
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(archive)
	digest := sha256.Sum256(data)
	if m.Version != "0.1.2" || m.Notes != "- 自动更新" || m.Size != int64(len(data)) ||
		m.URL != "https://github.com/Lumiaqian/loopwall/releases/download/v0.1.2/loopwall-0.1.2-darwin-arm64.tar.gz" {
		t.Errorf("manifest %+v", m)
	}
	if err := verify(pub, digest[:], m.Signature); err != nil {
		t.Fatal(err)
	}
	// What the updater unpacks: the one bundle, as built.
	dir := t.TempDir()
	if err := extractArchive(strings.NewReader(string(data)), dir); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "Loopwall.app" {
		t.Fatalf("unpacked %v, not the one bundle", entries)
	}
	want, got := tree(t, app), tree(t, filepath.Join(dir, "Loopwall.app"))
	for p, w := range want {
		if got[p] != w {
			t.Errorf("%s: unpacked %q, want %q", p, got[p], w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unpacked %d entries, want %d", len(got), len(want))
	}
	// Another key's signature is refused.
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if verify(other, digest[:], m.Signature) == nil {
		t.Error("another key verified the signature")
	}
}

func TestPackageRefuses(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	if _, _, err := Package(Options{App: t.TempDir(), Out: t.TempDir(), Key: key}); err == nil {
		t.Error("packaged what is not a bundle")
	}
	if _, _, err := Package(Options{App: bundle(t), Out: t.TempDir()}); err == nil {
		t.Error("packaged without a key")
	}
}

func TestKeys(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	parsed, err := ParsePrivateKey(base64.StdEncoding.EncodeToString(key) + "\n")
	if err != nil || !parsed.Equal(key) {
		t.Fatalf("parsed %v, %v", parsed, err)
	}
	if _, err := ParsePrivateKey("not a key"); err == nil {
		t.Error("parsed garbage")
	}
	if err := CheckKey(key, base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Error(err)
	}
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if CheckKey(key, base64.StdEncoding.EncodeToString(otherPub)) == nil {
		t.Error("a key matched another's public key")
	}
}

func TestReleaseNotes(t *testing.T) {
	changelog := "# Changelog\n\n## 0.1.2\n\n- 自动更新\n- 局域网\n\n## [0.1.1] - 2026-11-15\n\n- 预览\n"
	if got, err := ReleaseNotes(changelog, "0.1.2"); err != nil || got != "- 自动更新\n- 局域网" {
		t.Errorf("0.1.2: %q, %v", got, err)
	}
	if got, err := ReleaseNotes(changelog, "0.1.1"); err != nil || got != "- 预览" {
		t.Errorf("0.1.1: %q, %v", got, err)
	}
	if _, err := ReleaseNotes(changelog, "0.2.0"); err == nil {
		t.Error("found notes of a version the changelog lacks")
	}
}
