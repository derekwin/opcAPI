package redis

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr         string
	Password     string
	DB           int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

type Client struct {
	cfg Config
}

func NewClient(cfg Config) *Client {
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:6379"
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 2 * time.Second
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = 2 * time.Second
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = 2 * time.Second
	}
	return &Client{cfg: cfg}
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.exec(ctx, []string{"PING"})
	return err
}

func (c *Client) Get(ctx context.Context, key string) (string, bool, error) {
	resp, err := c.exec(ctx, []string{"GET", key})
	if err != nil {
		return "", false, err
	}
	if resp == nil {
		return "", false, nil
	}
	v, ok := resp.(string)
	if !ok {
		return "", false, errors.New("redis GET returned non-string value")
	}
	return v, true, nil
}

func (c *Client) GetInt64(ctx context.Context, key string) (int64, bool, error) {
	v, ok, err := c.Get(ctx, key)
	if err != nil || !ok {
		return 0, ok, err
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false, err
	}
	return n, true, nil
}

func (c *Client) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	args := []string{"SET", key, value}
	if ttl > 0 {
		seconds := int(ttl / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		args = append(args, "EX", strconv.Itoa(seconds))
	}
	_, err := c.exec(ctx, args)
	return err
}

func (c *Client) Incr(ctx context.Context, key string) (int64, error) {
	resp, err := c.exec(ctx, []string{"INCR", key})
	if err != nil {
		return 0, err
	}
	v, ok := resp.(int64)
	if !ok {
		return 0, errors.New("redis INCR returned non-integer value")
	}
	return v, nil
}

func (c *Client) IncrBy(ctx context.Context, key string, delta int64) (int64, error) {
	resp, err := c.exec(ctx, []string{"INCRBY", key, strconv.FormatInt(delta, 10)})
	if err != nil {
		return 0, err
	}
	v, ok := resp.(int64)
	if !ok {
		return 0, errors.New("redis INCRBY returned non-integer value")
	}
	return v, nil
}

func (c *Client) Expire(ctx context.Context, key string, ttl time.Duration) error {
	seconds := int(ttl / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	_, err := c.exec(ctx, []string{"EXPIRE", key, strconv.Itoa(seconds)})
	return err
}

func (c *Client) exec(ctx context.Context, args []string) (any, error) {
	if c == nil {
		return nil, errors.New("redis client is nil")
	}

	dialer := net.Dialer{Timeout: c.cfg.DialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", c.cfg.Addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(c.cfg.ReadTimeout))
	_ = conn.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout))

	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))

	if c.cfg.Password != "" {
		if _, err := doCommand(rw, []string{"AUTH", c.cfg.Password}); err != nil {
			return nil, err
		}
	}
	if c.cfg.DB > 0 {
		if _, err := doCommand(rw, []string{"SELECT", strconv.Itoa(c.cfg.DB)}); err != nil {
			return nil, err
		}
	}

	return doCommand(rw, args)
}

func doCommand(rw *bufio.ReadWriter, args []string) (any, error) {
	if err := writeRESPArray(rw, args); err != nil {
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		return nil, err
	}
	return readRESP(rw.Reader)
}

func writeRESPArray(w *bufio.ReadWriter, args []string) error {
	if _, err := fmt.Fprintf(w, "*%d\r\n", len(args)); err != nil {
		return err
	}
	for _, arg := range args {
		if _, err := fmt.Fprintf(w, "$%d\r\n%s\r\n", len(arg), arg); err != nil {
			return err
		}
	}
	return nil
}

func readRESP(r *bufio.Reader) (any, error) {
	prefix, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	switch prefix {
	case '+':
		line, err := readLine(r)
		if err != nil {
			return nil, err
		}
		return line, nil
	case '-':
		line, err := readLine(r)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("redis error: %s", line)
	case ':':
		line, err := readLine(r)
		if err != nil {
			return nil, err
		}
		n, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return nil, err
		}
		return n, nil
	case '$':
		line, err := readLine(r)
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, nil
		}
		buf := make([]byte, n+2)
		if _, err := r.Read(buf); err != nil {
			return nil, err
		}
		return string(buf[:n]), nil
	case '*':
		line, err := readLine(r)
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, nil
		}
		out := make([]any, 0, n)
		for i := 0; i < n; i++ {
			v, err := readRESP(r)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported redis response type: %q", prefix)
	}
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}
