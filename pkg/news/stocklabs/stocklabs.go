package stocklabs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/barandras/gonewsdesk/pkg/news"
)

const (
	stocklabsNewsStreamURL = "wss://ws.stocklabs.com/"

	initialBackoff = time.Second
	maxBackoff     = 30 * time.Second

	pingResponseJSON = `{"t":"ping","d":{}}`
)

type StocklabsNewsProvider struct {
	ctx         context.Context
	newsChannel chan news.ExternalNews
	debug       bool
}

type NewStocklabsNewsProviderParams struct {
	Ctx   context.Context
	Debug bool
}

func NewStocklabsNewsProvider(params NewStocklabsNewsProviderParams) *StocklabsNewsProvider {
	s := &StocklabsNewsProvider{
		ctx:         params.Ctx,
		newsChannel: make(chan news.ExternalNews, 256),
		debug:       params.Debug,
	}
	go s.run()
	return s
}

func (s *StocklabsNewsProvider) GetNewsChannel() <-chan news.ExternalNews {
	return s.newsChannel
}

func (s *StocklabsNewsProvider) debugLogf(format string, args ...any) {
	if !s.debug {
		return
	}
	log.Printf("DEBUG: stocklabs news provider: "+format, args...)
}

func (s *StocklabsNewsProvider) run() {
	defer close(s.newsChannel)

	backoff := initialBackoff
	for {
		if err := s.ctx.Err(); err != nil {
			return
		}

		err := s.connectOnce()
		if err != nil {
			log.Printf("stocklabs news stream: %v; reconnecting in %s", err, backoff)
			select {
			case <-s.ctx.Done():
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

func (s *StocklabsNewsProvider) connectOnce() error {
	conn, _, err := websocket.Dial(s.ctx, stocklabsNewsStreamURL, &websocket.DialOptions{
		CompressionMode: websocket.CompressionNoContextTakeover,
	})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	log.Printf("websocket connected to %s", stocklabsNewsStreamURL)
	defer conn.Close(websocket.StatusNormalClosure, "")

	return s.readLoop(s.ctx, conn)
}

func (s *StocklabsNewsProvider) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		for _, raw := range splitJSONMessages(data) {
			if len(raw) == 0 {
				continue
			}
			var env struct {
				T string          `json:"t"`
				D json.RawMessage `json:"d"`
			}
			if err := json.Unmarshal(raw, &env); err != nil {
				log.Printf("stocklabs news: skip malformed frame: %v", err)
				continue
			}
			switch env.T {
			case "ping":
				if err := conn.Write(ctx, websocket.MessageText, []byte(pingResponseJSON)); err != nil {
					return fmt.Errorf("ping response: %w", err)
				}
				s.debugLogf("sent ping ack")
			case "data_update":
				ext, err := externalNewsFromDataUpdate(env.D)
				if err != nil {
					s.debugLogf("skip data_update: %v", err)
					continue
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case s.newsChannel <- ext:
				}
			default:
				s.debugLogf("ignored message type %q: %s", env.T, string(raw))
			}
		}
	}
}

func splitJSONMessages(data []byte) [][]byte {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil
	}
	if data[0] == '[' {
		var msgs []json.RawMessage
		if err := json.Unmarshal(data, &msgs); err != nil {
			return nil
		}
		out := make([][]byte, 0, len(msgs))
		for _, m := range msgs {
			out = append(out, []byte(m))
		}
		return out
	}
	return [][]byte{data}
}

type dataUpdateEnvelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type stocklabsNewsPayload struct {
	Medium      string `json:"medium"`
	Link        string `json:"link"`
	Text        string `json:"text"`
	CreatedTime int64  `json:"created_time"`
	Profile     struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"profile"`
}

func externalNewsFromDataUpdate(d json.RawMessage) (news.ExternalNews, error) {
	var wrap dataUpdateEnvelope
	if err := json.Unmarshal(d, &wrap); err != nil {
		return news.ExternalNews{}, fmt.Errorf("decode data_update envelope: %w", err)
	}
	if wrap.Type != "news" {
		return news.ExternalNews{}, fmt.Errorf("not news type: %q", wrap.Type)
	}
	var p stocklabsNewsPayload
	if err := json.Unmarshal(wrap.Data, &p); err != nil {
		return news.ExternalNews{}, fmt.Errorf("decode news payload: %w", err)
	}
	if p.Text == "" && p.Link == "" {
		return news.ExternalNews{}, fmt.Errorf("empty news item")
	}

	var id string
	if p.Link == "" {
		id = fmt.Sprintf("stocklabs:%s:%d", p.Profile.ID, p.CreatedTime)
	} else {
		var err error
		id, err = xStatusIDFromURL(p.Link)
		if err != nil {
			return news.ExternalNews{}, fmt.Errorf("news id from link: %w", err)
		}
	}

	ts := time.Unix(p.CreatedTime, 0)
	if p.CreatedTime <= 0 {
		ts = time.Now().UTC()
	}

	author := p.Profile.Name
	if p.Profile.Username != "" {
		author = "@" + p.Profile.Username
	}

	source := p.Medium
	if source == "" {
		source = "X"
	}

	return news.ExternalNews{
		ID:               id,
		Headline:         p.Text,
		Author:           author,
		Source:           source,
		Url:              p.Link,
		SymbolsMentioned: nil,
		Timestamp:        ts,
	}, nil
}

// xStatusIDFromURL returns the status ID from an X/Twitter status URL (path .../status/<id>).
// link must be non-empty; any parse failure or unexpected shape returns an error.
func xStatusIDFromURL(link string) (string, error) {
	u, err := url.Parse(link)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, "x.com") && !strings.HasSuffix(host, "twitter.com") {
		return "", fmt.Errorf("expected x.com or twitter.com status URL, got host %q", host)
	}
	path := strings.Trim(u.Path, "/")
	parts := strings.Split(path, "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] != "status" {
			continue
		}
		id := parts[i+1]
		if j := strings.IndexAny(id, "?#"); j >= 0 {
			id = id[:j]
		}
		if id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("no /status/<id> in path %q", u.Path)
}
