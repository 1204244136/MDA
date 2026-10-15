package pienv

import (
	"errors"
	"sync"
	"testing"
)

func TestNormalizeControllerKind(t *testing.T) {
	for _, tt := range []struct {
		raw, want string
	}{
		{" AdB ", ControllerKindAdb},
		{"Win32", ControllerKindWin32},
		{"PlayCover", ControllerKindPlayCover},
		{" PLAY_COVER ", ControllerKindPlayCover},
		{" Native_Android ", ControllerKindNativeAndroid},
		{"macOS", ControllerKindMacOS},
		{"Linux", ControllerKindLinux},
		{"Gamepad", ControllerKindGamepad},
		{"", ControllerKindUnknown},
		{"unknown", ControllerKindUnknown},
		{"adb-device", ControllerKindUnknown},
	} {
		t.Run(tt.raw, func(t *testing.T) {
			if got := NormalizeControllerKind(tt.raw); got != tt.want {
				t.Fatalf("NormalizeControllerKind(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestResolveControllerKindPrefersPI(t *testing.T) {
	got := resolveControllerKind("Native_Android", func() (string, error) {
		t.Fatal("known PI type must not query the controller")
		return "", nil
	})
	if got != ControllerKindNativeAndroid {
		t.Fatalf("got %q, want native_android", got)
	}
}

func TestResolveControllerKindFallback(t *testing.T) {
	for _, raw := range []string{"", "unsupported"} {
		for _, tt := range []struct {
			name, info, want string
			err              error
		}{
			{name: "adb", info: `{"type":"Adb","adb_path":"adb.exe"}`, want: ControllerKindAdb},
			{name: "native Android", info: `{"type":"native_android"}`, want: ControllerKindNativeAndroid},
			{name: "alias", info: `{"type":"play_cover"}`, want: ControllerKindPlayCover},
			{name: "win32", info: `{"type":"Win32"}`, want: ControllerKindWin32},
			{name: "failed", info: `{"type":"Adb"}`, err: errors.New("disconnected")},
			{name: "empty"},
			{name: "malformed", info: `{"type":"Adb"`},
			{name: "error text", info: "failed to connect to adb"},
			{name: "no type", info: `{"name":"adb","path":"win32"}`},
			{name: "wrong type", info: `{"type":42}`},
			{name: "unknown type", info: `{"type":"custom","name":"adb"}`},
		} {
			t.Run(raw+"/"+tt.name, func(t *testing.T) {
				calls := 0
				got := resolveControllerKind(raw, func() (string, error) {
					calls++
					return tt.info, tt.err
				})
				if got != tt.want || calls != 1 {
					t.Fatalf("got %q with %d reads, want %q with one read", got, calls, tt.want)
				}
			})
		}
	}
	if got := resolveControllerKind("", nil); got != ControllerKindUnknown {
		t.Fatalf("missing PI and controller = %q, want unknown", got)
	}
}

func TestControllerKindKeepsRawTypeAndHandlesNilController(t *testing.T) {
	for _, tt := range []struct {
		name, env, raw, kind string
	}{
		{"valid", `{"name":"phone","type":" Native_Android "}`, " Native_Android ", ControllerKindNativeAndroid},
		{"missing", "", "", ControllerKindUnknown},
		{"malformed", `{"type":`, "", ControllerKindUnknown},
		{"unknown", `{"type":"future"}`, "future", ControllerKindUnknown},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvController, tt.env)
			once = sync.Once{}
			global = nil
			t.Cleanup(func() {
				once = sync.Once{}
				global = nil
			})
			if got := ControllerType(); got != tt.raw {
				t.Fatalf("raw type = %q, want %q", got, tt.raw)
			}
			if got := ControllerKind(); got != tt.kind {
				t.Fatalf("PI kind = %q, want %q", got, tt.kind)
			}
			if got := ResolveControllerKind(nil); got != tt.kind {
				t.Fatalf("nil controller kind = %q, want %q", got, tt.kind)
			}
		})
	}
}
