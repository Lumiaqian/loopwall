// Package share lets Macs on a local network pull copies of each other's
// videos: a server that shares this Mac's library over HTTPS to the
// devices paired with it, and a client that pairs with other Macs and
// downloads from them. Bonjour, which finds them, is the platform's.
package share

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Lumiaqian/loopwall/internal/catalog"
)

// Granted is a device paired to pull from this Mac.
type Granted struct {
	Name      string `json:"name"`
	TokenHash string `json:"tokenHash"` // SHA-256 of its token, hex
	PairedAt  int64  `json:"pairedAt"`
}

// Paired is a Mac this one pulls from.
type Paired struct {
	Name        string `json:"name"`
	Token       string `json:"token"`
	Fingerprint string `json:"fingerprint"` // SHA-256 of its certificate, hex
}

// state is share.json: this Mac's identity, the devices granted its
// library, the Macs it pulls from, and what it pulled.
type state struct {
	Device  string             `json:"device"`
	Granted map[string]Granted `json:"granted,omitempty"`
	Peers   map[string]Paired  `json:"peers,omitempty"`
	// Imported maps a peer's device and its video's ID, "device/id", to
	// the local copy's ID.
	Imported map[string]string `json:"imported,omitempty"`
}

// Store keeps sharing's state in a directory, with the certificate the
// server presents. Its methods may be called from any goroutine.
type Store struct {
	dir string
	mu  sync.Mutex
	s   state
}

// OpenStore loads the state kept in dir, making this Mac's identity the
// first time.
func OpenStore(dir string) (*Store, error) {
	st := &Store{dir: dir}
	b, err := os.ReadFile(st.path())
	switch {
	case err == nil:
		if err = json.Unmarshal(b, &st.s); err != nil {
			return nil, err
		}
	case !os.IsNotExist(err):
		return nil, err
	}
	if st.s.Device == "" {
		st.s.Device = randomHex(16)
		if err = st.saveLocked(); err != nil {
			return nil, err
		}
	}
	return st, nil
}

func (st *Store) path() string { return filepath.Join(st.dir, "share.json") }

func (st *Store) saveLocked() error { return catalog.AtomicJSON(st.path(), st.s) }

// Device is this Mac's identifier, which its Bonjour record carries.
func (st *Store) Device() string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Device
}

// Grant records a device paired to pull from this Mac, by its token's hash.
func (st *Store) Grant(device, name, token string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.s.Granted == nil {
		st.s.Granted = map[string]Granted{}
	}
	st.s.Granted[device] = Granted{Name: name, TokenHash: hashHex(token), PairedAt: time.Now().Unix()}
	return st.saveLocked()
}

// Revoke forgets a device paired to pull from this Mac.
func (st *Store) Revoke(device string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.s.Granted, device)
	return st.saveLocked()
}

// Granted returns the devices paired to pull from this Mac.
func (st *Store) Granted() map[string]Granted {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make(map[string]Granted, len(st.s.Granted))
	for k, v := range st.s.Granted {
		out[k] = v
	}
	return out
}

func (st *Store) granted(device string) (Granted, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	g, ok := st.s.Granted[device]
	return g, ok
}

// Pair records a Mac this one pulls from.
func (st *Store) Pair(device string, p Paired) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.s.Peers == nil {
		st.s.Peers = map[string]Paired{}
	}
	st.s.Peers[device] = p
	return st.saveLocked()
}

// Unpair forgets a Mac this one pulls from.
func (st *Store) Unpair(device string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.s.Peers, device)
	return st.saveLocked()
}

// Paired returns how this Mac pulls from device, if it is paired.
func (st *Store) Paired(device string) (Paired, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	p, ok := st.s.Peers[device]
	return p, ok
}

// Imported returns the local ID of the copy of device's video id, if one
// was pulled.
func (st *Store) Imported(device, id string) (string, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	local, ok := st.s.Imported[device+"/"+id]
	return local, ok
}

// RecordImport remembers that device's video id was pulled as local.
func (st *Store) RecordImport(device, id, local string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.s.Imported == nil {
		st.s.Imported = map[string]string{}
	}
	st.s.Imported[device+"/"+id] = local
	return st.saveLocked()
}

// Certificate returns the certificate the server presents, making it the
// first time: self-signed, as the devices pin its fingerprint on pairing.
func (st *Store) Certificate() (tls.Certificate, error) {
	certPath, keyPath := filepath.Join(st.dir, "share-cert.pem"), filepath.Join(st.dir, "share-key.pem")
	if c, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		return c, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Loopwall " + st.Device()},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(20, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err = os.MkdirAll(st.dir, 0700); err != nil {
		return tls.Certificate{}, err
	}
	if err = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		return tls.Certificate{}, err
	}
	if err = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		return tls.Certificate{}, err
	}
	return tls.LoadX509KeyPair(certPath, keyPath)
}

// Fingerprint is the SHA-256 of a certificate, hex, which pairing pins.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

func hashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(errors.New("share: no randomness: " + err.Error()))
	}
	return hex.EncodeToString(b)
}
