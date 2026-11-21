package oxibooru

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Search is a query of a list, a page of it.
type Search struct {
	Query  string
	Offset int
	Limit  int
	// Fields chooses the fields of the resources, all when empty.
	Fields []string
}

func (s Search) values() url.Values {
	q := fieldsQuery(s.Fields)
	if q == nil {
		q = url.Values{}
	}
	if s.Query != "" {
		q.Set("query", s.Query)
	}
	q.Set("offset", strconv.Itoa(s.Offset))
	if s.Limit > 0 {
		q.Set("limit", strconv.Itoa(s.Limit))
	}
	return q
}

func id(n int64) string { return strconv.FormatInt(n, 10) }

// Info returns the server's statistics and public configuration.
func (c *Client) Info(ctx context.Context) (*Info, error) {
	return get[*Info](ctx, c, "info", nil)
}

// Posts.

// Posts searches for posts.
func (c *Client) Posts(ctx context.Context, s Search) (*Page[Post], error) {
	return get[*Page[Post]](ctx, c, "posts/", s.values())
}

// Post returns a post.
func (c *Client) Post(ctx context.Context, postID int64, fields ...string) (*Post, error) {
	return get[*Post](ctx, c, "post/"+id(postID), fieldsQuery(fields))
}

// Around is the posts before and after one, in the order of a search.
type Around struct {
	Prev *Post `json:"prev"`
	Next *Post `json:"next"`
}

// PostAround returns the posts before and after a post among those a query
// finds.
func (c *Client) PostAround(ctx context.Context, postID int64, query string, fields ...string) (*Around, error) {
	q := fieldsQuery(fields)
	if q == nil {
		q = url.Values{}
	}
	if query != "" {
		q.Set("query", query)
	}
	return get[*Around](ctx, c, "post/"+id(postID)+"/around", q)
}

// FeaturedPost returns the post featured on the server's front page, nil
// for none.
func (c *Client) FeaturedPost(ctx context.Context) (*Post, error) {
	return get[*Post](ctx, c, "featured-post", nil)
}

// FeaturePost features a post on the front page.
func (c *Client) FeaturePost(ctx context.Context, postID int64) (*Post, error) {
	return send[*Post](ctx, c, http.MethodPost, "featured-post", nil, map[string]int64{"id": postID})
}

// PostFields is what creating or updating a post sets: only the fields that
// are not nil, and Version for an update.
type PostFields struct {
	Version     string    `json:"version,omitzero"`
	Tags        *[]string `json:"tags,omitzero"`
	Safety      *Safety   `json:"safety,omitzero"`
	Source      *string   `json:"source,omitzero"`
	Description *string   `json:"description,omitzero"`
	Relations   *[]int64  `json:"relations,omitzero"`
	Notes       *[]Note   `json:"notes,omitzero"`
	Flags       *[]string `json:"flags,omitzero"`
	Anonymous   *bool     `json:"anonymous,omitzero"`
	// ContentToken is a temporary upload's token (Client.Upload) to take the
	// content from; ContentURL a URL the server downloads it from.
	ContentToken   string `json:"contentToken,omitzero"`
	ContentURL     string `json:"contentUrl,omitzero"`
	ThumbnailToken string `json:"thumbnailToken,omitzero"`
	ThumbnailURL   string `json:"thumbnailUrl,omitzero"`
}

// CreatePost creates a post of content a temporary upload or a URL gives
// (f.ContentToken or f.ContentURL).
func (c *Client) CreatePost(ctx context.Context, f PostFields) (*Post, error) {
	return send[*Post](ctx, c, http.MethodPost, "posts/", nil, f)
}

// UpdatePost changes the fields of a post that f sets.
func (c *Client) UpdatePost(ctx context.Context, postID int64, f PostFields) (*Post, error) {
	return send[*Post](ctx, c, http.MethodPut, "post/"+id(postID), nil, f)
}

// DiscardThumbnail goes back to the thumbnail the server makes of a post's
// content, from a custom one.
func (c *Client) DiscardThumbnail(ctx context.Context, postID int64, version string) (*Post, error) {
	return send[*Post](ctx, c, http.MethodPut, "post/"+id(postID), nil, map[string]any{"version": version, "thumbnailToken": nil})
}

// DeletePost deletes a post.
func (c *Client) DeletePost(ctx context.Context, postID int64, version string) error {
	return c.do(ctx, http.MethodDelete, "post/"+id(postID), nil, versioned{version}, nil)
}

// MergePosts merges the post remove into mergeTo, its content too when
// replaceContent.
func (c *Client) MergePosts(ctx context.Context, remove, mergeTo *Post, replaceContent bool) (*Post, error) {
	body := map[string]any{
		"remove": remove.ID, "removeVersion": remove.Version,
		"mergeTo": mergeTo.ID, "mergeToVersion": mergeTo.Version,
		"replaceContent": replaceContent,
	}
	return send[*Post](ctx, c, http.MethodPost, "post-merge/", nil, body)
}

// SetFavorite adds a post to the user's favorites, or takes it out.
func (c *Client) SetFavorite(ctx context.Context, postID int64, favorite bool) (*Post, error) {
	method := http.MethodPost
	if !favorite {
		method = http.MethodDelete
	}
	return send[*Post](ctx, c, method, "post/"+id(postID)+"/favorite", nil, nil)
}

// RatePost sets the user's score of a post: -1, 0 or 1.
func (c *Client) RatePost(ctx context.Context, postID int64, score int) (*Post, error) {
	return send[*Post](ctx, c, http.MethodPut, "post/"+id(postID)+"/score", nil, map[string]int{"score": score})
}

// ReverseSearch finds the posts that look like a temporary upload.
func (c *Client) ReverseSearch(ctx context.Context, contentToken string) (*ImageSearch, error) {
	return send[*ImageSearch](ctx, c, http.MethodPost, "posts/reverse-search", nil, map[string]string{"contentToken": contentToken})
}

// ReverseSearchURL finds the posts that look like the picture at a URL,
// which the server downloads.
func (c *Client) ReverseSearchURL(ctx context.Context, contentURL string) (*ImageSearch, error) {
	return send[*ImageSearch](ctx, c, http.MethodPost, "posts/reverse-search", nil, map[string]string{"contentUrl": contentURL})
}

// Tag categories.

// TagCategories lists the tag categories.
func (c *Client) TagCategories(ctx context.Context) ([]TagCategory, error) {
	r, err := get[results[TagCategory]](ctx, c, "tag-categories", nil)
	return r.Results, err
}

// CategoryFields is what creating or updating a category sets.
type CategoryFields struct {
	Version string  `json:"version,omitzero"`
	Name    *string `json:"name,omitzero"`
	Color   *string `json:"color,omitzero"`
	Order   *int    `json:"order,omitzero"`
}

// CreateTagCategory creates a tag category.
func (c *Client) CreateTagCategory(ctx context.Context, f CategoryFields) (*TagCategory, error) {
	return send[*TagCategory](ctx, c, http.MethodPost, "tag-categories", nil, f)
}

// UpdateTagCategory changes a tag category.
func (c *Client) UpdateTagCategory(ctx context.Context, name string, f CategoryFields) (*TagCategory, error) {
	return send[*TagCategory](ctx, c, http.MethodPut, "tag-category/"+url.PathEscape(name), nil, f)
}

// SetDefaultTagCategory makes a category the one new tags get.
func (c *Client) SetDefaultTagCategory(ctx context.Context, name string) (*TagCategory, error) {
	return send[*TagCategory](ctx, c, http.MethodPut, "tag-category/"+url.PathEscape(name)+"/default", nil, struct{}{})
}

// DeleteTagCategory deletes a tag category, whose tags go to the default
// one.
func (c *Client) DeleteTagCategory(ctx context.Context, name, version string) error {
	return c.do(ctx, http.MethodDelete, "tag-category/"+url.PathEscape(name), nil, versioned{version}, nil)
}

// Tags.

// Tags searches for tags.
func (c *Client) Tags(ctx context.Context, s Search) (*Page[Tag], error) {
	return get[*Page[Tag]](ctx, c, "tags/", s.values())
}

// Tag returns a tag by any of its names.
func (c *Client) Tag(ctx context.Context, name string) (*Tag, error) {
	return get[*Tag](ctx, c, "tag/"+url.PathEscape(name), nil)
}

// TagSiblings returns the tags most used together with a tag.
func (c *Client) TagSiblings(ctx context.Context, name string) ([]TagSibling, error) {
	r, err := get[results[TagSibling]](ctx, c, "tag-siblings/"+url.PathEscape(name), nil)
	return r.Results, err
}

// TagFields is what creating or updating a tag sets.
type TagFields struct {
	Version      string    `json:"version,omitzero"`
	Names        *[]string `json:"names,omitzero"`
	Category     *string   `json:"category,omitzero"`
	Description  *string   `json:"description,omitzero"`
	Implications *[]string `json:"implications,omitzero"`
	Suggestions  *[]string `json:"suggestions,omitzero"`
}

// CreateTag creates a tag.
func (c *Client) CreateTag(ctx context.Context, f TagFields) (*Tag, error) {
	return send[*Tag](ctx, c, http.MethodPost, "tags", nil, f)
}

// UpdateTag changes a tag.
func (c *Client) UpdateTag(ctx context.Context, name string, f TagFields) (*Tag, error) {
	return send[*Tag](ctx, c, http.MethodPut, "tag/"+url.PathEscape(name), nil, f)
}

// DeleteTag deletes a tag, which no post may use.
func (c *Client) DeleteTag(ctx context.Context, name, version string) error {
	return c.do(ctx, http.MethodDelete, "tag/"+url.PathEscape(name), nil, versioned{version}, nil)
}

// MergeTags merges the tag remove into mergeTo.
func (c *Client) MergeTags(ctx context.Context, remove, mergeTo *Tag) (*Tag, error) {
	body := map[string]string{
		"remove": remove.Name(), "removeVersion": remove.Version,
		"mergeTo": mergeTo.Name(), "mergeToVersion": mergeTo.Version,
	}
	return send[*Tag](ctx, c, http.MethodPost, "tag-merge/", nil, body)
}

// Pool categories.

// PoolCategories lists the pool categories.
func (c *Client) PoolCategories(ctx context.Context) ([]PoolCategory, error) {
	r, err := get[results[PoolCategory]](ctx, c, "pool-categories", nil)
	return r.Results, err
}

// CreatePoolCategory creates a pool category.
func (c *Client) CreatePoolCategory(ctx context.Context, f CategoryFields) (*PoolCategory, error) {
	f.Order = nil
	return send[*PoolCategory](ctx, c, http.MethodPost, "pool-categories", nil, f)
}

// UpdatePoolCategory changes a pool category.
func (c *Client) UpdatePoolCategory(ctx context.Context, name string, f CategoryFields) (*PoolCategory, error) {
	f.Order = nil
	return send[*PoolCategory](ctx, c, http.MethodPut, "pool-category/"+url.PathEscape(name), nil, f)
}

// SetDefaultPoolCategory makes a category the one new pools get.
func (c *Client) SetDefaultPoolCategory(ctx context.Context, name string) (*PoolCategory, error) {
	return send[*PoolCategory](ctx, c, http.MethodPut, "pool-category/"+url.PathEscape(name)+"/default", nil, struct{}{})
}

// DeletePoolCategory deletes a pool category.
func (c *Client) DeletePoolCategory(ctx context.Context, name, version string) error {
	return c.do(ctx, http.MethodDelete, "pool-category/"+url.PathEscape(name), nil, versioned{version}, nil)
}

// Pools.

// Pools searches for pools.
func (c *Client) Pools(ctx context.Context, s Search) (*Page[Pool], error) {
	return get[*Page[Pool]](ctx, c, "pools/", s.values())
}

// Pool returns a pool.
func (c *Client) Pool(ctx context.Context, poolID int64) (*Pool, error) {
	return get[*Pool](ctx, c, "pool/"+id(poolID), nil)
}

// PoolFields is what creating or updating a pool sets.
type PoolFields struct {
	Version     string    `json:"version,omitzero"`
	Names       *[]string `json:"names,omitzero"`
	Category    *string   `json:"category,omitzero"`
	Description *string   `json:"description,omitzero"`
	Posts       *[]int64  `json:"posts,omitzero"`
}

// CreatePool creates a pool.
func (c *Client) CreatePool(ctx context.Context, f PoolFields) (*Pool, error) {
	return send[*Pool](ctx, c, http.MethodPost, "pool", nil, f)
}

// UpdatePool changes a pool.
func (c *Client) UpdatePool(ctx context.Context, poolID int64, f PoolFields) (*Pool, error) {
	return send[*Pool](ctx, c, http.MethodPut, "pool/"+id(poolID), nil, f)
}

// DeletePool deletes a pool, keeping its posts.
func (c *Client) DeletePool(ctx context.Context, poolID int64, version string) error {
	return c.do(ctx, http.MethodDelete, "pool/"+id(poolID), nil, versioned{version}, nil)
}

// MergePools merges the pool remove into mergeTo.
func (c *Client) MergePools(ctx context.Context, remove, mergeTo *Pool) (*Pool, error) {
	body := map[string]any{
		"remove": remove.ID, "removeVersion": remove.Version,
		"mergeTo": mergeTo.ID, "mergeToVersion": mergeTo.Version,
	}
	return send[*Pool](ctx, c, http.MethodPost, "pool-merge/", nil, body)
}

// Comments.

// Comments searches for comments.
func (c *Client) Comments(ctx context.Context, s Search) (*Page[Comment], error) {
	return get[*Page[Comment]](ctx, c, "comments/", s.values())
}

// CreateComment comments on a post.
func (c *Client) CreateComment(ctx context.Context, postID int64, text string) (*Comment, error) {
	return send[*Comment](ctx, c, http.MethodPost, "comments/", nil, map[string]any{"postId": postID, "text": text})
}

// UpdateComment changes a comment's text.
func (c *Client) UpdateComment(ctx context.Context, cm *Comment, text string) (*Comment, error) {
	return send[*Comment](ctx, c, http.MethodPut, "comment/"+id(cm.ID), nil, map[string]string{"version": cm.Version, "text": text})
}

// RateComment sets the user's score of a comment: -1, 0 or 1.
func (c *Client) RateComment(ctx context.Context, commentID int64, score int) (*Comment, error) {
	return send[*Comment](ctx, c, http.MethodPut, "comment/"+id(commentID)+"/score", nil, map[string]int{"score": score})
}

// DeleteComment deletes a comment.
func (c *Client) DeleteComment(ctx context.Context, cm *Comment) error {
	return c.do(ctx, http.MethodDelete, "comment/"+id(cm.ID), nil, versioned{cm.Version}, nil)
}

// Users.

// Users searches for users.
func (c *Client) Users(ctx context.Context, s Search) (*Page[User], error) {
	return get[*Page[User]](ctx, c, "users/", s.values())
}

// User returns a user. bumpLogin records the request as their login.
func (c *Client) User(ctx context.Context, name string, bumpLogin bool) (*User, error) {
	var q url.Values
	if bumpLogin {
		q = url.Values{"bump-login": {"true"}}
	}
	return get[*User](ctx, c, "user/"+url.PathEscape(name), q)
}

// UserFields is what creating or updating a user sets.
type UserFields struct {
	Version     string  `json:"version,omitzero"`
	Name        *string `json:"name,omitzero"`
	Password    *string `json:"password,omitzero"`
	Email       *string `json:"email,omitzero"`
	Rank        *Rank   `json:"rank,omitzero"`
	AvatarStyle *string `json:"avatarStyle,omitzero"`
	// AvatarToken is a temporary upload of the picture for the manual
	// avatar style.
	AvatarToken string `json:"avatarToken,omitzero"`
}

// CreateUser creates a user, as registering does.
func (c *Client) CreateUser(ctx context.Context, f UserFields) (*User, error) {
	return send[*User](ctx, c, http.MethodPost, "users", nil, f)
}

// UpdateUser changes a user.
func (c *Client) UpdateUser(ctx context.Context, name string, f UserFields) (*User, error) {
	return send[*User](ctx, c, http.MethodPut, "user/"+url.PathEscape(name), nil, f)
}

// DeleteUser deletes a user.
func (c *Client) DeleteUser(ctx context.Context, name, version string) error {
	return c.do(ctx, http.MethodDelete, "user/"+url.PathEscape(name), nil, versioned{version}, nil)
}

// User tokens.

// UserTokens lists a user's tokens.
func (c *Client) UserTokens(ctx context.Context, user string) ([]UserToken, error) {
	r, err := get[results[UserToken]](ctx, c, "user-tokens/"+url.PathEscape(user), nil)
	return r.Results, err
}

// TokenFields is what creating or updating a token sets.
type TokenFields struct {
	Version        string     `json:"version,omitzero"`
	Enabled        *bool      `json:"enabled,omitzero"`
	Note           *string    `json:"note,omitzero"`
	ExpirationTime *time.Time `json:"expirationTime,omitzero"`
}

// CreateUserToken creates a token for a user.
func (c *Client) CreateUserToken(ctx context.Context, user string, f TokenFields) (*UserToken, error) {
	return send[*UserToken](ctx, c, http.MethodPost, "user-token/"+url.PathEscape(user), nil, f)
}

// UpdateUserToken changes a user's token.
func (c *Client) UpdateUserToken(ctx context.Context, user, token string, f TokenFields) (*UserToken, error) {
	return send[*UserToken](ctx, c, http.MethodPut, "user-token/"+url.PathEscape(user)+"/"+url.PathEscape(token), nil, f)
}

// DeleteUserToken deletes a user's token.
func (c *Client) DeleteUserToken(ctx context.Context, user, token string) error {
	return c.do(ctx, http.MethodDelete, "user-token/"+url.PathEscape(user)+"/"+url.PathEscape(token), nil, struct{}{}, nil)
}

// Password reset.

// RequestPasswordReset mails a user a link to reset their password.
func (c *Client) RequestPasswordReset(ctx context.Context, nameOrEmail string) error {
	return c.do(ctx, http.MethodGet, "password-reset/"+url.PathEscape(nameOrEmail), nil, nil, nil)
}

// ConfirmPasswordReset returns a new password for a user, given the token
// of the link mailed to them.
func (c *Client) ConfirmPasswordReset(ctx context.Context, nameOrEmail, token string) (string, error) {
	r, err := send[struct {
		Password string `json:"password"`
	}](ctx, c, http.MethodPost, "password-reset/"+url.PathEscape(nameOrEmail), nil, map[string]string{"token": token})
	return r.Password, err
}

// Snapshots.

// Snapshots lists the recent changes of resources, newest first.
func (c *Client) Snapshots(ctx context.Context, s Search) (*Page[Snapshot], error) {
	return get[*Page[Snapshot]](ctx, c, "snapshots/", s.values())
}

// Privileges.

// RequiredRank returns the lowest rank granted a privilege, named as the
// server's configuration names it in any style ("post_edit_tag",
// "postEditTag", "posts:edit:tags"), the lowest among those it starts when
// it names a group ("post_edit"). ok is false for a privilege the server
// does not know.
func (cfg *Config) RequiredRank(privilege string) (rank Rank, ok bool) {
	want := normalizePrivilege(privilege)
	best := -1
	for name, r := range cfg.Privileges {
		if !strings.HasPrefix(normalizePrivilege(name), want) {
			continue
		}
		if l := r.Level(); best < 0 || l < best {
			best = l
		}
	}
	if best < 0 {
		return "", false
	}
	return Ranks[best], true
}

// Can reports whether a user of a rank has a privilege. Privileges the
// server does not know are granted, for the server to refuse.
func (cfg *Config) Can(rank Rank, privilege string) bool {
	need, ok := cfg.RequiredRank(privilege)
	if !ok {
		return true
	}
	return rank.Level() >= need.Level()
}

// normalizePrivilege writes a privilege's name in one style, its words
// singular and joined, so that Szurubooru's "posts:edit:tags" is
// Oxibooru's "post_edit_tag".
func normalizePrivilege(s string) string {
	words := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return r == '_' || r == ':' || r == '-' })
	for i, w := range words {
		switch {
		case strings.HasSuffix(w, "ies"):
			words[i] = w[:len(w)-3] + "y"
		case strings.HasSuffix(w, "ss"), strings.HasSuffix(w, "us"):
		case strings.HasSuffix(w, "s"):
			words[i] = w[:len(w)-1]
		}
	}
	return strings.Join(words, "")
}
