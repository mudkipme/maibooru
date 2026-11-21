package oxibooru

import (
	"bytes"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoverFindsAPIUnderSite(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"postCount":3,"config":{"name":"test","privileges":{"post_edit_tag":"regular"}}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ep, err := Discover(t.Context(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if ep.API != srv.URL+"/api/" || ep.Site != srv.URL+"/" {
		t.Errorf("endpoint %+v", ep)
	}
	if ep.Info.PostCount != 3 || ep.Info.Config.Name != "test" {
		t.Errorf("info %+v", ep.Info)
	}

	ep, err = Discover(t.Context(), srv.URL+"/api")
	if err != nil {
		t.Fatal(err)
	}
	if ep.API != srv.URL+"/api/" || ep.Site != srv.URL+"/" {
		t.Errorf("endpoint of the API's own URL %+v", ep)
	}
}

func TestAuthorizationAndErrors(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"name":"ResourceModified","title":"Resource Modified","description":"Someone else modified this in the meantime."}`))
			return
		}
		w.Write([]byte(`{"id":7,"version":"2024-09-14T19:06:56.979184564Z","tags":[{"names":["a","b"],"category":"default","usages":2}]}`))
	}))
	defer srv.Close()

	c, err := New(srv.URL+"/api", srv.URL, WithCredentials(Credentials{User: "ada", Token: "tok"}))
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.Post(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Token " + base64.StdEncoding.EncodeToString([]byte("ada:tok")); auth != want {
		t.Errorf("Authorization %q, want %q", auth, want)
	}
	if p.ID != 7 || p.Version != "2024-09-14T19:06:56.979184564Z" || p.TagNames()[0] != "a" {
		t.Errorf("post %+v", p)
	}
	tags := []string{"x"}
	_, err = c.UpdatePost(t.Context(), 7, PostFields{Version: p.Version, Tags: &tags})
	if !errors.Is(err, ErrConflict) || StatusOf(err) != http.StatusConflict {
		t.Errorf("error %v, want a conflict", err)
	}
	if err.Error() != "Someone else modified this in the meantime." {
		t.Errorf("message %q", err)
	}
}

func TestPostFieldsSendOnlyWhatIsSet(t *testing.T) {
	empty := []string{}
	safety := Unsafe
	b, err := json.Marshal(PostFields{Version: "v", Tags: &empty, Safety: &safety})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `{"version":"v","tags":[],"safety":"unsafe"}`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestPrivateFields(t *testing.T) {
	var u User
	if err := json.Unmarshal([]byte(`{"name":"a","email":false,"likedPostCount":3}`), &u); err != nil {
		t.Fatal(err)
	}
	if !u.Email.Hidden || u.LikedPostCount.Value != 3 || u.LikedPostCount.Hidden {
		t.Errorf("user %+v", u)
	}
	if err := json.Unmarshal([]byte(`{"name":"a","email":"a@b.c","likedPostCount":false}`), &u); err != nil {
		t.Fatal(err)
	}
	if u.Email.Hidden || u.Email.Value == nil || *u.Email.Value != "a@b.c" || !u.LikedPostCount.Hidden {
		t.Errorf("user %+v", u)
	}
}

func TestUploadSendsMultipartWithLength(t *testing.T) {
	var got []byte
	var length int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		length = r.ContentLength
		mr, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			return
		}
		part, err := mr.NextPart()
		if err != nil {
			t.Error(err)
			return
		}
		if part.FormName() != "content" || part.FileName() != "cat.png" {
			t.Errorf("part %q %q", part.FormName(), part.FileName())
		}
		got, _ = io.ReadAll(part)
		w.Write([]byte(`{"token":"abc"}`))
	}))
	defer srv.Close()
	c, _ := New(srv.URL, srv.URL)
	content := bytes.Repeat([]byte("x"), 10000)
	var last, total int64
	token, err := c.Upload(t.Context(), "/tmp/cat.png", bytes.NewReader(content), int64(len(content)), func(done, n int64) {
		last, total = done, n
	})
	if err != nil {
		t.Fatal(err)
	}
	if token != "abc" || !bytes.Equal(got, content) || last != length || total != length {
		t.Errorf("token %q, %d bytes, progress %d of %d, sent %d", token, len(got), last, total, length)
	}
}

func TestPrivileges(t *testing.T) {
	cfg := &Config{Privileges: map[string]Rank{
		"post_edit_tag":    Regular,
		"post_edit_safety": Power,
		"post_delete":      Moderator,
	}}
	for _, tc := range []struct {
		rank Rank
		priv string
		want bool
	}{
		{Regular, "post_edit_tag", true},
		{Regular, "postEditSafety", false},
		{Power, "post_edit_safety", true},
		{Regular, "post_edit", true},
		{Power, "post_delete", false},
		{Anonymous, "unknown_thing", true},
	} {
		if got := cfg.Can(tc.rank, tc.priv); got != tc.want {
			t.Errorf("Can(%s, %s) = %v", tc.rank, tc.priv, got)
		}
	}
}

func TestSzurubooruPrivileges(t *testing.T) {
	cfg := &Config{Privileges: map[string]Rank{
		"posts:edit:tags":         Regular,
		"posts:create:anonymous":  Power,
		"tag_categories:create":   Moderator,
		"users:edit:self:pass":    Regular,
		"user_tokens:create:self": Regular,
	}}
	for _, tc := range []struct {
		rank Rank
		priv string
		want bool
	}{
		{Regular, "post_edit_tag", true},
		{Regular, "post_create_anonymous", false},
		{Regular, "tag_category_create", false},
		{Moderator, "tag_category_create", true},
		{Regular, "user_edit_self_pass", true},
		{Restricted, "user_token_create_self", false},
	} {
		if got := cfg.Can(tc.rank, tc.priv); got != tc.want {
			t.Errorf("Can(%s, %s) = %v", tc.rank, tc.priv, got)
		}
	}
}

func TestQueryTokens(t *testing.T) {
	q := `cat -dog sort:score "two words" re\:zero`
	toks := Tokens(q)
	want := []string{"cat", "-dog", "sort:score", `"two words"`, `re\:zero`}
	if strings.Join(toks, "|") != strings.Join(want, "|") {
		t.Errorf("tokens %q", toks)
	}
	if tok := ParseToken(`re\:zero`); tok.Key != "" || Unescape(tok.Value) != "re:zero" {
		t.Errorf("escaped token %+v", tok)
	}
	if tok := ParseToken("-sort:score"); !tok.Negated || tok.Key != "sort" || tok.Value != "score" {
		t.Errorf("token %+v", tok)
	}
	if got := SetToken(q, "sort", "id"); got != `cat -dog sort:id "two words" re\:zero` {
		t.Errorf("SetToken %q", got)
	}
	if got := SetToken("cat", "type", "video"); got != "cat type:video" {
		t.Errorf("SetToken added %q", got)
	}
	if got := SetToken("cat sort:id", "sort", ""); got != "cat" {
		t.Errorf("SetToken removed %q", got)
	}
	if got := Escape("re:zero"); got != `re\:zero` || Unescape(got) != "re:zero" {
		t.Errorf("Escape %q", got)
	}
	if got := Escape("-x*"); got != `\-x\*` {
		t.Errorf("Escape %q", got)
	}
}
