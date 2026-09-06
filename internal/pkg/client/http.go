// Copyright 2026 Hanzo AI, Inc. All rights reserved.
// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	forge "github.com/hanzoai/git/modules/actions/runner"
)

// Conn is a live handle to one forge. Operations are POSTs under /v1/runner:
// the address is the operation and the body is the whole input.
type Conn struct {
	endpoint string
	base     string
	insecure bool
	uuid     string
	token    string
	http     *http.Client
}

var _ Client = (*Conn)(nil)

// New returns a client for the forge at endpoint. uuid and token are the
// credential a registered runner presents; both are empty while registering.
func New(endpoint string, insecure bool, uuid, token string) *Conn {
	transport := &http.Transport{
		MaxIdleConns: 10,
		// Every operation goes to one host, and the default of 2 causes frequent
		// reconnects while a job streams its log.
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
	if insecure && strings.HasPrefix(endpoint, "https://") {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Conn{
		endpoint: endpoint,
		base:     strings.TrimRight(endpoint, "/") + "/v1/runner/",
		insecure: insecure,
		uuid:     uuid,
		token:    token,
		http:     &http.Client{Transport: transport},
	}
}

func (c *Conn) Address() string { return c.endpoint }
func (c *Conn) Insecure() bool  { return c.insecure }

// Fault is what the forge answers when an operation fails: the status it chose,
// a short code, and a message. It is an error, so a caller that only wants to
// log it can, and one that needs to branch reads Status or Code.
type Fault struct {
	Status int    `json:"status"`
	Code   string `json:"code,omitempty"`
	Msg    string `json:"error"`
}

func (e *Fault) Error() string { return fmt.Sprintf("%s (%d)", e.Msg, e.Status) }

// call performs one operation and decodes the reply. It is a function rather
// than a method because Go methods cannot take type parameters; the methods
// below are one line each on top of it.
func call[In, Out any](ctx context.Context, c *Conn, opName string, in *In) (*Out, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", opName, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+opName, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.uuid != "" {
		req.Header.Set(UUIDHeader, c.uuid)
		req.Header.Set(TokenHeader, c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", opName, err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s reply: %w", opName, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		fault := &Fault{}
		if json.Unmarshal(payload, fault) != nil || fault.Msg == "" {
			return nil, &Fault{Status: resp.StatusCode, Msg: strings.TrimSpace(string(payload))}
		}
		if fault.Status == 0 {
			fault.Status = resp.StatusCode
		}
		return nil, fault
	}

	out := new(Out)
	if len(payload) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return nil, fmt.Errorf("decode %s reply: %w", opName, err)
	}
	return out, nil
}

func (c *Conn) Register(ctx context.Context, in *forge.RegisterIn) (*forge.RegisterOut, error) {
	return call[forge.RegisterIn, forge.RegisterOut](ctx, c, "register", in)
}

func (c *Conn) Declare(ctx context.Context, in *forge.DeclareIn) (*forge.DeclareOut, error) {
	return call[forge.DeclareIn, forge.DeclareOut](ctx, c, "declare", in)
}

func (c *Conn) Task(ctx context.Context, in *forge.TaskIn) (*forge.TaskOut, error) {
	return call[forge.TaskIn, forge.TaskOut](ctx, c, "task", in)
}

func (c *Conn) State(ctx context.Context, in *forge.StateIn) (*forge.StateOut, error) {
	return call[forge.StateIn, forge.StateOut](ctx, c, "state", in)
}

func (c *Conn) Logs(ctx context.Context, in *forge.LogsIn) (*forge.LogsOut, error) {
	return call[forge.LogsIn, forge.LogsOut](ctx, c, "logs", in)
}
