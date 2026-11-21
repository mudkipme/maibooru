package oxibooru

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// Progress reports the bytes of a transfer done so far, of total (-1 when
// unknown). It is called from the goroutine doing the transfer.
type Progress func(done, total int64)

// Upload puts content into the server's temporary storage and returns the
// token that refers to it, as PostFields.ContentToken, for a short while.
func (c *Client) Upload(ctx context.Context, name string, content io.Reader, size int64, progress Progress) (string, error) {
	boundary := randomBoundary()
	head := &bytes.Buffer{}
	fmt.Fprintf(head, "--%s\r\n", boundary)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="content"; filename="%s"`, escapeQuotes(filepath.Base(name))))
	ctype := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	h.Set("Content-Type", ctype)
	for k, vs := range h {
		for _, v := range vs {
			fmt.Fprintf(head, "%s: %s\r\n", k, v)
		}
	}
	head.WriteString("\r\n")
	tail := "\r\n--" + boundary + "--\r\n"

	total := int64(-1)
	if size >= 0 {
		total = int64(head.Len()) + size + int64(len(tail))
	}
	body := io.MultiReader(head, content, strings.NewReader(tail))
	var r io.Reader = body
	if progress != nil {
		r = &progressReader{r: body, total: total, progress: progress}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("uploads", nil), r)
	if err != nil {
		return "", err
	}
	req.ContentLength = total
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	var out struct {
		Token string `json:"token"`
	}
	if err := c.send(req, &out); err != nil {
		return "", err
	}
	return out.Token, nil
}

func randomBoundary() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "maibooru" + hex.EncodeToString(b[:])
}

func escapeQuotes(s string) string {
	return strings.NewReplacer("\\", "\\\\", `"`, "\\\"", "\r", "", "\n", "").Replace(s)
}

type progressReader struct {
	r        io.Reader
	done     atomic.Int64
	total    int64
	progress Progress
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.progress(p.done.Add(int64(n)), p.total)
	}
	return n, err
}

// Download writes what a data URL of a resource, as a post's contentUrl,
// points to into w.
func (c *Client) Download(ctx context.Context, ref string, w io.Writer, progress Progress) error {
	u := c.DataURL(ref)
	if u == "" {
		return fmt.Errorf("no URL to download")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if h := c.Credentials().header(); h != "" {
		req.Header.Set("Authorization", h)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return &Error{Status: res.StatusCode}
	}
	var r io.Reader = res.Body
	if progress != nil {
		r = &progressReader{r: res.Body, total: res.ContentLength, progress: progress}
	}
	_, err = io.Copy(w, r)
	return err
}
