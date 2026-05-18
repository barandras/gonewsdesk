package custom

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/barandras/gonewsdesk/pkg/news"
)

const (
	initialBackoff = time.Second
	maxBackoff     = 30 * time.Second
)

type CustomNewsProvider struct {
	ctx         context.Context
	url         string
	newsChannel chan news.ExternalNews
	debug       bool
	seenNewsIDs map[string]struct{}
}

type NewCustomNewsProviderParams struct {
	Ctx   context.Context
	URL   string
	Debug bool
}

func NewCustomNewsProvider(params NewCustomNewsProviderParams) (*CustomNewsProvider, error) {
	url, err := normalizeStreamURL(params.URL)
	if err != nil {
		return nil, err
	}
	c := &CustomNewsProvider{
		ctx:         params.Ctx,
		url:         url,
		newsChannel: make(chan news.ExternalNews, 256),
		debug:       params.Debug,
		seenNewsIDs: make(map[string]struct{}),
	}
	go c.run()
	return c, nil
}

func normalizeStreamURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("custom news stream URL is required")
	}
	if !strings.HasPrefix(raw, "ws://") && !strings.HasPrefix(raw, "wss://") {
		return "", fmt.Errorf("custom news stream URL must use ws:// or wss:// scheme")
	}
	return raw, nil
}

func (c *CustomNewsProvider) GetNewsChannel() <-chan news.ExternalNews {
	return c.newsChannel
}

func (c *CustomNewsProvider) debugLogf(format string, args ...any) {
	if !c.debug {
		return
	}
	log.Printf("DEBUG: custom news provider: "+format, args...)
}

func (c *CustomNewsProvider) run() {
	defer close(c.newsChannel)

	backoff := initialBackoff
	for {
		if err := c.ctx.Err(); err != nil {
			return
		}

		err := c.connectOnce()
		if err != nil {
			log.Printf("custom news stream: %v; reconnecting in %s", err, backoff)
			select {
			case <-c.ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}
		backoff = initialBackoff
	}
}

func (c *CustomNewsProvider) connectOnce() error {
	conn, _, err := websocket.Dial(c.ctx, c.url, &websocket.DialOptions{
		CompressionMode: websocket.CompressionNoContextTakeover,
	})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	log.Printf("websocket connected to %s", c.url)
	defer conn.Close(websocket.StatusNormalClosure, "")

	return c.readLoop(c.ctx, conn)
}

func (c *CustomNewsProvider) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if typ != websocket.MessageText {
			continue
		}
		var ext news.ExternalNews
		if err := json.Unmarshal(data, &ext); err != nil {
			log.Printf("custom news: skip malformed frame: %v", err)
			continue
		}
		c.debugLogf("received news id=%q headline=%q", ext.ID, ext.Headline)
		if !c.pushNews(ctx, ext) {
			return ctx.Err()
		}
	}
}

func (c *CustomNewsProvider) pushNews(ctx context.Context, ext news.ExternalNews) bool {
	if ext.ID != "" {
		if _, seen := c.seenNewsIDs[ext.ID]; seen {
			return true
		}
		c.seenNewsIDs[ext.ID] = struct{}{}
	}
	select {
	case <-ctx.Done():
		return false
	case c.newsChannel <- ext:
		return true
	}
}
