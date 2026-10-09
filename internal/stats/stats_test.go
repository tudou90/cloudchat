package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cloudchat/internal/database"
	"github.com/redis/go-redis/v9"
)

func TestDayJSON(t *testing.T) {
	d := Day{Date: "2026-10-08", Visitors: 12, Counts: map[string]int64{RoomsCreated: 3, FileBytes: 1 << 20}}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var back Day
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Date != d.Date || back.Visitors != 12 || back.Counts[RoomsCreated] != 3 || back.Counts[FileBytes] != 1<<20 {
		t.Fatalf("round trip lost data: %s -> %+v", b, back)
	}
}

func TestArchiveFile(t *testing.T) {
	a := Archive{Path: filepath.Join(t.TempDir(), "sub", "stats.jsonl")}
	if days, err := a.Load(); err != nil || len(days) != 0 {
		t.Fatalf("missing file should be empty, got %v, %v", days, err)
	}
	for _, date := range []string{"2026-10-02", "2026-10-01"} {
		if err := a.append(Day{Date: date, Visitors: 1, Counts: map[string]int64{Messages: 5}}); err != nil {
			t.Fatal(err)
		}
	}
	days, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 || days[0].Date != "2026-10-01" || days[1].Counts[Messages] != 5 {
		t.Fatalf("unexpected archive: %+v", days)
	}
	if ok, _ := a.Has("2026-10-02"); !ok {
		t.Fatal("Has should find an archived day")
	}
	if ok, _ := a.Has("2026-10-03"); ok {
		t.Fatal("Has found a day that was never archived")
	}
}

// redisForTest connects to a scratch Redis database or skips the test.
func redisForTest(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6389"
	}
	database.RDB = redis.NewClient(&redis.Options{Addr: addr, DB: 14})
	if err := database.RDB.Ping(database.Ctx).Err(); err != nil {
		t.Skipf("no Redis at %s: %v", addr, err)
	}
	keys, _ := database.RDB.Keys(database.Ctx, "stats:*").Result()
	if len(keys) > 0 {
		database.RDB.Del(database.Ctx, keys...)
	}
	t.Cleanup(func() {
		keys, _ := database.RDB.Keys(database.Ctx, "stats:*").Result()
		if len(keys) > 0 {
			database.RDB.Del(database.Ctx, keys...)
		}
	})
}

func TestCountAndRollover(t *testing.T) {
	redisForTest(t)
	clock := time.Date(2026, 10, 8, 23, 0, 0, 0, time.UTC)
	now = func() time.Time { return clock }
	t.Cleanup(func() { now = time.Now })

	Inc(RoomsCreated)
	Add(FileBytes, 2048)
	Peak(PeakOnline, 4)
	Peak(PeakOnline, 2)
	Peak(PeakOnline, 7)
	for _, ip := range []string{"203.0.113.1", "203.0.113.2", "203.0.113.1", "unknown"} {
		Visit(ip)
	}
	d, err := Read("2026-10-08")
	if err != nil {
		t.Fatal(err)
	}
	if d.Visitors != 2 || d.Counts[RoomsCreated] != 1 || d.Counts[FileBytes] != 2048 || d.Counts[PeakOnline] != 7 {
		t.Fatalf("today's counts wrong: %+v", d)
	}

	a := Archive{Path: filepath.Join(t.TempDir(), "stats.jsonl")}
	if dates, _ := a.Rollover(); len(dates) != 0 {
		t.Fatalf("today must not be archived yet, got %v", dates)
	}
	// Next day: yesterday gets archived once, then its Redis keys are gone.
	clock = clock.Add(2 * time.Hour)
	dates, err := a.Rollover()
	if err != nil || len(dates) != 1 || dates[0] != "2026-10-08" {
		t.Fatalf("rollover = %v, %v", dates, err)
	}
	if dates, _ := a.Rollover(); len(dates) != 0 {
		t.Fatalf("second rollover archived again: %v", dates)
	}
	if n, _ := database.RDB.Exists(database.Ctx, countsKey("2026-10-08"), visitorsKey("2026-10-08")).Result(); n != 0 {
		t.Fatal("archived day's keys should be deleted from Redis")
	}
	recent, err := a.Recent(30)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 || recent[0].Date != "2026-10-08" || recent[0].Visitors != 2 || recent[1].Date != "2026-10-09" {
		t.Fatalf("recent = %+v", recent)
	}
}
