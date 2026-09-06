// Copyright 2026 Hanzo AI, Inc. All rights reserved.
// SPDX-License-Identifier: MIT

// Package mocks holds a client.Client double for tests. Every operation has the
// same shape — a context and one input, a pointer and an error — so answer does
// the work once and each method is one line on top of it.
package mocks

import (
	"context"

	forge "github.com/hanzoai/git/modules/actions/runner"
	"github.com/stretchr/testify/mock"
)

type Client struct {
	mock.Mock
}

type testing interface {
	mock.TestingT
	Cleanup(func())
}

// NewClient returns a Client that asserts its expectations when t finishes.
func NewClient(t testing) *Client {
	m := &Client{}
	m.Mock.Test(t)
	t.Cleanup(func() { m.AssertExpectations(t) })
	return m
}

func (m *Client) Address() string { return m.MethodCalled("Address").Get(0).(string) }

// answer dispatches one operation. A test registers either the reply itself or a
// function to compute it, which is what lets a test answer differently per call.
func answer[In, Out any](m *Client, opName string, ctx context.Context, in *In) (*Out, error) {
	ret := m.MethodCalled(opName, ctx, in)
	if fn, ok := ret.Get(0).(func(context.Context, *In) (*Out, error)); ok {
		return fn(ctx, in)
	}
	out, _ := ret.Get(0).(*Out)
	return out, ret.Error(1)
}

func (m *Client) Register(ctx context.Context, in *forge.RegisterIn) (*forge.RegisterOut, error) {
	return answer[forge.RegisterIn, forge.RegisterOut](m, "Register", ctx, in)
}

func (m *Client) Declare(ctx context.Context, in *forge.DeclareIn) (*forge.DeclareOut, error) {
	return answer[forge.DeclareIn, forge.DeclareOut](m, "Declare", ctx, in)
}

func (m *Client) Task(ctx context.Context, in *forge.TaskIn) (*forge.TaskOut, error) {
	return answer[forge.TaskIn, forge.TaskOut](m, "Task", ctx, in)
}

func (m *Client) State(ctx context.Context, in *forge.StateIn) (*forge.StateOut, error) {
	return answer[forge.StateIn, forge.StateOut](m, "State", ctx, in)
}

func (m *Client) Log(ctx context.Context, in *forge.LogIn) (*forge.LogOut, error) {
	return answer[forge.LogIn, forge.LogOut](m, "Log", ctx, in)
}
