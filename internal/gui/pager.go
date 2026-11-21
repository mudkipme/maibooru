package gui

import (
	"context"

	"github.com/mudkipme/maibooru/internal/oxibooru"
)

// pager loads the results of a search a page at a time, as they scroll into
// view.
type pager[T any] struct {
	query   string
	items   []T
	total   int
	loading bool
	done    bool
	started bool
	err     error
	gen     int
	limit   int
	fetch   func(ctx context.Context, c *oxibooru.Client, s oxibooru.Search) (*oxibooru.Page[T], error)
}

// search starts the pager on a query, unless it shows that one already.
func (p *pager[T]) search(a *App, q string) {
	if p.started && p.query == q {
		return
	}
	p.query = q
	p.reload(a)
}

// reload loads the query's results again from the first.
func (p *pager[T]) reload(a *App) {
	p.started = true
	p.gen++
	p.items, p.total, p.done, p.loading, p.err = nil, 0, false, false, nil
	p.more(a)
}

// more loads the next page.
func (p *pager[T]) more(a *App) {
	if p.loading || p.done || p.fetch == nil {
		return
	}
	limit := p.limit
	if limit == 0 {
		limit = 50
	}
	p.loading, p.err = true, nil
	gen, s := p.gen, oxibooru.Search{Query: p.query, Offset: len(p.items), Limit: limit}
	async(a, func(ctx context.Context, c *oxibooru.Client) (*oxibooru.Page[T], error) {
		return p.fetch(ctx, c, s)
	}, func(page *oxibooru.Page[T], err error) {
		if gen != p.gen {
			return
		}
		p.loading = false
		if err != nil {
			p.err = err
			return
		}
		p.items = append(p.items, page.Results...)
		p.total = page.Total
		p.done = len(page.Results) == 0 || len(p.items) >= page.Total
	})
}

// near loads more once row i of the items shown nears their end.
func (p *pager[T]) near(a *App, i int) {
	if i >= len(p.items)-10 {
		p.more(a)
	}
}
