// Package release packages Loopwall's signed updates.
//
// The app updates itself with MyGO's updater, which reads update-<target>.json
// from the latest GitHub release and installs the archive it names once its
// Ed25519 signature checks out. `mygo build` writes those files only for the
// bundle it builds, before build-local.sh puts the wallpaper extension in it,
// so they would update the app without its extension. This package writes
// them for the final bundle instead, in the format of MyGO v0.9.0
// and v0.10.0, which did not change it (internal/update): a gzip-compressed tar of the bundle, and a manifest
// naming it with its size and the signature of its SHA-256 digest.
package release

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Manifest is update-<target>.json, as MyGO's updater reads it.
type Manifest struct {
	Version   string `json:"version"`
	Notes     string `json:"notes,omitempty"`
	Date      string `json:"date,omitempty"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	Signature string `json:"signature"`
}

// Options say what to package and where it is published.
type Options struct {
	App     string // the .app bundle, as shipped
	Out     string // the directory the archive and manifest go to
	Slug    string // names the archive, as mygo.json's slug
	Version string
	Target  string // as darwin-arm64
	Notes   string // the release notes, Markdown
	// Download is the URL of the release's files, the archive's name added.
	Download string
	Key      ed25519.PrivateKey
}

// Package writes the archive of the bundle and its manifest into Out, and
// returns their paths.
func Package(o Options) (archive, manifest string, err error) {
	if !strings.HasSuffix(o.App, ".app") {
		return "", "", errors.New("release: not an app bundle: " + o.App)
	}
	if len(o.Key) != ed25519.PrivateKeySize {
		return "", "", errors.New("release: no signing key")
	}
	name := o.Slug + "-" + o.Version + "-" + o.Target + ".tar.gz"
	archive = filepath.Join(o.Out, name)
	f, err := os.Create(archive)
	if err != nil {
		return "", "", err
	}
	sum := sha256.New()
	err = WriteArchive(io.MultiWriter(f, sum), filepath.Dir(o.App), filepath.Base(o.App))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(archive)
	if err != nil {
		return "", "", err
	}
	m := Manifest{
		Version:   o.Version,
		Notes:     o.Notes,
		Date:      time.Now().UTC().Format(time.RFC3339),
		URL:       strings.TrimSuffix(o.Download, "/") + "/" + name,
		Size:      info.Size(),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(o.Key, sum.Sum(nil))),
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	manifest = filepath.Join(o.Out, "update-"+o.Target+".json")
	if err := os.WriteFile(manifest, append(data, '\n'), 0o644); err != nil {
		return "", "", err
	}
	return archive, manifest, nil
}

// WriteArchive writes entry, under dir, as MyGO's update.WriteArchive
// does: files, directories and symbolic links with their permissions,
// named relative to dir, owned by no one, gzip-compressed at best.
func WriteArchive(w io.Writer, dir, entry string) error {
	gz, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(gz)
	err = filepath.WalkDir(filepath.Join(dir, entry), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		link := ""
		if info.Mode()&fs.ModeSymlink != 0 {
			if link, err = os.Readlink(p); err != nil {
				return err
			}
		}
		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if d.IsDir() {
			hdr.Name += "/"
		}
		hdr.Uid, hdr.Gid, hdr.Uname, hdr.Gname = 0, 0, "", ""
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// ParsePrivateKey decodes the key of `mygo keygen`, base64.
func ParsePrivateKey(s string) (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) != ed25519.PrivateKeySize {
		return nil, errors.New("release: invalid update private key")
	}
	return ed25519.PrivateKey(b), nil
}

// CheckKey reports whether key is the private half of publicKey, base64,
// as mygo.json gives it: an update signed with another is refused by every
// installed app.
func CheckKey(key ed25519.PrivateKey, publicKey string) error {
	if base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)) != strings.TrimSpace(publicKey) {
		return errors.New("release: the signing key does not match updates.publicKey in mygo.json")
	}
	return nil
}

// ReleaseNotes returns the section of version in a changelog, under a
// "## <version>" heading, without the heading.
func ReleaseNotes(changelog, version string) (string, error) {
	var out []string
	in := false
	for _, line := range strings.Split(changelog, "\n") {
		if heading, ok := strings.CutPrefix(line, "## "); ok {
			if in {
				break
			}
			v := strings.Fields(strings.Trim(heading, "[] "))
			in = len(v) > 0 && strings.Trim(v[0], "[]") == version
			continue
		}
		if in {
			out = append(out, line)
		}
	}
	notes := strings.TrimSpace(strings.Join(out, "\n"))
	if notes == "" {
		return "", fmt.Errorf("release: the changelog has no section for %s", version)
	}
	return notes, nil
}
