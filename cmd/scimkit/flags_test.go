package main

import (
	"bytes"
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scimtest"
)

func TestMockOptionsValidation(t *testing.T) {
	tests := []struct {
		name     string
		failRate float64
		latency  time.Duration
		wantErr  string
	}{
		{name: "zero is inert", failRate: 0},
		{name: "one is the upper bound", failRate: 1},
		{name: "negative fail-rate", failRate: -0.1, wantErr: "outside [0,1]"},
		{name: "fail-rate above one", failRate: 1.5, wantErr: "outside [0,1]"},
		{name: "NaN fail-rate", failRate: math.NaN(), wantErr: "outside [0,1]"},
		{name: "negative latency", latency: -time.Millisecond, wantErr: "negative"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := mockOptions("strict", "", "", tt.latency, tt.failRate)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.NotEmpty(t, opts)
		})
	}
}

func TestResolveToken(t *testing.T) {
	tests := []struct {
		name string
		flag string
		env  string
		want string
	}{
		{name: "flag wins over env", flag: "flag-token", env: "env-token", want: "flag-token"},
		{name: "env fallback when flag empty", env: "env-token", want: "env-token"},
		{name: "neither set", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tokenEnv, tt.env)
			assert.Equal(t, tt.want, resolveToken(tt.flag))
		})
	}
}

func TestExerciseUsesTokenEnv(t *testing.T) {
	srv := scimtest.NewServer(t, scimtest.WithBearerToken("from-env"))
	tests := []struct {
		name    string
		env     string
		wantErr error
	}{
		{name: "env token authenticates", env: "from-env"},
		{name: "wrong env token fails the scenario", env: "nope", wantErr: errExerciseFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tokenEnv, tt.env)
			var buf bytes.Buffer
			err := run(context.Background(),
				[]string{"exercise", "--target", srv.URL(), "--scenario", "rfc-strict-lifecycle"}, &buf)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestIsLoopback(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{addr: "127.0.0.1:8080", want: true},
		{addr: "[::1]:8080", want: true},
		{addr: "localhost:8080", want: true},
		{addr: ":8080", want: false},
		{addr: "0.0.0.0:8080", want: false},
		{addr: "10.0.0.5:8080", want: false},
		{addr: "not-an-addr", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			assert.Equal(t, tt.want, isLoopback(tt.addr))
		})
	}
}
