// Package agentcfg loads declarative sub-agent definitions from disk.
//
// Each *.yaml file in the configured directory becomes one specialist agent.
// The `description` field is the load-bearing one — the host multi-agent's
// LLM routes on it (it's exposed as the specialist's "IntendedUse").
package agentcfg

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

type AgentConfig struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	SystemPrompt string   `yaml:"system_prompt"`
	Tools        []string `yaml:"tools"`
	MaxStep      int      `yaml:"max_step"`
}

// Load reads every *.yaml/*.yml under dir, sorted by filename for determinism.
func Load(dir string) ([]AgentConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read agents dir %q: %w", dir, err)
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext == ".yaml" || ext == ".yml" {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(paths)

	var out []AgentConfig
	seenName := map[string]string{}
	for _, p := range paths {
		cfg, err := loadOne(p)
		if err != nil {
			return nil, err
		}
		if prev, ok := seenName[cfg.Name]; ok {
			return nil, fmt.Errorf("duplicate agent name %q in %s (also in %s)", cfg.Name, p, prev)
		}
		seenName[cfg.Name] = p
		out = append(out, cfg)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no agent yaml files found under %q", dir)
	}
	return out, nil
}

func loadOne(path string) (AgentConfig, error) {
	var cfg AgentConfig
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Name == "" {
		return cfg, fmt.Errorf("%s: name is required", path)
	}
	if cfg.Description == "" {
		return cfg, fmt.Errorf("%s: description is required (used by host for routing)", path)
	}
	if cfg.SystemPrompt == "" {
		return cfg, fmt.Errorf("%s: system_prompt is required", path)
	}
	if cfg.MaxStep <= 0 {
		cfg.MaxStep = 12
	}
	return cfg, nil
}
