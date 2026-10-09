// Package stats keeps daily usage counts: how many people visited, rooms
// were created, messages sent, files shared, and so on. Nothing personal is
// stored: only totals per day, plus a HyperLogLog (a probabilistic set that
// cannot be read back) to count distinct visitors.
//
// Today's counts live in Redis so every server adds to the same numbers.
// Redis is not persistent here (chats are meant to disappear), so once a day
// is over it is archived as one JSON line in a file on disk; that file is the
// permanent record.
package stats

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"cloudchat/internal/database"
	"github.com/redis/go-redis/v9"
)

// Counter names. Each is a field in a day's Redis hash and in its archive line.
const (
	RoomsCreated    = "rooms_created"
	RoomJoins       = "room_joins"
	Messages        = "messages"
	FilesUploaded   = "files_uploaded"
	FileBytes       = "files_bytes"
	FilesDownloaded = "files_downloaded"
	DownloadBytes   = "download_bytes"
	SecretsCreated  = "secrets_created"
	SecretsRead     = "secrets_read"
	PeakOnline      = "peak_online"
	PeakRooms       = "peak_rooms"
	RateLimited     = "rate_limited"
	StorageRejected = "storage_rejected"
)

// Counters lists every counter in the order reports show them.
var Counters = []string{
	RoomsCreated, RoomJoins, Messages,
	FilesUploaded, FileBytes, FilesDownloaded, DownloadBytes,
	SecretsCreated, SecretsRead,
	PeakOnline, PeakRooms, RateLimited, StorageRejected,
}

// Day is one day's totals.
type Day struct {
	Date     string           `json:"date"`
	Visitors int64            `json:"visitors"`
	Counts   map[string]int64 `json:"-"`
}

// MarshalJSON flattens Counts next to date and visitors.
func (d Day) MarshalJSON() ([]byte, error) {
	m := map[string]any{"date": d.Date, "visitors": d.Visitors}
	for k, v := range d.Counts {
		m[k] = v
	}
	return json.Marshal(m)
}

// UnmarshalJSON is the reverse of MarshalJSON.
func (d *Day) UnmarshalJSON(b []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	d.Counts = map[string]int64{}
	for k, v := range m {
		switch k {
		case "date":
			if err := json.Unmarshal(v, &d.Date); err != nil {
				return err
			}
		case "visitors":
			if err := json.Unmarshal(v, &d.Visitors); err != nil {
				return err
			}
		default:
			var n int64
			if err := json.Unmarshal(v, &n); err == nil {
				d.Counts[k] = n
			}
		}
	}
	return nil
}

// Keys stay for this long after their day, so a crashed server or a late
// archive run still finds them. Archiving does not wait for expiry.
const keyTTL = 3 * 24 * time.Hour

var (
	// disabled turns every recorder into a no-op (e.g. in tests, or admin
	// commands that must not count themselves).
	disabled bool
	// now is replaced in tests.
	now = time.Now
)

// Disable stops recording. Reading and archiving still work.
func Disable() { disabled = true }

// DateOf is the archive date of t (UTC, so every server agrees on when a
// day ends).
func DateOf(t time.Time) string { return t.UTC().Format("2006-01-02") }

func today() string { return DateOf(now()) }

func countsKey(date string) string   { return "stats:" + date }
func visitorsKey(date string) string { return "stats:" + date + ":visitors" }

// Add adds n to counter for today.
func Add(counter string, n int64) {
	if disabled || n == 0 {
		return
	}
	key := countsKey(today())
	_, err := database.RDB.TxPipelined(database.Ctx, func(p redis.Pipeliner) error {
		p.HIncrBy(database.Ctx, key, counter, n)
		p.Expire(database.Ctx, key, keyTTL)
		return nil
	})
	if err != nil {
		log.Printf("stats: %s += %d failed: %v", counter, n, err)
	}
}

// Inc adds 1 to counter for today.
func Inc(counter string) { Add(counter, 1) }

// maxLua raises a counter to v if v is higher. KEYS: hash. ARGV: field, v, ttl s.
var maxLua = redis.NewScript(`
local cur = tonumber(redis.call('HGET', KEYS[1], ARGV[1]) or '0')
if tonumber(ARGV[2]) > cur then redis.call('HSET', KEYS[1], ARGV[1], ARGV[2]) end
redis.call('EXPIRE', KEYS[1], ARGV[3])
return 1
`)

// Peak records v as today's peak for counter if it beats the current one.
func Peak(counter string, v int64) {
	if disabled {
		return
	}
	err := maxLua.Run(database.Ctx, database.RDB, []string{countsKey(today())},
		counter, v, int(keyTTL.Seconds())).Err()
	if err != nil {
		log.Printf("stats: peak %s failed: %v", counter, err)
	}
}

// Visit counts one visitor for today. client is the rate limiter's client
// key (an IP or IPv6 /64); it goes into a HyperLogLog, which keeps a few
// hashed bits per element and cannot list what was added.
func Visit(client string) {
	if disabled || client == "" || client == "unknown" {
		return
	}
	key := visitorsKey(today())
	_, err := database.RDB.TxPipelined(database.Ctx, func(p redis.Pipeliner) error {
		p.PFAdd(database.Ctx, key, client)
		p.Expire(database.Ctx, key, keyTTL)
		return nil
	})
	if err != nil {
		log.Printf("stats: visit failed: %v", err)
	}
}

// Read returns the day's totals from Redis (zero values if it has none).
func Read(date string) (Day, error) {
	pipe := database.RDB.Pipeline()
	counts := pipe.HGetAll(database.Ctx, countsKey(date))
	visitors := pipe.PFCount(database.Ctx, visitorsKey(date))
	if _, err := pipe.Exec(database.Ctx); err != nil && !errors.Is(err, redis.Nil) {
		return Day{}, err
	}
	d := Day{Date: date, Visitors: visitors.Val(), Counts: map[string]int64{}}
	for k, v := range counts.Val() {
		n, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			d.Counts[k] = n
		}
	}
	return d, nil
}

func (d Day) empty() bool {
	if d.Visitors > 0 {
		return false
	}
	for _, v := range d.Counts {
		if v != 0 {
			return false
		}
	}
	return true
}

// Archive is the file that keeps finished days, one JSON object per line.
type Archive struct {
	Path string
}

// Load reads every archived day, oldest first. A missing file is empty.
func (a Archive) Load() ([]Day, error) {
	f, err := os.Open(a.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var days []Day
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var d Day
		if err := json.Unmarshal(sc.Bytes(), &d); err != nil {
			return nil, fmt.Errorf("%s: %w", a.Path, err)
		}
		days = append(days, d)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	return days, nil
}

// Has reports whether date is already archived.
func (a Archive) Has(date string) (bool, error) {
	days, err := a.Load()
	if err != nil {
		return false, err
	}
	for _, d := range days {
		if d.Date == date {
			return true, nil
		}
	}
	return false, nil
}

func (a Archive) append(d Day) error {
	if err := os.MkdirAll(filepath.Dir(a.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(a.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// archiveLockTTL is how long one server holds the right to archive a day.
// Long enough to finish writing; short enough that a crash doesn't block
// the other servers for long.
const archiveLockTTL = time.Minute

// Rollover archives every finished day that still has data in Redis and is
// not in the file yet. Several servers may share the archive (same disk)
// or Redis; a lock in Redis makes sure a day is written once. It returns
// the dates it archived.
func (a Archive) Rollover() ([]string, error) {
	cur := today()
	var done []string
	for i := 1; i <= 3; i++ {
		date := DateOf(now().AddDate(0, 0, -i))
		if date >= cur {
			continue
		}
		if ok, err := a.Has(date); err != nil || ok {
			if err != nil {
				return done, err
			}
			continue
		}
		d, err := Read(date)
		if err != nil {
			return done, err
		}
		if d.empty() {
			continue
		}
		lock := "stats:archive:" + date
		got, err := database.RDB.SetNX(database.Ctx, lock, "1", archiveLockTTL).Result()
		if err != nil || !got {
			continue // another server is on it (or Redis is down; retry later)
		}
		if err := a.append(d); err != nil {
			database.RDB.Del(database.Ctx, lock)
			return done, err
		}
		database.RDB.Del(database.Ctx, countsKey(date), visitorsKey(date))
		done = append(done, date)
	}
	return done, nil
}

// StartRollover archives finished days now and then every hour, so a day's
// counts reach the file soon after midnight UTC whichever server is up.
func (a Archive) StartRollover() {
	run := func() {
		if dates, err := a.Rollover(); err != nil {
			log.Printf("stats: archiving to %s failed: %v", a.Path, err)
		} else if len(dates) > 0 {
			log.Printf("stats: archived %v to %s", dates, a.Path)
		}
	}
	go func() {
		run()
		for range time.Tick(time.Hour) {
			run()
		}
	}()
}

// Recent returns the last n days (oldest first): archived days from the
// file plus any still in Redis, including today.
func (a Archive) Recent(n int) ([]Day, error) {
	archived, err := a.Load()
	if err != nil {
		return nil, err
	}
	byDate := map[string]Day{}
	for _, d := range archived {
		byDate[d.Date] = d
	}
	// Days still in Redis: today and up to 3 days back (keyTTL).
	for i := 0; i <= 3; i++ {
		date := DateOf(now().AddDate(0, 0, -i))
		if _, ok := byDate[date]; ok {
			continue
		}
		d, err := Read(date)
		if err != nil {
			return nil, err
		}
		if !d.empty() || i == 0 {
			byDate[date] = d
		}
	}
	dates := make([]string, 0, len(byDate))
	for date := range byDate {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	if n > 0 && len(dates) > n {
		dates = dates[len(dates)-n:]
	}
	days := make([]Day, 0, len(dates))
	for _, date := range dates {
		days = append(days, byDate[date])
	}
	return days, nil
}
