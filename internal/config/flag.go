package config

import "flag"

var configPath string

// InitFlags is for explicitly initializing the flags.
func InitFlags(flagset *flag.FlagSet) {
	flagset.StringVar(&configPath, "config", configPath, "Config file path to load")
}
