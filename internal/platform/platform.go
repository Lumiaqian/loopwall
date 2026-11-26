// Package platform is a native module: its interfaces are implemented in
// Swift, Rust or Zig, in the directory of the language (see AGENTS.md).
package platform

import "context"

type MediaInfo struct {
	Width     int64
	Height    int64
	FrameRate float64
	Duration  float64
}

type DisplayInfo struct {
	Identifier    string
	Name          string
	RuntimeNumber int64
	BuiltIn       bool
}

// Peer is another Mac sharing its library on the local network, as Bonjour
// found it: its service's name, the device identifier its TXT record
// carries, and the IPv4 address and port it resolved to.
type Peer struct {
	Name   string
	Device string
	Host   string
	Port   int64
}

// SharingStatus is how sharing on the local network fares: whether the
// Bonjour service is published, how browsing goes, and why either fails.
type SharingStatus struct {
	// Advertised is the service published, as Bonjour confirmed it.
	Advertised bool
	// AdvertiseError is why publishing failed, empty while it has not.
	AdvertiseError string
	// Browsing is the browser's state: "" before it starts, "ready",
	// "waiting" or "failed".
	Browsing    string
	BrowseError string
	// Denied is the local network permission denied, to publish or browse.
	Denied bool
}

// Host exposes the macOS operations needed by the local video library.
//
//mygo:native
type Host interface {
	// Probe validates a decodable video and writes its thumbnail.
	Probe(ctx context.Context, path string, thumbnailPath string) (MediaInfo, error)
	// Displays returns stable identifiers instead of display names as identities.
	//mygo:mainthread
	Displays() ([]DisplayInfo, error)
	// NotifyLibraryChanged invalidates this provider's tiles and notifies its extension.
	NotifyLibraryChanged() error
	// AgentStuckSignals counts the extension's calls for help since the host
	// was made: it posts one when WallpaperAgent stops serving it, for the
	// app to restart WallpaperAgent, which the sandboxed extension cannot.
	AgentStuckSignals() (int64, error)
	// ComputerName is the name this Mac goes by on the network.
	ComputerName() (string, error)
	// Advertise publishes the library shared on port as the Bonjour
	// service _loopwall._tcp named name, whose TXT record carries
	// device; advertising again replaces it.
	Advertise(name string, port int64, device string) error
	// StopAdvertising withdraws what Advertise published.
	StopAdvertising() error
	// Peers browses for shared libraries, from its first call on, and
	// returns those found and resolved so far. Browsing asks for the local
	// network the first time, so it waits until the user looks for peers.
	Peers() ([]Peer, error)
	// SharingStatus reports how publishing and browsing fare, which fail
	// silently otherwise, as for a denied local network permission.
	SharingStatus() (SharingStatus, error)
}

// NearbyEvent identifies a single verification attempt. Only connected sessions
// can deliver the credential payload from the other Mac.
type NearbyEvent struct {
	Attempt  string
	Kind     string
	Device   string
	Value    string
	Incoming bool
}

//mygo:native
type Nearby interface {
	Configure(device string, advertise bool, discover bool) error
	Request(device string) error
	Decide(attempt string, accept bool) error
	Send(attempt string, value string) error
	Next() (NearbyEvent, error)
}
