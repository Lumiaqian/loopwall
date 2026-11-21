package main

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/Lumiaqian/loopwall/internal/catalog"
	"github.com/Lumiaqian/loopwall/internal/share"
	"github.com/egoist/mygo/ui"
)

// TestThumbLoadsLazily checks that a library thumbnail is read on first
// use only, that a video's missing one is remembered, and that the cache
// stays within its bound.
func TestThumbLoadsLazily(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "videos", idOne)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	a := &application{root: root}
	if a.thumb(idOne) != nil {
		t.Fatal("a thumbnail not yet written was found")
	}
	if err := os.WriteFile(catalog.ThumbnailPath(root, catalog.MediaEntry{ID: idOne}), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if a.thumb(idOne) != nil {
		t.Fatal("a miss was read again")
	}
	a.thumbs.Remove(idOne) // as a delete or an eviction does
	bm := a.thumb(idOne)
	if bm == nil {
		t.Fatal("the thumbnail did not load")
	}
	if a.thumb(idOne) != bm {
		t.Fatal("the thumbnail was decoded again")
	}
	for i := 0; i < 2*thumbCacheSize; i++ {
		a.thumbs.Put(string(rune('a'+i%26))+string(rune(i)), (*ui.Bitmap)(nil))
	}
	if a.thumbs.Len() > thumbCacheSize {
		t.Fatalf("cache holds %d", a.thumbs.Len())
	}
}

func TestMissingThumbnailsSkipsCached(t *testing.T) {
	s := &sharing{thumbs: newLRU[string, *ui.Bitmap](thumbCacheSize)}
	p := share.Peer{Device: "dev"}
	s.thumbs.Put("dev/x", &ui.Bitmap{})
	got := s.missingThumbnails(p, []share.Item{{ID: "x"}, {ID: "y"}})
	if len(got) != 1 || got[0].ID != "y" {
		t.Fatalf("missing %v", got)
	}
}
