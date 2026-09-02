package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type APIError struct {
	Status        int
	Code, Message string
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type Client struct {
	BaseURL, Token string
	HTTP           *http.Client
	// Refresh, when set, is called once after an unauthorized response to obtain a
	// fresh access token. Returning an error surfaces the original 401.
	Refresh func() (string, error)
}

func (c Client) Do(ctx context.Context, method, path string, in, out any) error {
	err := c.do(ctx, method, path, in, out)
	if c.Refresh == nil {
		return err
	}
	var api *APIError
	if !errors.As(err, &api) || api.Status != http.StatusUnauthorized {
		return err
	}
	token, refreshErr := c.Refresh()
	if refreshErr != nil {
		return refreshErr
	}
	c.Token = token
	c.Refresh = nil // One attempt only; a second 401 is a genuine failure.
	return c.do(ctx, method, path, in, out)
}

func (c Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, e := json.Marshal(in)
		if e != nil {
			return e
		}
		body = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if e != nil {
		return e
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	h := c.HTTP
	if h == nil {
		h = http.DefaultClient
	}
	res, e := h.Do(req)
	if e != nil {
		return fmt.Errorf("API unavailable: %w", e)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		var v struct{ Code, Error string }
		_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&v)
		if v.Error == "" {
			v.Error = http.StatusText(res.StatusCode)
		}
		return &APIError{res.StatusCode, v.Code, v.Error}
	}
	if out == nil || res.StatusCode == 204 {
		return nil
	}
	if e = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out); e != nil {
		return errors.New("invalid API response")
	}
	return nil
}
