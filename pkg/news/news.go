package news

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

const mergedNewsChanSize = 256

type NewsProvider interface {
	GetNewsChannel() <-chan ExternalNews
}

type ExternalNews struct {
	ID               string    `bson:"id"`
	Headline         string    `bson:"headline"`
	Content          string    `bson:"content"`
	Summary          string    `bson:"summary"`
	Author           string    `bson:"author"`
	Source           string    `bson:"source"`
	Url              string    `bson:"url"`
	SymbolsMentioned []string  `bson:"symbolsMentioned"`
	Timestamp        time.Time `bson:"ts"`
}

type NewsProcessor struct {
	ctx             context.Context
	newsProviders   []NewsProvider
	merged          chan ExternalNews // closed after all provider pumps exit
	excludeKeywords []string
	includeKeywords []string
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
					if !n.headlineAllows(item.Headline) {
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
