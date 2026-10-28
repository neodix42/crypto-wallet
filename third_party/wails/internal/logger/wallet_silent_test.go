package logger

import (
	public "github.com/wailsapp/wails/v2/pkg/logger"
	"testing"
)

type forbiddenLogger struct{ t *testing.T }

func (f forbiddenLogger) Print(string)   { f.t.Fatal("private runtime log escaped") }
func (f forbiddenLogger) Trace(string)   { f.t.Fatal("private runtime log escaped") }
func (f forbiddenLogger) Debug(string)   { f.t.Fatal("private runtime log escaped") }
func (f forbiddenLogger) Info(string)    { f.t.Fatal("private runtime log escaped") }
func (f forbiddenLogger) Warning(string) { f.t.Fatal("private runtime log escaped") }
func (f forbiddenLogger) Error(string)   { f.t.Fatal("private runtime log escaped") }
func (f forbiddenLogger) Fatal(string)   { f.t.Fatal("private runtime log escaped") }
func TestWalletNeverForwardsLogs(t *testing.T) {
	l := New(forbiddenLogger{t})
	l.SetLogLevel(public.TRACE)
	l.Print("disposable canary")
	l.Trace("%s", "disposable canary")
	l.Debug("canary")
	l.Info("canary")
	l.Warning("canary")
	l.Error("canary")
	l.CustomLogger("canary").Error("canary")
}
