package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Recovery instructions must run against the Compose volume with Core stopped.
func TestDockerResetDocumentation(t *testing.T) {
	sequence := "docker compose stop core\ndocker compose run --rm -it core admin reset-password\ndocker compose start core"
	for _, name := range []string{"docs/deployment.md", "docs/deployment.zh-CN.md"} {
		data, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), sequence) {
			t.Errorf("%s: include stop, interactive reset and restart in order", name)
		}
		lock := "data directory lock"
		if strings.HasSuffix(name, ".zh-CN.md") {
			lock = "数据目录锁"
		}
		if !strings.Contains(string(data), lock) {
			t.Errorf("%s: explain the directory lock", name)
		}
	}
}
