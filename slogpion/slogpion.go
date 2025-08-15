package slogpion

import (
	"fmt"
	"log/slog"

	"github.com/pion/logging"
)

// leveled adapts slog.Logger to Pion's LeveledLogger interface.
type leveled struct{ l *slog.Logger }

// Pion has Trace methods even if slog does not – map them to Debug.
func (lv *leveled) Trace(msg string)          { lv.l.Debug(msg) }
func (lv *leveled) Tracef(f string, a ...any) { lv.l.Debug(fmt.Sprintf(f, a...)) }
func (lv *leveled) Debug(msg string)          { lv.l.Debug(msg) }
func (lv *leveled) Debugf(f string, a ...any) { lv.l.Debug(fmt.Sprintf(f, a...)) }
func (lv *leveled) Info(msg string)           { lv.l.Info(msg) }
func (lv *leveled) Infof(f string, a ...any)  { lv.l.Info(fmt.Sprintf(f, a...)) }
func (lv *leveled) Warn(msg string)           { lv.l.Warn(msg) }
func (lv *leveled) Warnf(f string, a ...any)  { lv.l.Warn(fmt.Sprintf(f, a...)) }
func (lv *leveled) Error(msg string)          { lv.l.Error(msg) }
func (lv *leveled) Errorf(f string, a ...any) { lv.l.Error(fmt.Sprintf(f, a...)) }

// Factory produces a new slog-backed LeveledLogger for each Pion sub-scope.
type Factory struct{ base *slog.Logger }

func New(base *slog.Logger) *Factory { return &Factory{base} }

func (f *Factory) NewLogger(scope string) logging.LeveledLogger {
	return &leveled{l: f.base.With("pion.scope", scope)}
}
