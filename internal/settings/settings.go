// Package settings keeps what Maibooru remembers between runs: the servers
// the user connects to, their saved searches and the app's preferences.
// Tokens are kept in the system's credential store, not here.
package settings

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// Profile is a server the user connects to, as a user or anonymously.
type Profile struct {
	ID string `json:"id"`
	// Name is the server's name, as its configuration gives it.
	Name string `json:"name"`
	// URL is the address the user gave; API and Site where its API and
	// its data are.
	URL  string `json:"url"`
	API  string `json:"api"`
	Site string `json:"site"`
	// User is the name of the user signed in, empty for none.
	User string `json:"user,omitzero"`
}

// SavedSearch is a search the user keeps in the sidebar.
type SavedSearch struct {
	Name  string `json:"name"`
	Query string `json:"query"`
	Icon  string `json:"icon,omitzero"`
	Color string `json:"color,omitzero"`
}

// Theme is the appearance of the app.
type Theme string

const (
	ThemeSystem Theme = ""
	ThemeLight  Theme = "light"
	ThemeDark   Theme = "dark"
)

// Settings is everything kept.
type Settings struct {
	Profiles []Profile `json:"profiles"`
	Active   string    `json:"active,omitzero"`

	// Saved holds the saved searches of each profile, by its ID.
	Saved map[string][]SavedSearch `json:"saved,omitzero"`
	// Recent holds the tags used last on each profile, newest first.
	Recent map[string][]string `json:"recent,omitzero"`

	Theme          Theme   `json:"theme,omitzero"`
	ThumbnailSize  float64 `json:"thumbnailSize,omitzero"`
	SidebarWidth   float32 `json:"sidebarWidth,omitzero"`
	InspectorWidth float32 `json:"inspectorWidth,omitzero"`
	HideInspector  bool    `json:"hideInspector,omitzero"`
	ShowFilters    bool    `json:"showFilters,omitzero"`
	Layout         string  `json:"layout,omitzero"`
	ShowNotes      bool    `json:"showNotes,omitzero"`
	BlurUnsafe     bool    `json:"blurUnsafe,omitzero"`
	AutoplayVideos bool    `json:"autoplayVideos,omitzero"`
	MuteVideos     bool    `json:"muteVideos,omitzero"`

	TransparencyGrid    bool `json:"transparencyGrid,omitzero"`
	UpscaleSmall        bool `json:"upscaleSmall,omitzero"`
	UnderscoresAsSpaces bool `json:"underscoresAsSpaces,omitzero"`
	NoKeyShortcuts      bool `json:"noKeyShortcuts,omitzero"`

	UploadSafety       string `json:"uploadSafety,omitzero"`
	KeepDuplicates     bool   `json:"keepDuplicates,omitzero"`
	UploadAnonymously  bool   `json:"uploadAnonymously,omitzero"`
	UploadSimilar      bool   `json:"uploadSimilar,omitzero"`
	KeepGoingOnError   bool   `json:"keepGoingOnError,omitzero"`
	CacheLimitMB       int    `json:"cacheLimitMB,omitzero"`
	DownloadFolder     string `json:"downloadFolder,omitzero"`
	PageSize           int    `json:"pageSize,omitzero"`
	ConfirmDeletePosts bool   `json:"confirmDeletePosts,omitzero"`

	// PINHash is the salted hash of the PIN that unlocks the app, empty
	// when the app doesn't lock.
	PINHash string `json:"pinHash,omitzero"`
	// TouchID lets Touch ID unlock the app instead of the PIN.
	TouchID bool `json:"touchID,omitzero"`
}

// Defaults fills what is unset with the defaults.
func (s *Settings) Defaults() {
	if s.ThumbnailSize == 0 {
		s.ThumbnailSize = 180
	}
	if s.SidebarWidth == 0 {
		s.SidebarWidth = 220
	}
	if s.InspectorWidth == 0 {
		s.InspectorWidth = 320
	}
	if s.UploadSafety == "" {
		s.UploadSafety = "safe"
	}
	if s.CacheLimitMB == 0 {
		s.CacheLimitMB = 2048
	}
	if s.PageSize == 0 {
		s.PageSize = 100
	}
	if s.Saved == nil {
		s.Saved = map[string][]SavedSearch{}
	}
	if s.Recent == nil {
		s.Recent = map[string][]string{}
	}
}

// Profile returns the profile of an ID.
func (s *Settings) Profile(id string) (*Profile, bool) {
	i := slices.IndexFunc(s.Profiles, func(p Profile) bool { return p.ID == id })
	if i < 0 {
		return nil, false
	}
	return &s.Profiles[i], true
}

// PutProfile adds a profile, or replaces the one of its ID, and returns
// its ID, made up for a new one.
func (s *Settings) PutProfile(p Profile) string {
	if p.ID == "" {
		p.ID = NewID()
	}
	if old, ok := s.Profile(p.ID); ok {
		*old = p
	} else {
		s.Profiles = append(s.Profiles, p)
	}
	return p.ID
}

// RemoveProfile forgets a profile and what was kept for it.
func (s *Settings) RemoveProfile(id string) {
	s.Profiles = slices.DeleteFunc(s.Profiles, func(p Profile) bool { return p.ID == id })
	delete(s.Saved, id)
	delete(s.Recent, id)
	if s.Active == id {
		s.Active = ""
	}
}

// AddRecentTags puts tags at the front of a profile's recent tags.
func (s *Settings) AddRecentTags(profile string, tags ...string) {
	const keep = 40
	recent := s.Recent[profile]
	for _, t := range tags {
		recent = slices.DeleteFunc(recent, func(r string) bool { return r == t })
		recent = slices.Insert(recent, 0, t)
	}
	if len(recent) > keep {
		recent = recent[:keep]
	}
	s.Recent[profile] = recent
}

// NewID returns a random identifier.
func NewID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Store keeps the settings in a file.
type Store struct {
	path string
	mu   sync.Mutex
}

// Open returns the store of the file at path and the settings it has, the
// defaults when it has none yet.
func Open(path string) (*Store, *Settings, error) {
	st := &Store{path: path}
	s := &Settings{}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return st, defaults(s), err
	default:
		if err := json.Unmarshal(b, s); err != nil {
			return st, defaults(&Settings{}), err
		}
	}
	return st, defaults(s), nil
}

func defaults(s *Settings) *Settings {
	s.Defaults()
	return s
}

// Save writes the settings, replacing the file at once so that a crash
// leaves the old or the new.
func (st *Store) Save(s *Settings) error {
	if st == nil || st.path == "" {
		return nil
	}
	b, err := json.Marshal(s, json.Deterministic(true))
	if err != nil {
		return err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(st.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(st.path), ".settings-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), st.path)
}
