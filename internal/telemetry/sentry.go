// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package telemetry

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/getsentry/sentry-go"
	"github.com/pkg/errors"

	"go.jetpack.io/devbox/internal/build"
	"go.jetpack.io/devbox/internal/envir"
	"go.jetpack.io/devbox/internal/redact"
	"go.jetpack.io/devbox/internal/xdg"
)

var ExecutionID string

func init() {
	// Generate event UUIDs the same way the Sentry SDK does:
	// https://github.com/getsentry/sentry-go/blob/d9ce5344e7e1819921ea4901dd31e47a200de7e0/util.go#L15
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	id[6] &= 0x0F
	id[6] |= 0x40
	id[8] &= 0x3F
	id[8] |= 0x80
	ExecutionID = hex.EncodeToString(id)
}

var needsFlush atomic.Bool
var started bool

// Start enables telemetry for the current program.
func Start(appName string) {
	if started || envir.DoNotTrack() {
		return
	}
	started = initSentry(appName)
}

// Stop stops gathering telemetry and flushes buffered events to disk.
func Stop() {
	if !started || !needsFlush.Load() {
		return
	}

	// Report errors in a separate process so we don't block exiting.
	exe, err := os.Executable()
	if err == nil {
		_ = exec.Command(exe, "bug").Start()
	}
	started = false
}

var errorBufferDir = xdg.StateSubpath(filepath.FromSlash("devbox/sentry"))

func ReportErrors() {
	if !initSentry(AppDevbox) {
		return
	}

	dirEntries, err := os.ReadDir(errorBufferDir)
	if err != nil {
		return
	}
	for _, entry := range dirEntries {
		if !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(errorBufferDir, entry.Name())
		data, err := os.ReadFile(path)
		// Always delete the file so we don't end up with an infinitely growing
		// backlog of errors.
		_ = os.Remove(path)
		if err != nil {
			continue
		}

		event := &sentry.Event{}
		if err := json.Unmarshal(data, event); err != nil {
			continue
		}
		sentry.CaptureEvent(event)
	}
	sentry.Flush(3 * time.Second)
}

func initSentry(appName string) bool {
	if appName == "" {
		panic("telemetry.Start: app name is empty")
	}
	if build.SentryDSN == "" {
		return false
	}

	transport := sentry.NewHTTPTransport()
	transport.Timeout = time.Second * 2
	environment := "production"
	if build.IsDev {
		environment = "development"
	}
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              build.SentryDSN,
		Environment:      environment,
		Release:          appName + "@" + build.Version,
		Transport:        transport,
		TracesSampleRate: 1,
		BeforeSend: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			// redact the hostname, which the SDK automatically adds
			event.ServerName = ""
			return event
		},
	})
	return err == nil
}

type Metadata struct {
	Command      string
	CommandFlags []string
	FeatureFlags map[string]bool

	InShell   bool
	InCloud   bool
	InBrowser bool

	NixpkgsHash string
	Packages    []string

	CloudRegion string
	CloudCache  string
}

func (m *Metadata) cmdContext() map[string]any {
	sentryCtx := map[string]any{}
	if m.Command != "" {
		sentryCtx["Name"] = m.Command
	}
	if len(m.CommandFlags) > 0 {
		sentryCtx["Flags"] = m.CommandFlags
	}
	return sentryCtx
}

func (m *Metadata) envContext() map[string]any {
	sentryCtx := map[string]any{
		"In Shell":   m.InShell,
		"In Cloud":   m.InCloud,
		"In Browser": m.InBrowser,
	}
	if m.CloudCache != "" {
		sentryCtx["Cloud Cache"] = m.CloudCache
	}
	if m.CloudRegion != "" {
		sentryCtx["Cloud Region"] = m.CloudRegion
	}
	return sentryCtx
}

func (m *Metadata) featureContext() map[string]any {
	if len(m.FeatureFlags) == 0 {
		return nil
	}

	sentryCtx := make(map[string]any, len(m.FeatureFlags))
	for name, enabled := range m.FeatureFlags {
		sentryCtx[name] = enabled
	}
	return sentryCtx
}

func (m *Metadata) pkgContext() map[string]any {
	if len(m.Packages) == 0 {
		return nil
	}

	// Every package currently has the same commit hash as its version, but this
	// format will allow us to use individual package versions in the future.
	pkgVersion := "nixpkgs"
	if m.NixpkgsHash != "" {
		pkgVersion += "/" + m.NixpkgsHash
	}
	pkgVersion += "#"
	pkgContext := make(map[string]any, len(m.Packages))
	for _, pkg := range m.Packages {
		pkgContext[pkg] = pkgVersion + pkg
	}
	return pkgContext
}

// Error reports an error to the telemetry server.
func Error(err error, meta Metadata) {
	if !started || err == nil {
		return
	}

	event := &sentry.Event{
		EventID:   sentry.EventID(ExecutionID),
		Level:     sentry.LevelError,
		User:      sentry.User{ID: DeviceID},
		Exception: newSentryException(redact.Error(err)),
		Contexts: map[string]map[string]any{
			"os": {
				"name": build.OS(),
			},
			"device": {
				"arch": runtime.GOARCH,
			},
			"runtime": {
				"name":    "Go",
				"version": strings.TrimPrefix(runtime.Version(), "go"),
			},
		},
	}
	if meta.Command != "" {
		event.Tags = map[string]string{"command": meta.Command}
	}
	if sentryCtx := meta.cmdContext(); len(sentryCtx) > 0 {
		event.Contexts["Command"] = sentryCtx
	}
	if sentryCtx := meta.envContext(); len(sentryCtx) > 0 {
		event.Contexts["Devbox Environment"] = sentryCtx
	}
	if sentryCtx := meta.featureContext(); len(sentryCtx) > 0 {
		event.Contexts["Feature Flags"] = sentryCtx
	}
	if sentryCtx := meta.pkgContext(); len(sentryCtx) > 0 {
		event.Contexts["Devbox Packages"] = sentryCtx
	}
	bufferEvent(event)
}

// bufferEvent buffers a Sentry event to disk so that ReportErrors can upload
// it later.
func bufferEvent(event *sentry.Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	file := filepath.Join(errorBufferDir, string(event.EventID)+".json")
	err = os.WriteFile(file, data, 0600)
	if errors.Is(err, fs.ErrNotExist) {
		// XDG specifies perms 0700.
		if err := os.MkdirAll(errorBufferDir, 0700); err != nil {
			return
		}
		err = os.WriteFile(file, data, 0600)
	}
	if err == nil {
		needsFlush.Store(true)
	}
}

func newSentryException(err error) []sentry.Exception {
	errMsg := err.Error()
	binPkg := ""
	modPath := ""
	if build, ok := debug.ReadBuildInfo(); ok {
		binPkg = build.Path
		modPath = build.Main.Path
	}

	// Unwrap in a loop to get the most recent stack trace. stFunc is set to a
	// function that can generate a stack trace for the most recent error. This
	// avoids computing the full stack trace for every error.
	var stFunc func() []runtime.Frame
	errType := "Generic Error"
	for {
		if t := exportedErrType(err); t != "" {
			errType = t
		}

		//nolint:errorlint
		switch stackErr := err.(type) {
		// If the error implements the StackTrace method in the redact package, then
		// prefer that. The Sentry SDK gets some things wrong when guessing how
		// to extract the stack trace.
		case interface{ StackTrace() []runtime.Frame }:
			stFunc = stackErr.StackTrace
		// Otherwise use the pkg/errors StackTracer interface.
		case interface{ StackTrace() errors.StackTrace }:
			// Normalize the pkgs/errors.StackTrace type to a slice of runtime.Frame.
			stFunc = func() []runtime.Frame {
				pkgStack := stackErr.StackTrace()
				pc := make([]uintptr, len(pkgStack))
				for i := range pkgStack {
					pc[i] = uintptr(pkgStack[i])
				}
				frameIter := runtime.CallersFrames(pc)
				frames := make([]runtime.Frame, 0, len(pc))
				for {
					frame, more := frameIter.Next()
					frames = append(frames, frame)
					if !more {
						break
					}
				}
				return frames
			}
		}
		uw := errors.Unwrap(err)
		if uw == nil {
			break
		}
		err = uw
	}
	ex := []sentry.Exception{{Type: errType, Value: errMsg}}
	if stFunc != nil {
		ex[0].Stacktrace = newSentryStack(stFunc(), binPkg, modPath)
	}
	return ex
}

func newSentryStack(frames []runtime.Frame, binPkg, modPath string) *sentry.Stacktrace {
	stack := &sentry.Stacktrace{
		Frames: make([]sentry.Frame, len(frames)),
	}
	for i, frame := range frames {
		pkgName, funcName := splitPkgFunc(frame.Function)

		// The entrypoint has the full function name "main.main". Replace the
		// package name with its full package path to make it easier to find.
		if pkgName == "main" {
			pkgName = binPkg
		}

		// The file path will be absolute unless the binary was built with -trimpath
		// (which releases should be). Absolute paths make it more difficult for
		// Sentry to correctly group errors, but there's no way to infer a relative
		// path from an absolute path at runtime.
		var absPath, relPath string
		if filepath.IsAbs(frame.File) {
			absPath = frame.File
		} else {
			relPath = frame.File
		}

		// Reverse the frames - Sentry wants the most recent call first.
		stack.Frames[len(frames)-i-1] = sentry.Frame{
			Function: funcName,
			Module:   pkgName,
			Filename: relPath,
			AbsPath:  absPath,
			Lineno:   frame.Line,
			InApp:    strings.HasPrefix(frame.Function, modPath) || pkgName == binPkg,
		}
	}
	return stack
}

// exportedErrType returns the underlying type name of err if it's exported.
// Otherwise, it returns an empty string.
func exportedErrType(err error) string {
	t := reflect.TypeOf(err)
	if t == nil {
		return ""
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	name := t.Name()
	if r, _ := utf8.DecodeRuneInString(name); unicode.IsUpper(r) {
		return t.String()
	}
	return ""
}

// splitPkgFunc splits a fully-qualified function or method name into its
// package path and base name components.
func splitPkgFunc(name string) (pkgPath string, funcName string) {
	// Using the following fully-qualified function name as an example:
	// go.jetpack.io/devbox/internal/impl.(*Devbox).RunScript

	// dir = go.jetpack.io/devbox/internal/
	// base = impl.(*Devbox).RunScript
	dir, base := path.Split(name)

	// pkgName = impl
	// fn = (*Devbox).RunScript
	pkgName, fn, _ := strings.Cut(base, ".")

	// pkgPath = go.jetpack.io/devbox/internal/impl
	// funcName = (*Devbox).RunScript
	return dir + pkgName, fn
}
