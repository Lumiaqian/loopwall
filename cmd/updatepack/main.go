// Command updatepack writes the signed update of a built Loopwall.app: its
// archive and update-<target>.json, for the GitHub release of its version
// (see internal/release). The key comes from MYGO_UPDATER_PRIVATE_KEY.
//
//	go run ./cmd/updatepack -app dist/darwin-arm64/Loopwall.app -out dist
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/Lumiaqian/loopwall/internal/release"
)

func main() {
	app := flag.String("app", "dist/darwin-arm64/Loopwall.app", "the app bundle as shipped, its extension in it")
	out := flag.String("out", "dist", "where the archive and manifest go")
	target := flag.String("target", "darwin-arm64", "the platform the bundle runs on")
	changelog := flag.String("changelog", "CHANGELOG.md", "whose section of the version is the release notes")
	flag.Parse()
	log.SetFlags(0)

	var cfg struct {
		Slug    string `json:"slug"`
		Version string `json:"version"`
		Updates struct {
			PublicKey string `json:"publicKey"`
			GitHub    string `json:"github"`
		} `json:"updates"`
	}
	b, err := os.ReadFile("mygo.json")
	if err == nil {
		err = json.Unmarshal(b, &cfg)
	}
	if err != nil {
		log.Fatalf("mygo.json: %v", err)
	}
	if cfg.Updates.GitHub == "" || cfg.Updates.PublicKey == "" {
		log.Fatal("mygo.json: updates needs github and publicKey")
	}
	key, err := release.ParsePrivateKey(os.Getenv("MYGO_UPDATER_PRIVATE_KEY"))
	if err != nil {
		log.Fatal("MYGO_UPDATER_PRIVATE_KEY: ", err)
	}
	if err := release.CheckKey(key, cfg.Updates.PublicKey); err != nil {
		log.Fatal(err)
	}
	text, err := os.ReadFile(*changelog)
	if err != nil {
		log.Fatal(err)
	}
	notes, err := release.ReleaseNotes(string(text), cfg.Version)
	if err != nil {
		log.Fatal(err)
	}
	archive, manifest, err := release.Package(release.Options{
		App: *app, Out: *out, Slug: cfg.Slug, Version: cfg.Version, Target: *target, Notes: notes, Key: key,
		Download: fmt.Sprintf("https://github.com/%s/releases/download/v%s/", cfg.Updates.GitHub, cfg.Version),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(archive)
	fmt.Println(manifest)
}
