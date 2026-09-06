// Copyright 2026 Hanzo AI, Inc. All rights reserved.
// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"

	forge "github.com/hanzoai/git/modules/actions/runner"

	"github.com/valyala/fasthttp"
	"github.com/zap-proto/zip"
)

// Forge is a live handle to one forge. Every operation is a zip.Call addressed
// by the op's name, so the runner never spells a URL: the forge registers
// POST /v1/runner/<name> and zip derives the name both ends use from it.
type Forge struct {
	address string // as configured, and what a job is told its forge is
	cred    forge.Credential
	conn    *zip.Conn
}

var _ Client = (*Forge)(nil)

// New returns a handle to the forge at address. cred is empty while
// registering and is the minted runner credential ever after; wait bounds a
// call, and insecure accepts a certificate this host does not trust.
func New(address string, cred forge.Credential, insecure bool, wait time.Duration) (*Forge, error) {
	conn, err := dial(address, insecure, wait)
	if err != nil {
		return nil, err
	}
	return &Forge{address: address, cred: cred, conn: conn}, nil
}

func (f *Forge) Address() string { return f.address }

// scheme names the wire a runner reaches its forge over: ordinary HTTP, with a
// ceiling on the wait and the operator's answer on certificates.
//
// It is the runner's own scheme rather than "http" and "https" because
// declaring a transport REPLACES the one registered under that name. Taking
// those two names would leave the process able to dial plain HTTP through zip
// and no longer able to serve it — a capability removed at a distance from
// whatever asked for it next.
const scheme = "forge"

// dial resolves the configured address to the wire the runner speaks over it.
//
// The ceiling is not optional: a call is not cancellable once it is on the wire
// — a context deadline fails it before the first byte and never after — so a
// forge that accepts a connection and then says nothing would hold the poll
// loop for as long as it liked.
func dial(address string, insecure bool, wait time.Duration) (*zip.Conn, error) {
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || u.Scheme == "" {
		return nil, fmt.Errorf("forge address %q is not an absolute URL", address)
	}
	secure := u.Scheme == "https"
	zip.RegisterTransport(scheme, zip.Transport{Dial: func(addr string) zip.Client {
		return &fasthttp.HostClient{
			Addr:                   port(addr, secure),
			IsTLS:                  secure,
			TLSConfig:              verify(insecure),
			DisablePathNormalizing: true,
			ReadTimeout:            wait,
			WriteTimeout:           wait,
		}
	}})
	// Host only: the op plane answers at the root of the forge, and a dialer
	// cannot use a path.
	return zip.Dial(scheme + "://" + u.Host)
}

// port defaults the port a URL usually omits and a dialer never can.
func port(addr string, secure bool) string {
	if strings.LastIndex(addr, ":") > strings.LastIndex(addr, "]") {
		return addr
	}
	if secure {
		return addr + ":443"
	}
	return addr + ":80"
}

func verify(insecure bool) *tls.Config {
	if insecure {
		return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // the operator asked for it
	}
	return nil
}

func (f *Forge) Register(ctx context.Context, in *forge.RegisterIn) (*forge.RegisterOut, error) {
	return take(zip.Call[forge.RegisterIn, forge.RegisterOut](ctx, f.conn, "post_runner_register", in))
}

func (f *Forge) Declare(ctx context.Context, in *forge.DeclareIn) (*forge.DeclareOut, error) {
	in.Credential = f.cred
	return take(zip.Call[forge.DeclareIn, forge.DeclareOut](ctx, f.conn, "post_runner_declare", in))
}

func (f *Forge) Task(ctx context.Context, in *forge.TaskIn) (*forge.TaskOut, error) {
	in.Credential = f.cred
	return take(zip.Call[forge.TaskIn, forge.TaskOut](ctx, f.conn, "post_runner_task", in))
}

func (f *Forge) State(ctx context.Context, in *forge.StateIn) (*forge.StateOut, error) {
	in.Credential = f.cred
	return take(zip.Call[forge.StateIn, forge.StateOut](ctx, f.conn, "post_runner_state", in))
}

func (f *Forge) Log(ctx context.Context, in *forge.LogIn) (*forge.LogOut, error) {
	in.Credential = f.cred
	return take(zip.Call[forge.LogIn, forge.LogOut](ctx, f.conn, "post_runner_log", in))
}

// take gives the caller a reply it owns.
//
// ZAP reads text and bytes without copying — zap.Object.Text is an
// unsafe.String over the message — and zip.Call decodes the reply and then
// hands the response object back to fasthttp's pool. Every string a reply
// carries therefore points into memory the next call writes over. Measured on
// this wire: a runner token registered as "runner-secret" read back
// "1.2.3r-secret" one call later, the version string of the call in between
// laid over its first five bytes, and the forge answered 401 to a runner that
// was registered.
//
// So a reply is copied here, at the one door replies come through, and nothing
// downstream has to know where its strings came from.
func take[Out any](out *Out, err error) (*Out, error) {
	if out != nil {
		own(reflect.ValueOf(out).Elem())
	}
	var fault *zip.HTTPError
	if errors.As(err, &fault) {
		fault.Code, fault.Msg = strings.Clone(fault.Code), strings.Clone(fault.Msg)
	}
	return out, err
}

// own replaces every string and every []byte reachable from v with a copy.
func own(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() && v.Len() > 0 {
			v.SetString(strings.Clone(v.String()))
		}
	case reflect.Slice:
		if v.IsNil() {
			return
		}
		if v.Type().Elem().Kind() == reflect.Uint8 {
			if v.CanSet() {
				v.SetBytes(bytes.Clone(v.Bytes()))
			}
			return
		}
		fallthrough
	case reflect.Array:
		for i := range v.Len() {
			own(v.Index(i))
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			own(v.Elem())
		}
	case reflect.Struct:
		for i := range v.NumField() {
			own(v.Field(i))
		}
	}
}
