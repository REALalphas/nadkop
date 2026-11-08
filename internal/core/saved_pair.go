package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const SaveIntentFile = ".save-state.json"

type savedPair struct {
	Config string `json:"config"`
	Nodes  string `json:"nodes"`
}
type saveIntent struct {
	Version int       `json:"version"`
	Old     savedPair `json:"old"`
	New     savedPair `json:"new"`
}

func pairHashes(config, nodes []byte) savedPair {
	c, n := sha256.Sum256(config), sha256.Sum256(nodes)
	return savedPair{hex.EncodeToString(c[:]), hex.EncodeToString(n[:])}
}
func SaveIntentBytes(oldC, oldN, newC, newN []byte) []byte {
	data, _ := json.Marshal(saveIntent{1, pairHashes(oldC, oldN), pairHashes(newC, newN)})
	return data
}

func SaveIntentJSONBytes(oldC, oldN, newC, newN []byte) []byte {
	hash := func(data []byte) string {
		h := sha256.New()
		h.Write(data)
		h.Write([]byte{'\n'})
		return hex.EncodeToString(h.Sum(nil))
	}
	data, _ := json.Marshal(saveIntent{1, pairHashes(oldC, oldN), savedPair{hash(newC), hash(newN)}})
	return data
}

// Only hashes are persisted. This detects an interrupted mixed publication; it
// neither retains credentials nor replays or guesses which version to restore.
func VerifySavedPair(configPath string, config, nodes []byte) error {
	path := filepath.Join(filepath.Dir(configPath), SaveIntentFile)
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		return nil
	}
	failure := errors.New("Interrupted save: configuration/local nodes are unconfirmed or inconsistent; restore a matching config.json and nodes.json over SSH before retrying")
	if err != nil {
		return failure
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1024 {
		return failure
	}
	data, err := io.ReadAll(io.LimitReader(f, 1025))
	if err != nil || len(data) > 1024 {
		return failure
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var intent saveIntent
	if d.Decode(&intent) != nil || d.Decode(new(any)) != io.EOF || intent.Version != 1 {
		return failure
	}
	for _, hash := range []string{intent.Old.Config, intent.Old.Nodes, intent.New.Config, intent.New.Nodes} {
		if b, e := hex.DecodeString(hash); e != nil || len(b) != 32 {
			return failure
		}
	}
	pair := pairHashes(config, nodes)
	if pair != intent.Old && pair != intent.New {
		return failure
	}
	return nil
}

func CheckSavedPair(configPath string) error {
	if _, err := os.Lstat(filepath.Join(filepath.Dir(configPath), SaveIntentFile)); os.IsNotExist(err) {
		return nil
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	nodes, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "nodes.json"))
	if err != nil {
		return err
	}
	return VerifySavedPair(configPath, config, nodes)
}

// Called only with the configuration lock held. Both old and new complete
// pairs are usable. Mixed/unknown bytes keep the marker for manual recovery.
func resolveSavedPair(configPath string) error {
	path := filepath.Join(filepath.Dir(configPath), SaveIntentFile)
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil
	}
	if err := CheckSavedPair(configPath); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
