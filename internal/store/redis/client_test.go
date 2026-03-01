package redis

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestWriteRESPArray(t *testing.T) {
	var buf bytes.Buffer
	rw := bufio.NewReadWriter(bufio.NewReader(&buf), bufio.NewWriter(&buf))
	if err := writeRESPArray(rw, []string{"SET", "k", "v"}); err != nil {
		t.Fatalf("writeRESPArray failed: %v", err)
	}
	if err := rw.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	want := "*3\r\n$3\r\nSET\r\n$1\r\nk\r\n$1\r\nv\r\n"
	if got := buf.String(); got != want {
		t.Fatalf("unexpected payload:\n%s", got)
	}
}

func TestReadRESP(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantErr bool
	}{
		{name: "simple", payload: "+OK\r\n", wantErr: false},
		{name: "integer", payload: ":42\r\n", wantErr: false},
		{name: "bulk", payload: "$5\r\nhello\r\n", wantErr: false},
		{name: "error", payload: "-ERR bad\r\n", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := bufio.NewReader(strings.NewReader(tc.payload))
			_, err := readRESP(r)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
