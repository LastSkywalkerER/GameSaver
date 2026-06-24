// Package secrets is a tiny per-user encrypted key/value store for OAuth tokens
// and API keys used by the store-library integrations (#5).
//
// 🔴 Tokens must NEVER land in settings.json, logs, or the repo (secrets.md).
// Values are encrypted at rest with Windows DPAPI (CryptProtectData, current-
// user scope) and written to %LOCALAPPDATA%\GameSaver\secrets.dat. Only the
// logged-in user on this machine can decrypt them; copying the file elsewhere
// yields ciphertext. Error messages mention only scope+account, never a value.
package secrets

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ErrNotFound is returned when a (scope, account) pair has no stored value.
var ErrNotFound = errors.New("secret not found")

// Vault is a DPAPI-backed key/value store keyed by (scope, account).
type Vault struct {
	mu   sync.Mutex
	path string
	data map[string]string // "scope\x00account" -> base64(DPAPI ciphertext)
}

// Open loads (or creates) the vault file under dir.
func Open(dir string) (*Vault, error) {
	v := &Vault{path: filepath.Join(dir, "secrets.dat"), data: map[string]string{}}
	if b, err := os.ReadFile(v.path); err == nil {
		_ = json.Unmarshal(b, &v.data) // corrupt/missing → start empty
	}
	return v, nil
}

func vkey(scope, account string) string { return scope + "\x00" + account }

// Get returns the decrypted plaintext for (scope, account), or ErrNotFound.
func (v *Vault) Get(scope, account string) ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	enc, ok := v.data[vkey(scope, account)]
	if !ok {
		return nil, ErrNotFound
	}
	blob, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, err
	}
	return dpapiUnprotect(blob)
}

// Set encrypts and stores plaintext under (scope, account).
func (v *Vault) Set(scope, account string, plaintext []byte) error {
	blob, err := dpapiProtect(plaintext)
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.data[vkey(scope, account)] = base64.StdEncoding.EncodeToString(blob)
	return v.save()
}

// Delete removes the value for (scope, account).
func (v *Vault) Delete(scope, account string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.data, vkey(scope, account))
	return v.save()
}

func (v *Vault) save() error {
	b, err := json.Marshal(v.data)
	if err != nil {
		return err
	}
	tmp := v.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, v.path)
}

// ─── DPAPI ────────────────────────────────────────────────────────────────

var (
	crypt32            = windows.NewLazySystemDLL("crypt32.dll")
	procCryptProtect   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotect = crypt32.NewProc("CryptUnprotectData")
	kernel32           = windows.NewLazySystemDLL("kernel32.dll")
	procLocalFree      = kernel32.NewProc("LocalFree")
)

const cryptProtectUIForbidden = 0x1

// dataBlob mirrors Win32 DATA_BLOB { DWORD cbData; BYTE* pbData; } — 16 B on
// x64 (Go pads the DWORD to align the pointer). Don't add padding.
type dataBlob struct {
	cbData uint32
	pbData *byte
}

func dpapiProtect(in []byte) ([]byte, error) {
	var inBlob dataBlob
	if len(in) > 0 {
		inBlob = dataBlob{cbData: uint32(len(in)), pbData: &in[0]}
	}
	var out dataBlob
	r, _, err := procCryptProtect.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0, 0, 0, 0,
		uintptr(cryptProtectUIForbidden),
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return blobBytes(out), nil
}

func dpapiUnprotect(in []byte) ([]byte, error) {
	var inBlob dataBlob
	if len(in) > 0 {
		inBlob = dataBlob{cbData: uint32(len(in)), pbData: &in[0]}
	}
	var out dataBlob
	r, _, err := procCryptUnprotect.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0, 0, 0, 0,
		uintptr(cryptProtectUIForbidden),
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return blobBytes(out), nil
}

func blobBytes(b dataBlob) []byte {
	if b.pbData == nil || b.cbData == 0 {
		return nil
	}
	src := unsafe.Slice(b.pbData, b.cbData)
	out := make([]byte, b.cbData)
	copy(out, src)
	return out
}
