// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2017 Datadog, Inc.

package fanout

import (
	"errors"
	"time"
)

// Config holds the arguments for Setup
type Config struct {
	Name             string
	WriteTimeout     time.Duration
	OutputBufferSize int
}

// ErrTimeout is sent when a listener is forcefully unsuscribed
// because if a write timeout. It is the responsibility of the
// listener to recover from that.
var ErrTimeout = errors.New("timeout while writing to channel")
