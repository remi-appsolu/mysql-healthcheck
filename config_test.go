package main

import "testing"

func TestCreateConfig(t *testing.T) {
	config := CreateConfig()
	// Validate creation of basic config with defaults
	if config == nil {
		t.Error("Received nil object from CreateConfig()")
	}

	numDefaults := len(config.AllKeys())
	if numDefaults == 0 {
		t.Error("No default values found in config.")
	}
}
