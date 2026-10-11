package main

import (
	"context"
	"github.com/josh/codex-switch/internal/codexstate"
	"time"
)

func loadCodexModelConfig() (codexstate.EffectiveModelConfig, string) {
	reader, err := codexstate.DefaultReader()
	if err != nil {
		return codexstate.EffectiveModelConfig{}, err.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	value, err := reader.ModelConfig(ctx)
	if err != nil {
		return codexstate.EffectiveModelConfig{}, err.Error()
	}
	return value, ""
}
