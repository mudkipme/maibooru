package oxibooru

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strconv"
	"time"
)

// Safety is how safe for work a post is.
type Safety string

const (
	Safe    Safety = "safe"
	Sketchy Safety = "sketchy"
	Unsafe  Safety = "unsafe"
)

// Safeties lists the safeties from the safest.
var Safeties = []Safety{Safe, Sketchy, Unsafe}

// PostType is the kind of a post's content.
type PostType string

const (
	TypeImage     PostType = "image"
	TypeAnimation PostType = "animation"
	TypeVideo     PostType = "video"
	TypeFlash     PostType = "flash"
	TypeDocument  PostType = "document"
)

// Flags of posts.
const (
	FlagLoop  = "loop"
	FlagSound = "sound"
)

// Rank is a user's rank, which grants them privileges.
type Rank string

const (
	Anonymous     Rank = "anonymous"
	Restricted    Rank = "restricted"
	Regular       Rank = "regular"
	Power         Rank = "power"
	Moderator     Rank = "moderator"
	Administrator Rank = "administrator"
	Nobody        Rank = "nobody"
)

// Ranks lists the ranks from the lowest.
var Ranks = []Rank{Anonymous, Restricted, Regular, Power, Moderator, Administrator, Nobody}

// Level returns the place of the rank among Ranks, -1 when unknown.
func (r Rank) Level() int {
	for i, x := range Ranks {
		if x == r {
			return i
		}
	}
	return -1
}

// Version is a resource's version, which changes to it send back as the
// server gave it: a time on Oxibooru, a number on Szurubooru.
type Version string

func (v *Version) UnmarshalJSON(b []byte) error {
	switch {
	case string(b) == "null":
		*v = ""
	case len(b) > 0 && b[0] == '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*v = Version(s)
	default:
		if _, err := strconv.ParseFloat(string(b), 64); err != nil {
			return fmt.Errorf("version %s is neither a string nor a number", b)
		}
		*v = Version(b)
	}
	return nil
}

func (v Version) MarshalJSON() ([]byte, error) {
	if v == "" {
		return []byte("null"), nil
	}
	if _, err := strconv.ParseInt(string(v), 10, 64); err == nil {
		return []byte(v), nil
	}
	return json.Marshal(string(v))
}

// Private is a value the server hides from some users, sending false.
type Private[T any] struct {
	Value  T
	Hidden bool
}

func (p *Private[T]) UnmarshalJSON(b []byte) error {
	if string(b) == "false" {
		*p = Private[T]{Hidden: true}
		return nil
	}
	*p = Private[T]{}
	if string(b) == "null" {
		return nil
	}
	return json.Unmarshal(b, &p.Value)
}

func (p Private[T]) MarshalJSON() ([]byte, error) {
	if p.Hidden {
		return []byte("false"), nil
	}
	return json.Marshal(p.Value)
}

// MicroUser is a user, as other resources refer to them.
type MicroUser struct {
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
}

// User is an account of the server.
type User struct {
	Version           Version          `json:"version"`
	Name              string           `json:"name"`
	Email             Private[*string] `json:"email"`
	Rank              Rank             `json:"rank"`
	LastLoginTime     time.Time        `json:"lastLoginTime"`
	CreationTime      time.Time        `json:"creationTime"`
	AvatarStyle       string           `json:"avatarStyle"`
	AvatarURL         string           `json:"avatarUrl"`
	CommentCount      int              `json:"commentCount"`
	UploadedPostCount int              `json:"uploadedPostCount"`
	LikedPostCount    Private[int]     `json:"likedPostCount"`
	DislikedPostCount Private[int]     `json:"dislikedPostCount"`
	FavoritePostCount int              `json:"favoritePostCount"`
}

// UserToken is a token that authenticates a user in place of their
// password.
type UserToken struct {
	User           MicroUser  `json:"user"`
	Token          string     `json:"token"`
	Note           string     `json:"note"`
	Enabled        bool       `json:"enabled"`
	ExpirationTime *time.Time `json:"expirationTime"`
	Version        Version    `json:"version"`
	CreationTime   time.Time  `json:"creationTime"`
	LastEditTime   time.Time  `json:"lastEditTime"`
	LastUsageTime  time.Time  `json:"lastUsageTime"`
}

// TagCategory groups tags, as characters or artists.
type TagCategory struct {
	Version Version `json:"version"`
	Name    string  `json:"name"`
	Color   string  `json:"color"`
	Usages  int     `json:"usages"`
	Order   int     `json:"order"`
	Default bool    `json:"default"`
}

// MicroTag is a tag, as posts refer to it.
type MicroTag struct {
	Names    []string `json:"names"`
	Category string   `json:"category"`
	Usages   int      `json:"usages"`
}

// Name returns the tag's first name, which posts are tagged with.
func (t MicroTag) Name() string {
	if len(t.Names) == 0 {
		return ""
	}
	return t.Names[0]
}

// Tag is a tag of posts.
type Tag struct {
	Version      Version    `json:"version"`
	Names        []string   `json:"names"`
	Category     string     `json:"category"`
	Implications []MicroTag `json:"implications"`
	Suggestions  []MicroTag `json:"suggestions"`
	CreationTime time.Time  `json:"creationTime"`
	LastEditTime time.Time  `json:"lastEditTime"`
	Usages       int        `json:"usages"`
	Description  string     `json:"description"`
}

// Name returns the tag's first name.
func (t Tag) Name() string {
	if len(t.Names) == 0 {
		return ""
	}
	return t.Names[0]
}

// Micro returns the tag as posts refer to it.
func (t Tag) Micro() MicroTag {
	return MicroTag{Names: t.Names, Category: t.Category, Usages: t.Usages}
}

// TagSibling is a tag used together with another, and how often.
type TagSibling struct {
	Tag         Tag `json:"tag"`
	Occurrences int `json:"occurrences"`
}

// MicroPost is a post, as other resources refer to it.
type MicroPost struct {
	ID           int64  `json:"id"`
	ThumbnailURL string `json:"thumbnailUrl"`
}

// Point is a point of a note's polygon, each coordinate from 0 to 1 of the
// post's size.
type Point [2]float64

// Note is a text annotation drawn over a post.
type Note struct {
	Polygon []Point `json:"polygon"`
	Text    string  `json:"text"`
}

// Post is a file with its metadata.
type Post struct {
	Version            Version     `json:"version"`
	ID                 int64       `json:"id"`
	CreationTime       time.Time   `json:"creationTime"`
	LastEditTime       time.Time   `json:"lastEditTime"`
	Safety             Safety      `json:"safety"`
	Source             string      `json:"source"`
	Description        string      `json:"description"`
	Type               PostType    `json:"type"`
	Checksum           string      `json:"checksum"`
	ChecksumMD5        string      `json:"checksumMD5"`
	FileSize           int64       `json:"fileSize"`
	CanvasWidth        int         `json:"canvasWidth"`
	CanvasHeight       int         `json:"canvasHeight"`
	ContentURL         string      `json:"contentUrl"`
	ThumbnailURL       string      `json:"thumbnailUrl"`
	Flags              []string    `json:"flags"`
	Tags               []MicroTag  `json:"tags"`
	Relations          []MicroPost `json:"relations"`
	Notes              []Note      `json:"notes"`
	User               *MicroUser  `json:"user"`
	Score              int         `json:"score"`
	OwnScore           int         `json:"ownScore"`
	OwnFavorite        bool        `json:"ownFavorite"`
	TagCount           int         `json:"tagCount"`
	FavoriteCount      int         `json:"favoriteCount"`
	CommentCount       int         `json:"commentCount"`
	NoteCount          int         `json:"noteCount"`
	FeatureCount       int         `json:"featureCount"`
	RelationCount      int         `json:"relationCount"`
	LastFeatureTime    *time.Time  `json:"lastFeatureTime"`
	FavoritedBy        []MicroUser `json:"favoritedBy"`
	HasCustomThumbnail bool        `json:"hasCustomThumbnail"`
	MimeType           string      `json:"mimeType"`
	Comments           []Comment   `json:"comments"`
	Pools              []MicroPool `json:"pools"`
}

// HasFlag reports whether the post has a flag.
func (p *Post) HasFlag(flag string) bool {
	return slices.Contains(p.Flags, flag)
}

// TagNames returns the first names of the post's tags.
func (p *Post) TagNames() []string {
	names := make([]string, 0, len(p.Tags))
	for _, t := range p.Tags {
		names = append(names, t.Name())
	}
	return names
}

// RelationIDs returns the IDs of the post's relations.
func (p *Post) RelationIDs() []int64 {
	ids := make([]int64, 0, len(p.Relations))
	for _, r := range p.Relations {
		ids = append(ids, r.ID)
	}
	return ids
}

// PoolCategory groups pools.
type PoolCategory struct {
	Version Version `json:"version"`
	Name    string  `json:"name"`
	Color   string  `json:"color"`
	Usages  int     `json:"usages"`
	Default bool    `json:"default"`
}

// MicroPool is a pool, as posts refer to it.
type MicroPool struct {
	ID          int64    `json:"id"`
	Names       []string `json:"names"`
	Category    string   `json:"category"`
	Description string   `json:"description"`
	PostCount   int      `json:"postCount"`
}

// Name returns the pool's first name.
func (p MicroPool) Name() string {
	if len(p.Names) == 0 {
		return ""
	}
	return p.Names[0]
}

// Pool is an ordered list of posts.
type Pool struct {
	Version      Version     `json:"version"`
	ID           int64       `json:"id"`
	Names        []string    `json:"names"`
	Category     string      `json:"category"`
	Posts        []MicroPost `json:"posts"`
	CreationTime time.Time   `json:"creationTime"`
	LastEditTime time.Time   `json:"lastEditTime"`
	PostCount    int         `json:"postCount"`
	Description  string      `json:"description"`
}

// Name returns the pool's first name.
func (p Pool) Name() string {
	if len(p.Names) == 0 {
		return ""
	}
	return p.Names[0]
}

// PostIDs returns the IDs of the pool's posts, in order.
func (p Pool) PostIDs() []int64 {
	ids := make([]int64, 0, len(p.Posts))
	for _, post := range p.Posts {
		ids = append(ids, post.ID)
	}
	return ids
}

// Comment is a comment under a post.
type Comment struct {
	Version      Version    `json:"version"`
	ID           int64      `json:"id"`
	PostID       int64      `json:"postId"`
	User         *MicroUser `json:"user"`
	Text         string     `json:"text"`
	CreationTime time.Time  `json:"creationTime"`
	LastEditTime time.Time  `json:"lastEditTime"`
	Score        int        `json:"score"`
	OwnScore     int        `json:"ownScore"`
}

// Snapshot is a version of a resource: what was created, changed, deleted
// or merged.
type Snapshot struct {
	Operation string         `json:"operation"`
	Type      string         `json:"type"`
	ID        jsontext.Value `json:"id"`
	User      *MicroUser     `json:"user"`
	Data      jsontext.Value `json:"data"`
	Time      time.Time      `json:"time"`
}

// ResourceID returns the snapshot's resource ID as text.
func (s Snapshot) ResourceID() string {
	var v any
	if json.Unmarshal(s.ID, &v) != nil {
		return string(s.ID)
	}
	switch v := v.(type) {
	case string:
		return v
	case nil:
		return ""
	}
	return string(s.ID)
}

// Config is the public configuration of a server.
type Config struct {
	Name                 string          `json:"name"`
	UserNameRegex        string          `json:"userNameRegex"`
	PasswordRegex        string          `json:"passwordRegex"`
	TagNameRegex         string          `json:"tagNameRegex"`
	TagCategoryNameRegex string          `json:"tagCategoryNameRegex"`
	DefaultUserRank      Rank            `json:"defaultUserRank"`
	EnableSafety         bool            `json:"enableSafety"`
	ContactEmail         string          `json:"contactEmail"`
	CanSendMails         bool            `json:"canSendMails"`
	Privileges           map[string]Rank `json:"privileges"`
}

// Info is a server's statistics and public configuration.
type Info struct {
	PostCount     int       `json:"postCount"`
	DiskUsage     int64     `json:"diskUsage"`
	FeaturedPost  *Post     `json:"featuredPost"`
	FeaturingTime time.Time `json:"featuringTime"`
	FeaturingUser string    `json:"featuringUser"`
	ServerTime    time.Time `json:"serverTime"`
	Config        Config    `json:"config"`
}

// Page is a page of a search.
type Page[T any] struct {
	Query   string `json:"query"`
	Offset  int    `json:"offset"`
	Limit   int    `json:"limit"`
	Total   int    `json:"total"`
	Results []T    `json:"results"`
}

// results is the answer of a search without pages.
type results[T any] struct {
	Results []T `json:"results"`
}

// SimilarPost is a post that looks like an image searched for.
type SimilarPost struct {
	Distance float64 `json:"distance"`
	Post     Post    `json:"post"`
}

// ImageSearch is what a reverse image search found.
type ImageSearch struct {
	ExactPost    *Post         `json:"exactPost"`
	SimilarPosts []SimilarPost `json:"similarPosts"`
}
