package auth

import (
	"log"
)

type SimpleAuthProvider struct {
	debugLog *log.Logger
}

func NewSimpleAuthProvider(debugLog *log.Logger) *SimpleAuthProvider {
	return &SimpleAuthProvider{
		debugLog: debugLog,
	}
}

func (p *SimpleAuthProvider) logDebug(format string, args ...interface{}) {
	if p.debugLog != nil {
		p.debugLog.Printf(format, args...)
	}
}
