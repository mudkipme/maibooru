package settings

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestSaveAndOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "settings.json")
	st, s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.ThumbnailSize != 180 || s.PageSize != 100 {
		t.Errorf("defaults %+v", s)
	}
	id := s.PutProfile(Profile{Name: "Home", URL: "booru.local", API: "https://booru.local/api/", Site: "https://booru.local/", User: "ada"})
	s.Active = id
	s.Saved[id] = []SavedSearch{{Name: "Cats", Query: "cat"}}
	s.Theme = ThemeDark
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	_, got, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := got.Profile(id)
	if !ok || p.User != "ada" || got.Active != id || got.Theme != ThemeDark || got.Saved[id][0].Query != "cat" {
		t.Errorf("read back %+v", got)
	}
	info, err := os.Stat(path)
	if err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, %v", info.Mode(), err)
	}

	got.RemoveProfile(id)
	if len(got.Profiles) != 0 || got.Active != "" || got.Saved[id] != nil {
		t.Errorf("after removing %+v", got)
	}
}

func TestBrokenFileStartsAfresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte("{nope"), 0o600)
	_, s, err := Open(path)
	if err == nil {
		t.Error("no error for a broken file")
	}
	if s == nil || s.ThumbnailSize == 0 {
		t.Error("no defaults for a broken file")
	}
}

func TestRecentTags(t *testing.T) {
	s := &Settings{}
	s.Defaults()
	s.AddRecentTags("p", "a", "b")
	s.AddRecentTags("p", "c", "a")
	if got := s.Recent["p"]; !slices.Equal(got, []string{"a", "c", "b"}) {
		t.Errorf("recent %v", got)
	}
	for i := range 100 {
		s.AddRecentTags("p", string(rune('A'+i%60))+"x")
	}
	if len(s.Recent["p"]) != 40 {
		t.Errorf("%d recent tags kept", len(s.Recent["p"]))
	}
}
