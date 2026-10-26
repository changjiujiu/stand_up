package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigLegacyPosition(t *testing.T) {
	p := filepath.Join(t.TempDir(), "standup.ini")
	cases := []struct {
		ini  string
		want string
		warn int
	}{
		{"position=corner\r\n", "bottom_right", 0}, // v1 配置无损迁移
		{"position=top_left\r\n", "top_left", 0},
		{"position = CENTER\r\n", "center", 0},
		{"position=nowhere\r\n", "bottom_right", 1},
	}
	for _, c := range cases {
		os.WriteFile(p, []byte(c.ini), 0o644)
		cfg, w, _, err := LoadConfig(p)
		if err != nil || cfg.Position != c.want || len(w) != c.warn {
			t.Errorf("%q：期望 %s/%d 条告警，实际 %s/%v err=%v", c.ini, c.want, c.warn, cfg.Position, w, err)
		}
	}
}

func TestSaveConfigAllPositions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "standup.ini")
	for _, pos := range PositionOrder {
		c := DefaultConfig()
		c.Position = pos
		if err := SaveConfig(p, c); err != nil {
			t.Fatal(err)
		}
		got, w, _, err := LoadConfig(p)
		if err != nil || len(w) != 0 || got.Position != pos {
			t.Errorf("%s 写回再读不一致：%+v warns=%v err=%v", pos, got, w, err)
		}
	}
}
