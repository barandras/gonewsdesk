package alpaca

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/barandras/gonewsdesk/pkg/news"
)

const (
	alpacaNewsStreamURL = "wss://stream.data.alpaca.markets/v1beta1/news"

	initialBackoff = time.Second
	maxBackoff     = 30 * time.Second
)

type AlpacaCredentials struct {
	APIKeyID     string
	APIKeySecret string
}

type AlpacaNewsProvider struct {
	ctx               context.Context
	alpacaCredentials AlpacaCredentials
	newsChannel       chan news.ExternalNews
	debug             bool
}

type NewAlpacaNewsProviderParams struct {
	Ctx               context.Context
	AlpacaCredentials AlpacaCredentials
	Debug             bool
}

func NewAlpacaNewsProvider(params NewAlpacaNewsProviderParams) *AlpacaNewsProvider {
	a := &AlpacaNewsProvider{
		ctx:               params.Ctx,
		alpacaCredentials: params.AlpacaCredentials,
		newsChannel:       make(chan news.ExternalNews, 256),
		debug:             params.Debug,
	}
	go a.run()
	return a
}

func (a *AlpacaNewsProvider) GetNewsChannel() <-chan news.ExternalNews {
	return a.newsChannel
}

func (a *AlpacaNewsProvider) debugLogf(format string, args ...any) {
	if !a.debug {
		return
	}
	log.Printf("DEBUG: alpaca news provider: "+format, args...)
}

func (a *AlpacaNewsProvider) run() {
	defer close(a.newsChannel)

	backoff := initialBackoff
	for {
		if err := a.ctx.Err(); err != nil {
			return
		}

		err := a.connectOnce()
		if err != nil {
			log.Printf("alpaca news stream: %v; reconnecting in %s", err, backoff)
			select {
			case <-a.ctx.Done():
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

func (a *AlpacaNewsProvider) connectOnce() error {
	header := http.Header{}
	header.Set("APCA-API-KEY-ID", a.alpacaCredentials.APIKeyID)
	header.Set("APCA-API-SECRET-KEY", a.alpacaCredentials.APIKeySecret)

	conn, _, err := websocket.Dial(a.ctx, alpacaNewsStreamURL, &websocket.DialOptions{
		HTTPHeader:      header,
		CompressionMode: websocket.CompressionNoContextTakeover,
	})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	log.Printf("websocket connected to %s", alpacaNewsStreamURL)
	defer conn.Close(websocket.StatusNormalClosure, "")

	if err := a.handshakeAndSubscribe(a.ctx, conn); err != nil {
		return err
	}

	return a.readLoop(a.ctx, conn)
}

func (a *AlpacaNewsProvider) handshakeAndSubscribe(ctx context.Context, conn *websocket.Conn) error {
	for step := 0; step < 2; step++ {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("handshake read: %w", err)
		}
		msgs, err := parseMessageArray(data)
		if err != nil {
			return fmt.Errorf("handshake parse: %w", err)
		}
		for _, raw := range msgs {
			var ctrl struct {
				T   string `json:"T"`
				Msg string `json:"msg"`
			}
			if err := json.Unmarshal(raw, &ctrl); err != nil {
				return fmt.Errorf("handshake decode: %w", err)
			}
			if ctrl.T != "success" {
				return fmt.Errorf("unexpected handshake message: %s", string(raw))
			}
			a.debugLogf("handshake: %s", ctrl.Msg)
		}
	}

	sub := []byte(`{"action":"subscribe","news":["*"]}`)
	if err := conn.Write(ctx, websocket.MessageText, sub); err != nil {
		return fmt.Errorf("subscribe write: %w", err)
	}
	a.debugLogf("subscribe request sent (news=[*])")
	return nil
}

func (a *AlpacaNewsProvider) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		msgs, err := parseMessageArray(data)
		if err != nil {
			log.Printf("alpaca news: skip malformed frame: %v", err)
			continue
		}
		for _, raw := range msgs {
			var head struct {
				T string `json:"T"`
			}
			if err := json.Unmarshal(raw, &head); err != nil {
				continue
			}
			switch head.T {
			case "success":
				continue
			case "subscription":
				a.debugLogf("subscription confirmed: %s", string(raw))
				continue
			case "error":
				var e struct {
					Code int    `json:"code"`
					Msg  string `json:"msg"`
				}
				_ = json.Unmarshal(raw, &e)
				return fmt.Errorf("server error %d: %s", e.Code, e.Msg)
			case "n":
				ext, err := externalNewsFromAlpacaJSON(raw)
				if err != nil {
					log.Printf("alpaca news: drop item: %v", err)
					continue
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case a.newsChannel <- ext:
				}
			default:
				continue
			}
		}
	}
}

func parseMessageArray(data []byte) ([]json.RawMessage, error) {
	var msgs []json.RawMessage
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

type alpacaNewsJSON struct {
	T         string   `json:"T"`
	ID        int64    `json:"id"`
	Headline  string   `json:"headline"`
	Summary   string   `json:"summary"`
	Author    string   `json:"author"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	Content   string   `json:"content"`
	URL       string   `json:"url"`
	Symbols   []string `json:"symbols"`
	Source    string   `json:"source"`
}

func externalNewsFromAlpacaJSON(raw []byte) (news.ExternalNews, error) {
	var p alpacaNewsJSON
	if err := json.Unmarshal(raw, &p); err != nil {
		return news.ExternalNews{}, err
	}
	ts, err := time.Parse(time.RFC3339, p.CreatedAt)
	if err != nil {
		return news.ExternalNews{}, fmt.Errorf("created_at: %w", err)
	}
	return news.ExternalNews{
		ID:               strconv.FormatInt(p.ID, 10),
		Headline:         p.Headline,
		Content:          p.Content,
		Summary:          p.Summary,
		Author:           p.Author,
		Source:           p.Source,
		Url:              p.URL,
		SymbolsMentioned: append([]string(nil), p.Symbols...),
		Timestamp:        ts,
	}, nil
}
