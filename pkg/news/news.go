package news

import (
	"context"
	"fmt"
	"html"
	"strings"
	"sync"
	"time"
)

const (
	mergedNewsChanSize = 256
	maxSeenNewsIDs     = 100
)

type NewsProvider interface {
	GetNewsChannel() <-chan ExternalNews
}

type ExternalNews struct {
	ID                string    `bson:"id" json:"id"`
	Headline          string    `bson:"headline" json:"headline"`
	Content           string    `bson:"content" json:"content"`
	Summary           string    `bson:"summary" json:"summary"`
	Author            string    `bson:"author" json:"author"`
	Source            string    `bson:"source" json:"source"`
	Url               string    `bson:"url" json:"url"`
	SymbolsMentioned  []string  `bson:"symbolsMentioned" json:"symbolsMentioned"`
	Timestamp         time.Time `bson:"ts" json:"ts"`
	SignificanceScore *uint     `bson:"significanceScore,omitempty" json:"significanceScore,omitempty"`
}

type NewsProcessor struct {
	ctx             context.Context
	newsProviders   []NewsProvider
	merged          chan ExternalNews // closed after all provider pumps exit
	excludeKeywords []string
	includeKeywords []string
	seenMu          sync.Mutex
	seenIDs         []string
}

type NewNewsProcessorParams struct {
	Ctx context.Context
	// NewsProviders feeds the merge; each item is forwarded to Stream only if it passes headline filters.
	NewsProviders []NewsProvider
	// ExcludeKeywords: if any keyword matches the headline (case-insensitive substring), the item is dropped.
	ExcludeKeywords []string
	// IncludeKeywords: if non-empty, at least one keyword must match the headline or the item is dropped.
	IncludeKeywords []string
}

func NewNewsProcessor(params NewNewsProcessorParams) (*NewsProcessor, error) {
	if len(params.NewsProviders) == 0 {
		return nil, fmt.Errorf("at least one news provider is required")
	}

	np := &NewsProcessor{
		ctx:             params.Ctx,
		newsProviders:   params.NewsProviders,
		excludeKeywords: append([]string(nil), params.ExcludeKeywords...),
		includeKeywords: append([]string(nil), params.IncludeKeywords...),
	}
	np.start()
	return np, nil
}

// Stream returns the receive side of the merged, headline-filtered stream.
// The channel is closed when every provider channel has closed and merge
// goroutines have exited. Callers should also cancel the processor context
// to stop work promptly.
func (n *NewsProcessor) Stream() <-chan ExternalNews {
	return n.merged
}

func (n *NewsProcessor) start() {
	n.merged = make(chan ExternalNews, mergedNewsChanSize)
	merged := n.merged
	var wg sync.WaitGroup
	for _, p := range n.newsProviders {
		wg.Add(1)
		go func(ch <-chan ExternalNews) {
			defer wg.Done()
			for {
				select {
				case <-n.ctx.Done():
					return
				case item, ok := <-ch:
					if !ok {
						return
					}
					item = decodeHTMLEntitiesIfNeeded(item)
					if !n.headlineAllows(item.Headline) {
						continue
					}
					if !n.recordIfNew(item.ID) {
						continue
					}
					select {
					case merged <- item:
					case <-n.ctx.Done():
						return
					}
				}
			}
		}(p.GetNewsChannel())
	}
	go func() {
		wg.Wait()
		close(merged)
	}()
}

// recordIfNew returns false if id was already seen. Empty ids are always accepted.
func (n *NewsProcessor) recordIfNew(id string) bool {
	if id == "" {
		return true
	}
	n.seenMu.Lock()
	defer n.seenMu.Unlock()
	for _, seen := range n.seenIDs {
		if seen == id {
			return false
		}
	}
	if len(n.seenIDs) >= maxSeenNewsIDs {
		n.seenIDs = n.seenIDs[1:]
	}
	n.seenIDs = append(n.seenIDs, id)
	return true
}

func (n *NewsProcessor) headlineAllows(headline string) bool {
	h := strings.ToLower(strings.TrimSpace(headline))
	for _, kw := range n.excludeKeywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw == "" {
			continue
		}
		if strings.Contains(h, kw) {
			return false
		}
	}
	if len(n.includeKeywords) == 0 {
		return true
	}
	for _, kw := range n.includeKeywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw == "" {
			continue
		}
		if strings.Contains(h, kw) {
			return true
		}
	}
	return false
}

func decodeHTMLEntitiesIfNeeded(item ExternalNews) ExternalNews {
	item.Headline = unescapeIfEntityLike(item.Headline)
	item.Summary = unescapeIfEntityLike(item.Summary)
	item.Content = unescapeIfEntityLike(item.Content)
	item.Author = unescapeIfEntityLike(item.Author)
	item.Source = unescapeIfEntityLike(item.Source)
	return item
}

func unescapeIfEntityLike(s string) string {
	if !strings.Contains(s, "&") || !strings.Contains(s, ";") {
		return s
	}
	return html.UnescapeString(s)
}
