package oxibooru

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"os"
	"strconv"
	"testing"
)

// TestLiveServer runs against the server at $MAIBOORU_TEST_SERVER, as one
// started with Oxibooru's docker-compose.yml, and makes users, posts, tags
// and pools there.
func TestLiveServer(t *testing.T) {
	addr := os.Getenv("MAIBOORU_TEST_SERVER")
	if addr == "" {
		t.Skip("MAIBOORU_TEST_SERVER is not set")
	}
	ctx := t.Context()
	ep, err := Discover(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(ep.API, ep.Site)
	if err != nil {
		t.Fatal(err)
	}
	// The user given, or a new one, who is an administrator on a new
	// server.
	name, pass := os.Getenv("MAIBOORU_TEST_USER"), os.Getenv("MAIBOORU_TEST_PASSWORD")
	if name == "" {
		name, pass = fmt.Sprintf("test%d", rand.IntN(1_000_000)), "secret1"
		if _, err := c.CreateUser(ctx, UserFields{Name: &name, Password: &pass}); err != nil {
			t.Fatal(err)
		}
	}
	c.SetCredentials(Credentials{User: name, Password: pass})
	me, err := c.User(ctx, name, true)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := c.CreateUserToken(ctx, name, TokenFields{Enabled: new(true), Note: new("live test")})
	if err != nil {
		t.Fatal(err)
	}
	c.SetCredentials(Credentials{User: name, Token: tok.Token})
	t.Logf("user %s, rank %s", me.Name, me.Rank)

	cats, err := c.TagCategories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) == 0 {
		t.Fatal("no tag categories")
	}

	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	seed := uint8(rand.IntN(255))
	for y := range 48 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 5), seed, 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	token, err := c.Upload(ctx, "test.png", bytes.NewReader(buf.Bytes()), int64(buf.Len()), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReverseSearch(ctx, token); err != nil {
		t.Fatal(err)
	}
	suffix := strconv.Itoa(rand.IntN(1_000_000))
	tags := []string{"live_" + suffix, "maibooru_test"}
	post, err := c.CreatePost(ctx, PostFields{Tags: &tags, Safety: new(Safe), ContentToken: token, Description: new("hello")})
	if err != nil {
		t.Fatal(err)
	}
	szuru := ep.Info.Config.Szurubooru()
	if post.CanvasWidth != 64 || len(post.Tags) != 2 || !szuru && post.Description != "hello" {
		t.Errorf("post %+v", post)
	}

	page, err := c.Posts(ctx, Search{Query: Escape("live_" + suffix), Limit: 10, Fields: []string{"id", "thumbnailUrl"}})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Results[0].ID != post.ID || page.Results[0].ThumbnailURL == "" {
		t.Errorf("search %+v", page)
	}

	tags = append(tags, "added")
	post, err = c.UpdatePost(ctx, post.ID, PostFields{Version: post.Version, Tags: &tags})
	if err != nil {
		t.Fatal(err)
	}
	if len(post.Tags) != 3 {
		t.Errorf("tags after update %v", post.TagNames())
	}
	stale := Version("2001-01-01T00:00:00Z")
	if szuru {
		stale = "0"
	}
	if _, err := c.UpdatePost(ctx, post.ID, PostFields{Version: stale, Tags: &tags}); !errors.Is(err, ErrConflict) {
		t.Errorf("outdated update: %v", err)
	}
	if post, err = c.SetFavorite(ctx, post.ID, true); err != nil || !post.OwnFavorite {
		t.Errorf("favorite: %v %v", err, post.OwnFavorite)
	}
	if post, err = c.RatePost(ctx, post.ID, 1); err != nil || post.OwnScore != 1 {
		t.Errorf("rate: %v", err)
	}
	cm, err := c.CreateComment(ctx, post.ID, "nice *one*")
	if err != nil {
		t.Fatal(err)
	}
	if cm, err = c.UpdateComment(ctx, cm, "nicer"); err != nil || cm.Text != "nicer" {
		t.Errorf("comment: %v", err)
	}

	var buf2 bytes.Buffer
	if err := c.Download(ctx, post.ContentURL, &buf2, nil); err != nil || buf2.Len() != buf.Len() {
		t.Errorf("download: %v, %d bytes", err, buf2.Len())
	}

	poolName := "pool_" + suffix
	pool, err := c.CreatePool(ctx, PoolFields{Names: &[]string{poolName}, Category: new("default"), Posts: &[]int64{post.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if pool.PostCount != 1 {
		t.Errorf("pool %+v", pool)
	}
	tag, err := c.Tag(ctx, "added")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateTag(ctx, tag.Name(), TagFields{Version: tag.Version, Description: new("a tag")}); err != nil {
		t.Fatal(err)
	}
	snaps, err := c.Snapshots(ctx, Search{Limit: 5})
	if err != nil || len(snaps.Results) == 0 {
		t.Fatalf("snapshots: %v", err)
	}
	t.Logf("latest change: %s %s %s", snaps.Results[0].Operation, snaps.Results[0].Type, snaps.Results[0].ResourceID())

	full, err := c.Post(ctx, post.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Pools) != 1 || len(full.Comments) != 1 {
		t.Errorf("full post: pools %v comments %v", full.Pools, full.Comments)
	}
	if err := c.DeletePool(ctx, pool.ID, pool.Version); err != nil {
		t.Error(err)
	}
	if err := c.DeleteComment(ctx, cm); err != nil {
		t.Error(err)
	}
	if err := c.DeletePost(ctx, full.ID, full.Version); err != nil {
		t.Error(err)
	}
	if err := c.DeleteUserToken(ctx, name, tok.Token); err != nil {
		t.Error(err)
	}
}
