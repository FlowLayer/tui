package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempFlowLayerConfig(t *testing.T, raw string) string {
	t.Helper()

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "flowlayer.jsonc")
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

func TestResolveRuntimeOptionsLoadsConfigFromConfigFlag(t *testing.T) {
	configPath := writeTempFlowLayerConfig(t, `{
	  // shared flowlayer config used by multiple clients
	  "session": {
	    "bind": "127.0.0.1:7999",
	    "token": "cfg-token"
	  },
	  "services": {
	    "billing": {
	      "cmd": "npm run billing",
	      "port": 3002
	    },
	    "users": {
	      "cmd": "npm run users",
	      "port": 3003,
	      "dependsOn": ["billing",]
	    }
	  },
	}`)

	options, err := resolveRuntimeOptions(configPath, "", false, "", false)
	if err != nil {
		t.Fatalf("resolve runtime options: %v", err)
	}

	if options.addr != "127.0.0.1:7999" {
		t.Fatalf("addr = %q, want %q", options.addr, "127.0.0.1:7999")
	}
	if options.token != "cfg-token" {
		t.Fatalf("token = %q, want %q", options.token, "cfg-token")
	}
}

func TestResolveRuntimeOptionsFlagsOverrideConfig(t *testing.T) {
	configPath := writeTempFlowLayerConfig(t, `{
	  "session": {
	    "bind": "127.0.0.1:7999",
	    "token": "cfg-token"
	  },
	  "services": {
	    "billing": {
	      "cmd": "npm run billing",
	      "port": 3002
	    }
	  }
	}`)

	options, err := resolveRuntimeOptions(configPath, "127.0.0.1:9001", true, "cli-token", true)
	if err != nil {
		t.Fatalf("resolve runtime options: %v", err)
	}

	if options.addr != "127.0.0.1:9001" {
		t.Fatalf("addr = %q, want %q", options.addr, "127.0.0.1:9001")
	}
	if options.token != "cli-token" {
		t.Fatalf("token = %q, want %q", options.token, "cli-token")
	}
}

func TestResolveRuntimeOptionsUsesFallbackWithoutConfig(t *testing.T) {
	options, err := resolveRuntimeOptions("", "", false, "", false)
	if err != nil {
		t.Fatalf("resolve runtime options: %v", err)
	}

	if options.addr != defaultAddrFallback {
		t.Fatalf("addr = %q, want %q", options.addr, defaultAddrFallback)
	}
	if options.token != defaultTokenFallback {
		t.Fatalf("token = %q, want %q", options.token, defaultTokenFallback)
	}
}

func TestResolveRuntimeOptionsRejectsTopLevelLogViewField(t *testing.T) {
	configPath := writeTempFlowLayerConfig(t, `{
	  "session": {
	    "bind": "127.0.0.1:7999",
	    "token": "cfg-token"
	  },
	  "logView": {
	    "maxEntries": 100
	  },
	  "services": {
	    "billing": {
	      "cmd": "npm run billing",
	      "port": 3002
	    }
	  }
	}`)

	_, err := resolveRuntimeOptions(configPath, "", false, "", false)
	if err == nil {
		t.Fatal("expected unknown-field error for top-level logView")
	}
	if !strings.Contains(err.Error(), `unknown field "logView"`) {
		t.Fatalf("error = %q, want unknown-field logView", err)
	}
	if !strings.Contains(err.Error(), `parse config file`) {
		t.Fatalf("error = %q, want parse config context", err)
	}
}

func TestResolveRuntimeOptionsRejectsServiceLogViewField(t *testing.T) {
	configPath := writeTempFlowLayerConfig(t, `{
	  "session": {
	    "bind": "127.0.0.1:7999",
	    "token": "cfg-token"
	  },
	  "services": {
	    "billing": {
	      "cmd": "npm run billing",
	      "port": 3002,
	      "logView": {
	        "maxEntries": 100
	      }
	    }
	  }
	}`)

	_, err := resolveRuntimeOptions(configPath, "", false, "", false)
	if err == nil {
		t.Fatal("expected unknown-field error for services.*.logView")
	}
	if !strings.Contains(err.Error(), `unknown field "logView"`) {
		t.Fatalf("error = %q, want unknown-field logView", err)
	}
}
