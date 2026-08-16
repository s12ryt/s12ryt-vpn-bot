package main

import (
	"testing"
)

func TestLoadControllerConfigUsesSafeDefaults(t *testing.T) {
	configuration, err := loadControllerConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadControllerConfig() error = %v", err)
	}
	if configuration.controlSocket != "/run/s12ryt/core-control.sock" ||
		configuration.activeConfig != "/var/lib/s12ryt/sing-box/config.json" ||
		configuration.singBoxBinary != "/usr/local/bin/sing-box" ||
		configuration.dockerSocket != "/var/run/docker.sock" ||
		configuration.containerName != "s12ryt-sing-box" {
		t.Fatalf("configuration = %#v", configuration)
	}
}

func TestLoadControllerConfigRejectsRelativePathsAndUnsafeContainer(t *testing.T) {
	tests := map[string]string{
		"CORE_CONTROL_SOCKET": "relative.sock",
		"SINGBOX_CONFIG_PATH": "config.json",
		"SINGBOX_BINARY":      "sing-box",
		"DOCKER_SOCKET":       "docker.sock",
		"SINGBOX_CONTAINER":   "../other",
	}
	for key, value := range tests {
		t.Run(key, func(t *testing.T) {
			_, err := loadControllerConfig(func(name string) string {
				if name == key {
					return value
				}
				return ""
			})
			if err == nil {
				t.Fatalf("loadControllerConfig() accepted %s=%q", key, value)
			}
		})
	}
}
