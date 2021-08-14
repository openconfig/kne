package deploy

import (
	log "github.com/sirupsen/logrus"
	klog "sigs.k8s.io/kind/pkg/log"
)

type logAdapter struct {
	*log.Logger
}

func (l *logAdapter) Error(msg string) {
	l.Logger.Error(msg)
}

func (l *logAdapter) Info(msg string) {
	l.Logger.Info(msg)
}

func (l *logAdapter) Warn(msg string) {
	l.Logger.Warn(msg)
}

func (l *logAdapter) Enabled() bool {
	return true
}

type debugLogger struct {
	*log.Logger
}

func (l *debugLogger) Info(msg string) {
	l.Logger.Debug(msg)
}

func (l *debugLogger) Infof(fmt string, args ...interface{}) {
	l.Logger.Debugf(fmt, args...)
}

func (l *debugLogger) Enabled() bool {
	return true
}

func (l *logAdapter) V(level klog.Level) klog.InfoLogger {
	if level == 0 {
		return l
	}
	return &debugLogger{l.Logger}
}
