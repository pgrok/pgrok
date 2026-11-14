package embed

import (
	_ "embed"
)

// Config is the default config, which the config file overrides.
//
//go:embed config.ini
var Config []byte
